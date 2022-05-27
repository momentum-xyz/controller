package socket

import (
	"sync"
	"time"

	"github.com/momentum-xyz/controller/internal/logger"
	"github.com/momentum-xyz/controller/utils"
	"github.com/momentum-xyz/posbus-protocol/posbus"

	"github.com/eapache/queue"
	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
)

const (
	writeWait = 10 * time.Second
	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second
	// send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10
	// Maximum message size allowed from peer.
	inMessageSizeLimit = 1024
	// maximal size of buffer in messages, after which we drop connection as not-working
	maxBufferSize = 10000
)

var log = logger.L()

type Connection struct {
	send      chan *websocket.PreparedMessage
	buffer    *queue.Queue
	OnReceive func(m *posbus.Message)
	OnPumpEnd func()
	stopChan  chan bool
	canWrite  utils.TAtomBool
	mu        *sync.Mutex
	conn      *websocket.Conn
	closed    bool
}

func OnReceiveStub(m *posbus.Message) {
	panic("implement me")
}

func OnPumpEndStub() {
	panic("implement me")
}

// func (this *Connection) CustomCloseHandler(code int, text string) error {
// 	logger.Logln(4, "Closed:", code, text)
// 	return errors.New("Close error")
// }

func (c *Connection) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return
	}
	c.closed = true

	log.Info("Closing connection and chan")
	close(c.send)
	c.conn.Close()
}

func (c *Connection) SetReceiveCallback(f func(m *posbus.Message)) {
	c.OnReceive = f
}

func (c *Connection) SetPumpEndCallback(f func()) {
	c.OnPumpEnd = f
}

func NewConnection(conn *websocket.Conn) *Connection {
	c := &Connection{
		send:      make(chan *websocket.PreparedMessage, 10),
		stopChan:  make(chan bool),
		buffer:    queue.New(),
		OnReceive: OnReceiveStub,
		OnPumpEnd: OnPumpEndStub,
		mu:        new(sync.Mutex),
		conn:      conn,
	}
	c.canWrite.Set(false)
	return c
}

func (c *Connection) StartReadPump() {
	c.conn.SetReadLimit(inMessageSizeLimit)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(
		func(string) error {
			c.conn.SetReadDeadline(time.Now().Add(pongWait))
			return nil
		},
	)
	log.Debug("Starting read pump")
	for {
		messageType, message, err := c.conn.ReadMessage()
		if err != nil {
			if ce, ok := err.(*websocket.CloseError); ok {
				switch ce.Code {
				case websocket.CloseNormalClosure,
					websocket.CloseGoingAway,
					websocket.CloseNoStatusReceived:
					log.Infof("websocket closed by client: %v", err)
					return
				}
			}
			log.Errorf("error: reading message fron connection: %v", err)
			break
		}
		if messageType != websocket.BinaryMessage {
			log.Error("error: wrong incoming message type")
		} else {
			c.OnReceive(posbus.MsgFromBytes(message))
		}
	}
	c.stopChan <- true
	c.conn.Close()
	log.Debug("End of read")

}

func (c *Connection) EnableWriting() {
	c.canWrite.Set(true)
}

func (c *Connection) StartWritePump() {
	pingTicker := time.NewTicker(pingPeriod)
	defer func() {
		c.OnPumpEnd()
		pingTicker.Stop()
		// c.conn.Close()
		log.Info("End of IO pump")
	}()

	log.Info("Starting WritePump")

	// send goroutine
	for {
		select {
		case message, ok := <-c.send:
			if !ok {
				c.conn.SetWriteDeadline(time.Now().Add(writeWait))
				if err := c.conn.WriteMessage(websocket.CloseMessage, []byte{}); err != nil {
					log.Error(err)
				}
				return
			}
			if c.canWrite.Get() {
				for c.buffer.Length() > 0 {
					if err := c.SendDirectly(c.buffer.Remove().(*websocket.PreparedMessage)); err != nil {
						return
					}
				}
				if err := c.SendDirectly(message); err != nil {
					return
				}
			} else {
				if c.buffer.Length() < maxBufferSize {
					c.buffer.Add(message)
				} else {
					log.Error(errors.New("Buffer full, dropping connection!"))
					return
				}
			}
		case <-pingTicker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
			if c.canWrite.Get() {
				for c.buffer.Length() > 0 {
					if err := c.SendDirectly(c.buffer.Remove().(*websocket.PreparedMessage)); err != nil {
						return
					}
				}
			}
		case <-c.stopChan:
			return
		}
	}
}

func (c *Connection) Send(m *websocket.PreparedMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return
	}
	c.send <- m
}

func (c *Connection) SendDirectly(m *websocket.PreparedMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}

	if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
		return errors.WithMessage(err, "failed to set write deadline")
	}
	if err := c.conn.WritePreparedMessage(m); err != nil {
		return errors.WithMessage(err, "failed to write prepared message")
	}

	return nil
}
