package websocketservice

import (
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type Service struct {
	// The subprotocols to advertise during WebSocket negotiation.
	Subprotocols []string

	// Origin provides a means of sending messages to the origin server.
	Origin Origin

	// Cluster provides a means of sending messages to other nodes in the cluster.
	Cluster Cluster

	// If non-zero, the origin will receive keep-alive events for WebSocket connections. These
	// events don't represent any actual WebSockett activity, but indicate that the connection is
	// still alive and healthy.
	KeepAliveInterval time.Duration

	// The logger to use. If nil, the standard logger will be used.
	Logger logrus.FieldLogger

	initOnce sync.Once

	address Address

	connectionsMutex sync.Mutex
	connections      map[string]*Connection
	connectionIds    map[*Connection]ConnectionId
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
			s.connectionIds = make(map[*Connection]ConnectionId)
		}
		if s.Logger == nil {
			s.Logger = logrus.StandardLogger()
		}
		s.address = s.Cluster.Address()
	})
}

func (s *Service) serveWebSocket(w http.ResponseWriter, r *http.Request) {
	var upgrader = websocket.Upgrader{
		CheckOrigin:       func(r *http.Request) bool { return true },
		EnableCompression: true,
		Subprotocols:      s.Subprotocols,
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	id, err := NewConnectionId(s.address)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.connectionsMutex.Lock()
	logger := s.Logger.WithField("connection_id", id)
	logger.WithField("connection_count", len(s.connections)+1).Info("connection established")
	connection := NewConnection(conn, logger, s.connectionHandler(id, logger), s.KeepAliveInterval)
	s.connections[string(id)] = connection
	s.connectionIds[connection] = id
	s.connectionsMutex.Unlock()

	if err := s.Origin.SendOriginRequest(&OriginRequest{
		WebSocketEvent: &WebSocketEvent{
			ConnectionId: id,
			ConnectionEstablished: &WebSocketEventConnectionEstablished{
				Subprotocol: conn.Subprotocol(),
			},
		},
	}); err != nil {
		logger.Warn(errors.Wrap(err, "origin error on websocket connection establishment"))
		s.closeConnection(id)
	}

	return
}

type serviceConnectionHandler struct {
	s            *Service
	connectionId ConnectionId
	logger       logrus.FieldLogger
}

func (h *serviceConnectionHandler) KeepAlive() {
	if err := h.s.Origin.SendOriginRequest(&OriginRequest{
		WebSocketEvent: &WebSocketEvent{
			ConnectionId: h.connectionId,
			KeepAlive:    &WebSocketKeepAlive{},
		},
	}); err != nil {
		h.logger.Warn(errors.Wrap(err, "origin error on websocket keep-alive"))
	}
}

func (h *serviceConnectionHandler) HandleWebSocketMessage(msg *WebSocketMessage) {
	if err := h.s.Origin.SendOriginRequest(&OriginRequest{
		WebSocketEvent: &WebSocketEvent{
			ConnectionId:    h.connectionId,
			MessageReceived: msg,
		},
	}); err != nil {
		h.logger.Warn(errors.Wrap(err, "origin error on websocket message"))
		h.s.closeConnection(h.connectionId)
	}
}

func (h *serviceConnectionHandler) HandleClose() {
	h.s.CloseConnection(h.connectionId)
}

func (s *Service) connectionHandler(connectionId ConnectionId, logger logrus.FieldLogger) ConnectionHandler {
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

func (s *Service) ConnectionCount() int {
	s.connectionsMutex.Lock()
	defer s.connectionsMutex.Unlock()
	return len(s.connections)
}

func (s *Service) CloseConnection(id ConnectionId) error {
	if didClose, err := s.closeConnection(id); err != nil {
		return err
	} else if didClose {
		if err := s.Origin.SendOriginRequest(&OriginRequest{
			WebSocketEvent: &WebSocketEvent{
				ConnectionId:     id,
				ConnectionClosed: &WebSocketEventConnectionClosed{},
			},
		}); err != nil {
			s.Logger.WithField("connection_id", id).Warn(errors.Wrap(err, "origin error on websocket connection closed"))
		}
	}
	return nil
}

func (s *Service) closeConnection(id ConnectionId) (bool, error) {
	s.init()

	s.connectionsMutex.Lock()
	connection, ok := s.connections[string(id)]
	s.connectionsMutex.Unlock()
	if !ok {
		return false, nil
	}

	if err := connection.Close(); err != nil {
		return false, err
	}

	s.connectionsMutex.Lock()
	defer s.connectionsMutex.Unlock()
	if connection, ok := s.connections[string(id)]; ok {
		delete(s.connections, string(id))
		delete(s.connectionIds, connection)
		s.Logger.WithFields(logrus.Fields{
			"connection_count": len(s.connections),
			"connection_id":    id,
		}).Info("connection closed")
		return true, nil
	}
	return false, nil
}

func (s *Service) HandleServiceRequest(r *ServiceRequest) {
	s.init()

	requestsToForward := map[string]*ServiceRequest{}

	s.connectionsMutex.Lock()
	for _, msg := range r.OutgoingWebSocketMessages {
		preparedMessage, err := msg.Message.PreparedMessage()
		if err != nil {
			s.Logger.Warn(errors.Wrap(err, "invalid message in service request"))
			continue
		}
		forwarded := map[string][]ConnectionId{}
		for _, id := range msg.ConnectionIds {
			if id.Address().Equal(s.address) {
				if conn, ok := s.connections[string(id)]; ok {
					conn.Send(preparedMessage)
				}
				continue
			}
			forwarded[string(id.Address())] = append(forwarded[string(id.Address())], id)
		}
		for address, ids := range forwarded {
			forwardedRequest, ok := requestsToForward[address]
			if !ok {
				forwardedRequest = &ServiceRequest{}
				requestsToForward[address] = forwardedRequest
			}
			forwardedRequest.OutgoingWebSocketMessages = append(forwardedRequest.OutgoingWebSocketMessages, &OutgoingWebSocketMessage{
				ConnectionIds: ids,
				Message:       msg.Message,
			})
		}
	}
	s.connectionsMutex.Unlock()

	var wg sync.WaitGroup
	for address, request := range requestsToForward {
		address := address
		request := request
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Cluster.SendServiceRequest(Address(address), request); err != nil {
				s.Logger.Warn(errors.Wrap(err, "unable to forward service request"))
			}
		}()
	}
	wg.Wait()
}

type ServiceRequest struct {
	OutgoingWebSocketMessages []*OutgoingWebSocketMessage
}

type OutgoingWebSocketMessage struct {
	ConnectionIds []ConnectionId
	Message       *WebSocketMessage
}
