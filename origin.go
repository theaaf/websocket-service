package websocketservice

type Origin interface {
	ServeWebSocketService(*OriginRequest) (*OriginResponse, error)
}

type OriginFunc func(*OriginRequest) (*OriginResponse, error)

func (f OriginFunc) ServeWebSocketService(r *OriginRequest) (*OriginResponse, error) {
	return f(r)
}

func HTTPOrigin(url string) Origin {
	// TODO
	return nil
}
