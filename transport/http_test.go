package transport

import (
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	wss "github.com/theaaf/websocket-service"
)

func TestDeadPeer(t *testing.T) {
	start := time.Now()
	cluster := &HTTPCluster{}
	err := cluster.SendServiceRequest(wss.Address("192.0.2.0:1234"), &wss.ServiceRequest{})
	assert.Error(t, err)
	assert.True(t, time.Now().Sub(start) < 2*time.Second)
}

func TestSlowPeer(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()

	httpServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			time.Sleep(3 * time.Second)
			fmt.Fprint(w, "hello")
		}),
	}
	go httpServer.Serve(l)

	cluster := &HTTPCluster{}
	err = cluster.SendServiceRequest(wss.Address(l.Addr().String()), &wss.ServiceRequest{})
	assert.NoError(t, err)
}
