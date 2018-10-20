package websocketservice

import (
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type Service struct {
	Subprotocols []string
	Origin       Origin
	Cluster      Cluster
	Logger       logrus.FieldLogger

	initOnce sync.Once

	localAddress []byte

	connectionsMutex sync.Mutex
	connections      map[string]*Connection
	connectionIds    map[*Connection]Id
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.init()

	if websocket.IsWebSocketUpgrade(r) {
		s.serveWebSocket(w, r)
		return
	}
	http.Error(w, "not a websocket upgrade", http.StatusBadRequest)
}

func (s *Service) init() {
	s.initOnce.Do(func() {
		if s.connections == nil {
			s.connections = make(map[string]*Connection)
		}
		if s.connectionIds == nil {
			s.connectionIds = make(map[*Connection]Id)
		}
		s.localAddress = s.Cluster.Address()
	})
}

func (s *Service) connectionLogger(connectionId Id) logrus.FieldLogger {
	logger := s.Logger
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return logger.WithField("connection_id", connectionId)
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

	id, err := NewId(s.localAddress)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	logger := s.connectionLogger(id)
	logger.Info("connection established")

	s.connectionsMutex.Lock()
	connection := NewConnection(conn, logger, s.connectionHandler(id, logger))
	s.connections[string(id)] = connection
	s.connectionIds[connection] = id
	s.connectionsMutex.Unlock()

	if _, err := s.Origin.ServeWebSocketService(&OriginRequest{
		WebSocketEvent: &WebSocketEvent{
			ConnectionId: id,
			ConnectionEstablished: &WebSocketEventConnectionEstablished{
				Subprotocol: conn.Subprotocol(),
			},
		},
	}); err != nil {
		logger.Warn(errors.Wrap(err, "origin error on websocket connection establishment"))
		s.CloseConnection(id)
	}

	return
}

type serviceConnectionHandler struct {
	s            *Service
	connectionId Id
	logger       logrus.FieldLogger
}

func (h *serviceConnectionHandler) HandleWebSocketMessage(msg *WebSocketMessage) {
	if _, err := h.s.Origin.ServeWebSocketService(&OriginRequest{
		WebSocketEvent: &WebSocketEvent{
			ConnectionId:    h.connectionId,
			MessageReceived: msg,
		},
	}); err != nil {
		h.logger.Warn(errors.Wrap(err, "origin error on websocket message"))
		h.s.CloseConnection(h.connectionId)
	}
}

func (s *Service) connectionHandler(connectionId Id, logger logrus.FieldLogger) ConnectionHandler {
	return &serviceConnectionHandler{
		s:            s,
		connectionId: connectionId,
		logger:       logger,
	}
}

func (s *Service) Close() error {
	var wg sync.WaitGroup

	s.connectionsMutex.Lock()
	errCh := make(chan error, len(s.connectionIds))
	for _, id := range s.connectionIds {
		id := id
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.CloseConnection(id); err != nil {
				errCh <- err
			}
		}()
	}
	s.connectionsMutex.Unlock()

	wg.Wait()

	var ret error
	for {
		select {
		case err := <-errCh:
			ret = multierror.Append(ret, err)
		default:
			return ret
		}
	}
}

func (s *Service) CloseConnection(id Id) error {
	s.init()

	s.connectionsMutex.Lock()
	connection, ok := s.connections[string(id)]
	s.connectionsMutex.Unlock()
	if !ok {
		return nil
	}

	if err := connection.Close(); err != nil {
		return err
	}

	s.connectionsMutex.Lock()
	defer s.connectionsMutex.Unlock()
	delete(s.connections, string(id))
	delete(s.connectionIds, connection)
	return nil
}
