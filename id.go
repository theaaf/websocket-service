package websocketservice

import (
	"crypto/rand"
	"encoding/base64"
)

type Id []byte

func NewId(address []byte) (Id, error) {
	id := make([]byte, 21+len(address))
	id[0] = 0
	copy(id[1:], address)
	if _, err := rand.Read(id[1+len(address):]); err != nil {
		return nil, err
	}
	return id, nil
}

func (id Id) Address() []byte {
	if id[0] != 0 || len(id) < 21 {
		return nil
	}
	return id[1 : len(id)-20]
}

func (id Id) String() string {
	return base64.RawURLEncoding.EncodeToString(id)
}
