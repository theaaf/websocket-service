package cluster

import (
	"fmt"

	wss "github.com/theaaf/websocket-service"
	"github.com/theaaf/websocket-service/subprotocol"
)

type memoryClusterMember struct {
	ServiceRequests          chan *wss.ServiceRequest
	GraphQLWSServiceRequests chan *subprotocol.GraphQLWSServiceRequest
}

// MemoryCluster can be used to create a simple in-memory cluster, useful for testing.
type MemoryCluster struct {
	address wss.Address
	members map[string]memoryClusterMember
}

var _ wss.Cluster = &MemoryCluster{}

func NewMemoryCluster() *MemoryCluster {
	address := fmt.Sprintf("%d", 0)
	return &MemoryCluster{
		address: wss.Address(address),
		members: map[string]memoryClusterMember{
			address: {
				ServiceRequests:          make(chan *wss.ServiceRequest, 1000),
				GraphQLWSServiceRequests: make(chan *subprotocol.GraphQLWSServiceRequest, 1000),
			},
		},
	}
}

func JoinMemoryCluster(other *MemoryCluster) *MemoryCluster {
	address := fmt.Sprintf("%d", len(other.members))
	other.members[address] = memoryClusterMember{
		ServiceRequests:          make(chan *wss.ServiceRequest, 1000),
		GraphQLWSServiceRequests: make(chan *subprotocol.GraphQLWSServiceRequest, 1000),
	}
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
	case c.members[string(address)].ServiceRequests <- r:
		return nil
	default:
		return fmt.Errorf("destination buffer full")
	}
}

func (c *MemoryCluster) ServiceRequests() <-chan *wss.ServiceRequest {
	return c.members[string(c.address)].ServiceRequests
}

func (c *MemoryCluster) SendGraphQLWSServiceRequest(address wss.Address, r *subprotocol.GraphQLWSServiceRequest) error {
	select {
	case c.members[string(address)].GraphQLWSServiceRequests <- r:
		return nil
	default:
		return fmt.Errorf("destination buffer full")
	}
}

func (c *MemoryCluster) GraphQLWSServiceRequests() <-chan *subprotocol.GraphQLWSServiceRequest {
	return c.members[string(c.address)].GraphQLWSServiceRequests
}
