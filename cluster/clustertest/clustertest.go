package clustertest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/theaaf/websocket-service"
)

type Cluster struct {
	Cluster         websocketservice.Cluster
	ServiceRequests <-chan *websocketservice.ServiceRequest
}

func (c *Cluster) WaitForServiceRequest(t *testing.T) *websocketservice.ServiceRequest {
	t.Helper()
	select {
	case r := <-c.ServiceRequests:
		return r
	case <-time.After(time.Second):
		t.Fatal("no request received")
	}
	return nil
}

func Test(t *testing.T, a, b, c *Cluster) {
	b.Cluster.SendServiceRequest(a.Cluster.Address(), &websocketservice.ServiceRequest{})
	r := a.WaitForServiceRequest(t)
	assert.NotNil(t, r)

	b.Cluster.SendServiceRequest(c.Cluster.Address(), &websocketservice.ServiceRequest{})
	r = c.WaitForServiceRequest(t)
	assert.NotNil(t, r)
}
