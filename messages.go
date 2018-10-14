package websocketservice

type OriginRequest struct {
	WebSocketEvent *WebSocketEvent
}

type OriginResponse struct {
	Action *Action
}

type Action struct {
	WebSocketActions []*WebSocketAction
}

type WebSocketEventConnectionEstablished struct {
	Subprotocol string
}

type WebSocketEvent struct {
	ConnectionId []byte

	ConnectionEstablished *WebSocketEventConnectionEstablished
}

type WebSocketAction struct {
	ConnectionId []byte

	SendFrames []struct {
		Data []byte
	}
}
