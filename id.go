package websocketservice

import (
	"crypto/rand"
	"encoding/base64"
)

type Id []byte

func NewId() (Id, error) {
	id := make([]byte, 20)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	return id, nil
}

func (id Id) String() string {
	return base64.RawURLEncoding.EncodeToString(id)
}
