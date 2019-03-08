package subprotocol_test

import (
	"encoding/json"
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
	"github.com/theaaf/websocket-service/subprotocol"
)

type TestOrigin struct {
	t              *testing.T
	handlers       chan subprotocol.GraphQLWSOriginFunc
	requestHandled chan struct{}
}

func (o *TestOrigin) SendGraphQLWSOriginRequest(request *subprotocol.GraphQLWSOriginRequest) (*subprotocol.GraphQLWSOriginResponse, error) {
	o.t.Helper()

	select {
	case handler := <-o.handlers:
		resp, err := handler(request)
		o.requestHandled <- struct{}{}
		return resp, err
	case <-time.After(2 * time.Second):
		o.t.Fatal("received unexpected request")
	}
	return nil, nil
}

func (o *TestOrigin) WaitForRequest(f subprotocol.GraphQLWSOriginFunc) {
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
		handlers:       make(chan subprotocol.GraphQLWSOriginFunc),
		requestHandled: make(chan struct{}, 1),
	}
}

func newTestClient(t *testing.T, sp *subprotocol.GraphQLWS) *websocket.Conn {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()

	httpServer := &http.Server{
		Handler: sp,
	}
	go httpServer.Serve(l)

	dialer := &websocket.Dialer{
		HandshakeTimeout: time.Second,
		Subprotocols:     []string{"graphql-ws"},
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

type graphqlWSMessage struct {
	Id      string          `json:"id"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func TestGraphQLWS(t *testing.T) {
	origin := newTestOrigin(t)

	// Create a two-node cluster.
	clusterA := cluster.NewMemoryCluster()
	nodeA := &subprotocol.GraphQLWS{
		Origin:  origin,
		Cluster: clusterA,
	}
	clusterB := cluster.JoinMemoryCluster(clusterA)
	nodeB := &subprotocol.GraphQLWS{
		Origin:  origin,
		Cluster: clusterB,
	}

	// Forward requests from clusters to their nodes.
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case r := <-clusterA.GraphQLWSServiceRequests():
				nodeA.HandleGraphQLWSServiceRequest(r)
			case r := <-clusterB.GraphQLWSServiceRequests():
				nodeB.HandleGraphQLWSServiceRequest(r)
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

	// Here's our WebSocket client.
	client := newTestClient(t, nodeA)
	defer func() {
		assert.NoError(t, client.Close())
	}()

	require.NoError(t, client.WriteJSON(map[string]string{
		"id":   "init",
		"type": "connection_init",
	}))

	var msg graphqlWSMessage

	require.NoError(t, client.ReadJSON(&msg))
	assert.Equal(t, "connection_ack", msg.Type)

	require.NoError(t, client.ReadJSON(&msg))
	assert.Equal(t, "ka", msg.Type)

	t.Run("Query", func(t *testing.T) {
		require.NoError(t, client.WriteJSON(map[string]interface{}{
			"id":   "query",
			"type": "start",
			"payload": map[string]interface{}{
				"query": `
					query Foo($foo: String!) {
						foo
					}
				`,
				"operationName": "Foo",
				"variables": map[string]interface{}{
					"foo": "bar",
				},
			},
		}))

		origin.WaitForRequest(func(request *subprotocol.GraphQLWSOriginRequest) (*subprotocol.GraphQLWSOriginResponse, error) {
			assert.Equal(t, nodeA.Address(), request.Sender)
			require.NotNil(t, request.StartRequest)
			assert.NotEmpty(t, request.StartRequest.Query)
			assert.Equal(t, "Foo", request.StartRequest.OperationName)
			assert.NotEmpty(t, request.StartRequest.Variables)
			return &subprotocol.GraphQLWSOriginResponse{
				Result: &subprotocol.GraphQLWSResult{
					Data: json.RawMessage(`{"foo": "bar"}`),
				},
			}, nil
		})

		require.NoError(t, client.ReadJSON(&msg))
		assert.Equal(t, "query", msg.Id)
		assert.Equal(t, "data", msg.Type)

		require.NoError(t, client.ReadJSON(&msg))
		assert.Equal(t, "query", msg.Id)
		assert.Equal(t, "complete", msg.Type)
	})

	t.Run("QueryError", func(t *testing.T) {
		require.NoError(t, client.WriteJSON(map[string]interface{}{
			"id":   "query",
			"type": "start",
			"payload": map[string]interface{}{
				"query": `
					query {
						foo
					}
				`,
			},
		}))

		origin.WaitForRequest(func(request *subprotocol.GraphQLWSOriginRequest) (*subprotocol.GraphQLWSOriginResponse, error) {
			return &subprotocol.GraphQLWSOriginResponse{
				Result: &subprotocol.GraphQLWSResult{
					Data:   json.RawMessage(`{"foo": null}`),
					Errors: json.RawMessage(`[{"message": "error!"}]`),
				},
			}, nil
		})

		require.NoError(t, client.ReadJSON(&msg))
		assert.Equal(t, "query", msg.Id)
		assert.Equal(t, "data", msg.Type)
		assert.JSONEq(t, `{"data":{"foo": null},"errors":[{"message": "error!"}]}`, string(msg.Payload))

		require.NoError(t, client.ReadJSON(&msg))
		assert.Equal(t, "query", msg.Id)
		assert.Equal(t, "complete", msg.Type)
	})

	t.Run("Subscription", func(t *testing.T) {
		require.NoError(t, client.WriteJSON(map[string]interface{}{
			"id":   "sub",
			"type": "start",
			"payload": map[string]interface{}{
				"query": `
					subscription {
						foo
					}
				`,
			},
		}))

		origin.WaitForRequest(func(request *subprotocol.GraphQLWSOriginRequest) (*subprotocol.GraphQLWSOriginResponse, error) {
			assert.Equal(t, nodeA.Address(), request.Sender)
			require.NotNil(t, request.StartRequest)
			assert.NotEmpty(t, request.StartRequest.Query)
			return &subprotocol.GraphQLWSOriginResponse{
				Subscription: &subprotocol.GraphQLWSSubscription{
					InputDigest:  "foo",
					InputContext: json.RawMessage(`"foo"`),
					Keys:         []string{"foo"},
				},
			}, nil
		})

		nodeB.HandleGraphQLWSServiceRequest(&subprotocol.GraphQLWSServiceRequest{
			PublishRequest: &subprotocol.GraphQLWSPublishRequest{
				Subscribers: []wss.Address{nodeA.Address()},
				Keys:        []string{"foo"},
				Context:     json.RawMessage(`"foo"`),
			},
		})

		origin.WaitForRequest(func(request *subprotocol.GraphQLWSOriginRequest) (*subprotocol.GraphQLWSOriginResponse, error) {
			assert.Equal(t, nodeA.Address(), request.Sender)
			require.NotNil(t, request.SubscriptionQueryRequest)
			assert.NotEmpty(t, request.SubscriptionQueryRequest.Query)
			assert.Equal(t, json.RawMessage(`"foo"`), request.SubscriptionQueryRequest.Context)
			return &subprotocol.GraphQLWSOriginResponse{
				Result: &subprotocol.GraphQLWSResult{
					Data: json.RawMessage(`{"foo": "bar"}`),
				},
			}, nil
		})

		require.NoError(t, client.ReadJSON(&msg))
		assert.Equal(t, "sub", msg.Id)
		assert.Equal(t, "data", msg.Type)
	})
}
