package websocketservice

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewConnectionId(t *testing.T) {
	id, err := NewConnectionId(Address("foo"))
	require.NoError(t, err)
	assert.Equal(t, Address("foo"), id.Address())
}
