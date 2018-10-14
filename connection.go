package websocketservice

import (
	"time"

	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type Connection struct {
	conn          *websocket.Conn
	logger        logrus.FieldLogger
	readLoopDone  chan struct{}
	writeLoopDone chan struct{}
	outgoing      chan *websocket.PreparedMessage
	closing       chan struct{}
	handler       ConnectionHandler
}

type ConnectionHandler interface {
	HandleWebSocketMessage(msg *WebSocketMessage)
}

func NewConnection(conn *websocket.Conn, logger logrus.FieldLogger, handler ConnectionHandler) *Connection {
	ret := &Connection{
		conn:          conn,
		logger:        logger,
		readLoopDone:  make(chan struct{}),
		writeLoopDone: make(chan struct{}),
		outgoing:      make(chan *websocket.PreparedMessage, 10),
		closing:       make(chan struct{}),
		handler:       handler,
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

	for {
		msg, ok := <-c.outgoing
		if !ok {
			break
		}

		c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))

		if err := c.conn.WritePreparedMessage(msg); err != nil {
			if !websocket.IsCloseError(err, websocket.CloseAbnormalClosure, websocket.CloseGoingAway) && err != websocket.ErrCloseSent {
				c.logger.Error(errors.Wrap(err, "websocket write error"))
			}
			break
		}
	}
}
