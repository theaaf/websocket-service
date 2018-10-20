package websocketservice

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testCluster(t *testing.T, a, b, c Cluster) {
	b.Send(a.Address(), []byte("foo"))
	message, err := a.Receive(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []byte("foo"), message)

	b.Send(c.Address(), []byte("bar"))
	message, err = c.Receive(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []byte("bar"), message)
}

func TestMemoryCluster(t *testing.T) {
	a := NewMemoryCluster()
	b := JoinMemoryCluster(a)
	c := JoinMemoryCluster(a)
	testCluster(t, a, b, c)
}
