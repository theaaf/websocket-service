package websocketservice

import (
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type Service struct {
	Subprotocols []string
	Origin       Origin
	Logger       logrus.FieldLogger

	connectionsMutex sync.Mutex
	connections      map[string]*Connection
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if websocket.IsWebSocketUpgrade(r) {
		s.serveWebSocket(w, r)
		return
	}
	http.Error(w, "not a websocket upgrade", http.StatusBadRequest)
}

func (s *Service) serveWebSocket(w http.ResponseWriter, r *http.Request) {
	var upgrader = websocket.Upgrader{
		CheckOrigin:  func(r *http.Request) bool { return true },
		Subprotocols: s.Subprotocols,
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	id, err := NewId()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	logger := s.Logger
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	logger = logger.WithField("connection_id", id)
	logger.Info("connection established")

	s.connectionsMutex.Lock()
	if s.connections == nil {
		s.connections = make(map[string]*Connection)
	}
	s.connections[string(id)] = &Connection{
		id:     id,
		conn:   conn,
		logger: logger,
	}
	s.connectionsMutex.Unlock()

	_, err = s.Origin.ServeWebSocketService(&OriginRequest{
		WebSocketEvent: &WebSocketEvent{
			ConnectionId: id,
			ConnectionEstablished: &WebSocketEventConnectionEstablished{
				Subprotocol: conn.Subprotocol(),
			},
		},
	})
	if err != nil {
		logger.Warn(errors.Wrap(err, "origin error on connection establishment"))
		s.CloseConnection(id)
	}

	return
}

func (s *Service) Close() error {
	// TODO
	return nil
}

func (s *Service) CloseConnection(id Id) {
	// TODO
}
