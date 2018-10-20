package websocketservice

import (
	"context"
	"fmt"
)

type Cluster interface {
	// Returns the address of the local node. Other nodes in the cluster should be able to send
	// messages to this node using this address.
	Address() []byte

	Send(address, message []byte) error

	Receive(context.Context) ([]byte, error)
}

// MemoryCluster can be used to create a simple in-memory cluster, useful for testing.
type MemoryCluster struct {
	address string
	members map[string]chan []byte
}

var _ Cluster = &MemoryCluster{}

func NewMemoryCluster() *MemoryCluster {
	address := fmt.Sprintf("%d", 0)
	return &MemoryCluster{
		address: address,
		members: map[string]chan []byte{
			address: make(chan []byte, 1000),
		},
	}
}

func JoinMemoryCluster(other *MemoryCluster) *MemoryCluster {
	address := fmt.Sprintf("%d", len(other.members))
	other.members[address] = make(chan []byte, 1000)
	return &MemoryCluster{
		address: address,
		members: other.members,
	}
}

func (c *MemoryCluster) Address() []byte {
	return []byte(c.address)
}

func (c *MemoryCluster) Send(address, message []byte) error {
	select {
	case c.members[string(address)] <- message:
		return nil
	default:
		return fmt.Errorf("destination message buffer full")
	}
}

func (c *MemoryCluster) Receive(ctx context.Context) ([]byte, error) {
	select {
	case message := <-c.members[c.address]:
		return message, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
