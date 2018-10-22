package websocketservice

import (
	"bytes"
	"encoding/base64"
)

type Address []byte

func (address Address) String() string {
	return base64.RawURLEncoding.EncodeToString(address)
}

func (address Address) Equal(other Address) bool {
	return bytes.Equal(address, other)
}
