package cluster

import (
	"fmt"

	wss "github.aaf.cloud/platform/websocket-service"
)

// MemoryCluster can be used to create a simple in-memory cluster, useful for testing.
type MemoryCluster struct {
	address wss.Address
	members map[string]chan *wss.ServiceRequest
}

var _ wss.Cluster = &MemoryCluster{}

func NewMemoryCluster() *MemoryCluster {
	address := fmt.Sprintf("%d", 0)
	return &MemoryCluster{
		address: wss.Address(address),
		members: map[string]chan *wss.ServiceRequest{
			address: make(chan *wss.ServiceRequest, 1000),
		},
	}
}

func JoinMemoryCluster(other *MemoryCluster) *MemoryCluster {
	address := fmt.Sprintf("%d", len(other.members))
	other.members[address] = make(chan *wss.ServiceRequest, 1000)
	return &MemoryCluster{
		address: wss.Address(address),
		members: other.members,
	}
}

func (c *MemoryCluster) Address() wss.Address {
	return c.address
}

func (c *MemoryCluster) SendServiceRequest(address wss.Address, r *wss.ServiceRequest) error {
	select {
	case c.members[string(address)] <- r:
		return nil
	default:
		return fmt.Errorf("destination buffer full")
	}
}

func (c *MemoryCluster) ServiceRequests() <-chan *wss.ServiceRequest {
	return c.members[string(c.address)]
}
