package websocketservice

type OriginRequest struct {
	WebSocketEvent *WebSocketEvent
}

type OriginResponse struct{}

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
