package websocketservice

import (
	"testing"
	"time"
)

type TestOrigin struct {
	t        *testing.T
	handlers chan OriginFunc
}

func (o *TestOrigin) ServeWebSocketService(request *OriginRequest) (*OriginResponse, error) {
	o.t.Helper()
	select {
	case handler := <-o.handlers:
		return handler(request)
	case <-time.After(time.Second):
		o.t.Fatal("received unexpected request")
	}
	return nil, nil
}

func (o *TestOrigin) WaitForRequest(f OriginFunc) {
	o.t.Helper()
	select {
	case o.handlers <- f:
	case <-time.After(time.Second):
		o.t.Fatal("expected request")
	}
}

func newTestOrigin(t *testing.T) *TestOrigin {
	return &TestOrigin{
		t:        t,
		handlers: make(chan OriginFunc),
	}
}
