package websocketservice

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewId(t *testing.T) {
	id, err := NewId(Address("foo"))
	require.NoError(t, err)
	assert.Equal(t, Address("foo"), id.Address())
}
