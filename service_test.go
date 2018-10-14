package websocketservice

import (
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestClient(t *testing.T, s *Service, subprotocols []string) *websocket.Conn {
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

	s0 := &Service{
		Subprotocols: []string{"test"},
		Origin:       origin,
	}
	defer s0.Close()

	client := newTestClient(t, s0, s0.Subprotocols)
	defer client.Close()

	origin.WaitForRequest(func(request *OriginRequest) (*OriginResponse, error) {
		require.NotNil(t, request.WebSocketEvent)
		assert.NotEmpty(t, request.WebSocketEvent.ConnectionId)
		require.NotNil(t, request.WebSocketEvent.ConnectionEstablished)
		assert.Equal(t, "test", request.WebSocketEvent.ConnectionEstablished.Subprotocol)
		return nil, nil
	})
}
