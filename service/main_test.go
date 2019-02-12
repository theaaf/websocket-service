package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	jsoniter "github.com/json-iterator/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	wss "github.com/theaaf/websocket-service"
)

func TestServe(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()

	router := mux.NewRouter()
	router.HandleFunc("/origin", func(w http.ResponseWriter, r *http.Request) {
		var originRequest *wss.OriginRequest
		err := jsoniter.NewDecoder(r.Body).Decode(&originRequest)
		require.NoError(t, err)

		if originRequest.WebSocketEvent.MessageReceived != nil {
			responseText := "bar"
			payload, err := jsoniter.Marshal(&wss.ServiceRequest{
				OutgoingWebSocketMessages: []*wss.OutgoingWebSocketMessage{
					{
						ConnectionIds: []wss.ConnectionId{originRequest.WebSocketEvent.ConnectionId},
						Message: &wss.WebSocketMessage{
							Text: &responseText,
						},
					},
				},
			})
			require.NoError(t, err)

			resp, err := http.Post("http://127.0.0.1:10011/sr", "application/json", bytes.NewReader(payload))
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, http.StatusOK, resp.StatusCode)
		}
	})

	httpServer := &http.Server{
		Handler: router,
	}
	go httpServer.Serve(l)
	defer httpServer.Close()

	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())

	wg.Add(1)
	go func() {
		defer wg.Done()
		assert.NoError(t, serve(ctx, []string{"--origin-url", "http://" + l.Addr().String() + "/origin", "--websocket-port", "10000", "--service-request-port", "10010"}))
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		assert.NoError(t, serve(ctx, []string{"--origin-url", "http://" + l.Addr().String() + "/origin", "--websocket-port", "10001", "--service-request-port", "10011"}))
	}()

	defer wg.Wait()
	defer cancel()

	dialer := &websocket.Dialer{
		HandshakeTimeout: time.Second,
	}

	var client *websocket.Conn
	for attempts := 0; attempts < 100; attempts++ {
		var err error
		if client, _, err = dialer.Dial("ws://127.0.0.1:10000", nil); err == nil {
			break
		}
	}
	require.NotNil(t, client)

	require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte("foo")))

	messageType, message, err := client.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, websocket.TextMessage, messageType)
	assert.Equal(t, "bar", string(message))
}
