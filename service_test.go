package websocketservice_test

import (
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	wss "github.com/theaaf/websocket-service"
	"github.com/theaaf/websocket-service/cluster"
)

type TestOrigin struct {
	t              *testing.T
	handlers       chan wss.OriginFunc
	requestHandled chan struct{}
}

func (o *TestOrigin) SendOriginRequest(request *wss.OriginRequest) error {
	o.t.Helper()

	select {
	case handler := <-o.handlers:
		ret := handler(request)
		o.requestHandled <- struct{}{}
		return ret
	case <-time.After(2 * time.Second):
		o.t.Fatal("received unexpected request")
	}
	return nil
}

func (o *TestOrigin) WaitForClose(connectionId wss.ConnectionId) {
	o.t.Helper()
	o.WaitForRequest(func(request *wss.OriginRequest) error {
		require.NotNil(o.t, request.WebSocketEvent)
		assert.Equal(o.t, connectionId, request.WebSocketEvent.ConnectionId)
		require.NotNil(o.t, request.WebSocketEvent.ConnectionClosed)
		return nil
	})
}

func (o *TestOrigin) WaitForRequest(f wss.OriginFunc) {
	o.t.Helper()
	select {
	case o.handlers <- f:
		<-o.requestHandled
	case <-time.After(2 * time.Second):
		o.t.Fatal("expected request")
	}
}

func newTestOrigin(t *testing.T) *TestOrigin {
	return &TestOrigin{
		t:              t,
		handlers:       make(chan wss.OriginFunc),
		requestHandled: make(chan struct{}, 1),
	}
}

func newTestClient(t *testing.T, s *wss.Service, subprotocols []string) *websocket.Conn {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()

	httpServer := &http.Server{
		Handler: s,
	}
	go httpServer.Serve(l)

	dialer := &websocket.Dialer{
		HandshakeTimeout: time.Second,
		Subprotocols:     subprotocols,
	}

	for attempts := 0; true; attempts++ {
		clientConn, _, err := dialer.Dial("ws://"+l.Addr().String(), nil)
		if err != nil {
			if attempts > 100 {
				t.Fatal(errors.Wrap(err, "error dialing websocket connection"))
			}
			time.Sleep(time.Millisecond * 10)
			continue
		}
		return clientConn
	}

	return nil
}

func TestService(t *testing.T) {
	origin := newTestOrigin(t)

	// Create a two-node cluster.
	clusterA := cluster.NewMemoryCluster()
	nodeA := &wss.Service{
		Subprotocols: []string{"test"},
		Origin:       origin,
		Cluster:      clusterA,
	}
	clusterB := cluster.JoinMemoryCluster(clusterA)
	nodeB := &wss.Service{
		Subprotocols: []string{"test"},
		Origin:       origin,
		Cluster:      clusterB,
	}

	// Forward requests from clusters to their nodes.
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case r := <-clusterA.ServiceRequests():
				nodeA.HandleServiceRequest(r)
			case r := <-clusterB.ServiceRequests():
				nodeB.HandleServiceRequest(r)
			case <-stop:
				return
			}
		}
	}()
	// Shut the nodes down when the test is over.
	defer func() {
		close(stop)
		assert.NoError(t, nodeA.Close())
		assert.NoError(t, nodeB.Close())
	}()

	var connectionId wss.ConnectionId

	// Here's our WebSocket client.
	client := newTestClient(t, nodeA, nodeA.Subprotocols)
	defer func() {
		assert.NoError(t, client.Close())
		origin.WaitForClose(connectionId)
	}()

	// Wait for the origin to get the "connection established" message.
	origin.WaitForRequest(func(request *wss.OriginRequest) error {
		require.NotNil(t, request.WebSocketEvent)
		assert.NotEmpty(t, request.WebSocketEvent.ConnectionId)
		connectionId = request.WebSocketEvent.ConnectionId
		require.NotNil(t, request.WebSocketEvent.ConnectionEstablished)
		assert.Equal(t, "test", request.WebSocketEvent.ConnectionEstablished.Subprotocol)
		return nil
	})

	// Send a WebSocket message.
	require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte("foo")))

	// Wait for the origin to get that message.
	origin.WaitForRequest(func(request *wss.OriginRequest) error {
		require.NotNil(t, request.WebSocketEvent)
		assert.Equal(t, connectionId, request.WebSocketEvent.ConnectionId)
		assert.Equal(t, "foo", *request.WebSocketEvent.MessageReceived.Text)
		return nil
	})

	// Send a WebSocket message back to the client.
	responseText := "bar"
	nodeA.HandleServiceRequest(&wss.ServiceRequest{
		OutgoingWebSocketMessages: []*wss.OutgoingWebSocketMessage{
			{
				ConnectionIds: []wss.ConnectionId{connectionId},
				Message: &wss.WebSocketMessage{
					Text: &responseText,
				},
			},
		},
	})

	// Wait for the client to get that message.
	messageType, message, err := client.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, websocket.TextMessage, messageType)
	assert.Equal(t, "bar", string(message))

	// Now try sending a message to the client again, but through a different cluster node.
	t.Run("CrossNode", func(t *testing.T) {
		messageText := "baz"
		nodeB.HandleServiceRequest(&wss.ServiceRequest{
			OutgoingWebSocketMessages: []*wss.OutgoingWebSocketMessage{
				{
					ConnectionIds: []wss.ConnectionId{connectionId},
					Message: &wss.WebSocketMessage{
						Text: &messageText,
					},
				},
			},
		})

		messageType, message, err := client.ReadMessage()
		require.NoError(t, err)
		assert.Equal(t, websocket.TextMessage, messageType)
		assert.Equal(t, "baz", string(message))
	})
}

func TestService_KeepAliveInterval(t *testing.T) {
	origin := newTestOrigin(t)

	// Create a cluster.
	cluster := cluster.NewMemoryCluster()
	node := &wss.Service{
		Subprotocols:      []string{"test"},
		Origin:            origin,
		Cluster:           cluster,
		KeepAliveInterval: 500 * time.Millisecond,
	}

	// Forward requests from clusters to their nodes.
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case r := <-cluster.ServiceRequests():
				node.HandleServiceRequest(r)
			case <-stop:
				return
			}
		}
	}()
	// Shut the nodes down when the test is over.
	defer func() {
		close(stop)
		assert.NoError(t, node.Close())
	}()

	var connectionId wss.ConnectionId

	// Here's our WebSocket client.
	client := newTestClient(t, node, node.Subprotocols)
	defer func() {
		assert.NoError(t, client.Close())
		origin.WaitForClose(connectionId)
	}()

	// Wait for the origin to get the "connection established" message.
	origin.WaitForRequest(func(request *wss.OriginRequest) error {
		require.NotNil(t, request.WebSocketEvent)
		assert.NotEmpty(t, request.WebSocketEvent.ConnectionId)
		connectionId = request.WebSocketEvent.ConnectionId
		require.NotNil(t, request.WebSocketEvent.ConnectionEstablished)
		assert.Equal(t, "test", request.WebSocketEvent.ConnectionEstablished.Subprotocol)
		return nil
	})

	// Wait for the origin to get a keep-alive.
	origin.WaitForRequest(func(request *wss.OriginRequest) error {
		assert.NotEmpty(t, request.WebSocketKeepAlives)
		return nil
	})
}

func TestService_Close(t *testing.T) {
	origin := newTestOrigin(t)

	// Create a cluster.
	cluster := cluster.NewMemoryCluster()
	node := &wss.Service{
		Subprotocols: []string{"test"},
		Origin:       origin,
		Cluster:      cluster,
	}

	// Forward requests from clusters to their nodes.
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case r := <-cluster.ServiceRequests():
				node.HandleServiceRequest(r)
			case <-stop:
				return
			}
		}
	}()
	// Shut the nodes down when the test is over.
	defer func() {
		close(stop)
		assert.NoError(t, node.Close())
	}()

	// Here's our WebSocket client.
	client := newTestClient(t, node, node.Subprotocols)
	defer func() {
		if client != nil {
			assert.NoError(t, client.Close())
		}
	}()

	var connectionId wss.ConnectionId

	// Wait for the origin to get the "connection established" message.
	origin.WaitForRequest(func(request *wss.OriginRequest) error {
		require.NotNil(t, request.WebSocketEvent)
		assert.NotEmpty(t, request.WebSocketEvent.ConnectionId)
		connectionId = request.WebSocketEvent.ConnectionId
		require.NotNil(t, request.WebSocketEvent.ConnectionEstablished)
		assert.Equal(t, "test", request.WebSocketEvent.ConnectionEstablished.Subprotocol)
		return nil
	})

	assert.Equal(t, 1, node.ConnectionCount())

	// Close the connection.
	assert.NoError(t, client.Close())
	client = nil

	origin.WaitForClose(connectionId)

	assert.Equal(t, 0, node.ConnectionCount())
}
