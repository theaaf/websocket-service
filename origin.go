package websocketservice

import (
	"fmt"

	"github.com/gorilla/websocket"
)

type Origin interface {
	SendOriginRequest(*OriginRequest) error
}

type OriginFunc func(*OriginRequest) error

func (f OriginFunc) SendOriginRequest(r *OriginRequest) error {
	return f(r)
}

type OriginRequest struct {
	ServiceURI     string
	WebSocketEvent *WebSocketEvent
}

type WebSocketEventConnectionEstablished struct {
	Subprotocol string
}

type WebSocketEvent struct {
	ConnectionId Id

	ConnectionEstablished *WebSocketEventConnectionEstablished
	MessageReceived       *WebSocketMessage
}

type WebSocketMessage struct {
	Binary []byte
	Text   *string
}

func (msg *WebSocketMessage) PreparedMessage() (*websocket.PreparedMessage, error) {
	if msg.Binary == nil && msg.Text != nil {
		return websocket.NewPreparedMessage(websocket.TextMessage, []byte(*msg.Text))
	} else if msg.Binary != nil && msg.Text == nil {
		return websocket.NewPreparedMessage(websocket.BinaryMessage, msg.Binary)
	}
	return nil, fmt.Errorf("invalid websocket message")
}
