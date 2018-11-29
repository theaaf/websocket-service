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

	// The service will periodically send keep-alives, which don't represent actual websocket
	// events, but just let you know the connections are still alive and well.
	WebSocketKeepAlives []*WebSocketKeepAlive
}

type WebSocketEventConnectionEstablished struct {
	Subprotocol string
}

type WebSocketEventConnectionClosed struct{}

type WebSocketEvent struct {
	ConnectionId ConnectionId

	ConnectionEstablished *WebSocketEventConnectionEstablished
	ConnectionClosed      *WebSocketEventConnectionClosed
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

type WebSocketKeepAlive struct {
	ConnectionId ConnectionId
}
