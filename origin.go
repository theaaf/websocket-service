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
	WebSocketEvent *WebSocketEvent
}

type WebSocketEventConnectionEstablished struct {
	Subprotocol string
}

type WebSocketEvent struct {
	ConnectionId ConnectionId

	ConnectionEstablished *WebSocketEventConnectionEstablished
	MessageReceived       *WebSocketMessage

	// The service will periodically send keep-alive events, which don't represent actual websocket
	// events, but just let you know the connection is still alive and well.
	KeepAlive *WebSocketKeepAlive
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

type WebSocketKeepAlive struct{}
