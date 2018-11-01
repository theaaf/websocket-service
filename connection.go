package websocketservice

import (
	"crypto/rand"
	"encoding/base64"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type ConnectionId []byte

const (
	connectionIdVersion0    = 0
	connectionIdRandomBytes = 20
)

func NewConnectionId(address Address) (ConnectionId, error) {
	id := make([]byte, 1+len(address)+connectionIdRandomBytes)
	id[0] = connectionIdVersion0
	copy(id[1:], address)
	if _, err := rand.Read(id[1+len(address):]); err != nil {
		return nil, err
	}
	return id, nil
}

func (id ConnectionId) Address() Address {
	if id[0] != connectionIdVersion0 || len(id) < 1+connectionIdRandomBytes {
		return nil
	}
	return Address(id[1 : len(id)-connectionIdRandomBytes])
}

func (id ConnectionId) String() string {
	return base64.RawURLEncoding.EncodeToString(id)
}

type Connection struct {
	conn              *websocket.Conn
	logger            logrus.FieldLogger
	readLoopDone      chan struct{}
	writeLoopDone     chan struct{}
	outgoing          chan *websocket.PreparedMessage
	closing           chan struct{}
	handler           ConnectionHandler
	keepAliveInterval time.Duration
}

type ConnectionHandler interface {
	// KeepAlive will be invoked periodically according to the connection's specified keep-alive
	// interval.
	KeepAlive()

	HandleWebSocketMessage(msg *WebSocketMessage)
}

const connectionSendBufferSize = 100

func NewConnection(conn *websocket.Conn, logger logrus.FieldLogger, handler ConnectionHandler, keepAliveInterval time.Duration) *Connection {
	ret := &Connection{
		conn:              conn,
		logger:            logger,
		readLoopDone:      make(chan struct{}),
		writeLoopDone:     make(chan struct{}),
		outgoing:          make(chan *websocket.PreparedMessage, connectionSendBufferSize),
		closing:           make(chan struct{}),
		handler:           handler,
		keepAliveInterval: keepAliveInterval,
	}
	go ret.readLoop()
	go ret.writeLoop()
	return ret
}

func (c *Connection) Send(msg *websocket.PreparedMessage) {
	select {
	case c.outgoing <- msg:
	default:
		c.logger.Warn("dropping outgoing websocket message")
	}
}

func (c *Connection) Close() error {
	close(c.outgoing)
	close(c.closing)
	<-c.readLoopDone
	<-c.writeLoopDone
	return nil
}

func (c *Connection) readLoop() {
	defer close(c.readLoopDone)

	for {
		messageType, p, err := c.conn.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseAbnormalClosure, websocket.CloseGoingAway) {
				select {
				case <-c.closing:
				default:
					c.logger.Error(errors.Wrap(err, "websocket read error"))
				}
			}
			return
		}

		if messageType == websocket.TextMessage {
			text := string(p)
			c.handler.HandleWebSocketMessage(&WebSocketMessage{
				Text: &text,
			})
		} else if messageType == websocket.BinaryMessage {
			c.handler.HandleWebSocketMessage(&WebSocketMessage{
				Binary: p,
			})
		}
	}
}

func (c *Connection) writeLoop() {
	defer close(c.writeLoopDone)

	defer c.conn.Close()

	var tick <-chan time.Time

	if c.keepAliveInterval > 0 {
		ticker := time.NewTicker(c.keepAliveInterval)
		tick = ticker.C
		defer ticker.Stop()
	}

	for {
		select {
		case msg, ok := <-c.outgoing:
			if !ok {
				return
			}

			c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))

			if err := c.conn.WritePreparedMessage(msg); err != nil {
				if !websocket.IsCloseError(err, websocket.CloseAbnormalClosure, websocket.CloseGoingAway) && err != websocket.ErrCloseSent {
					c.logger.Error(errors.Wrap(err, "websocket write error"))
				}
				break
			}
		case <-tick:
			c.handler.KeepAlive()
		}
	}
}
