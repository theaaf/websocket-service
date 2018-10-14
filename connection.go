package websocketservice

import (
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

type Connection struct {
	id     []byte
	conn   *websocket.Conn
	logger logrus.FieldLogger
}
