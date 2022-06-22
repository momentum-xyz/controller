package socket

import (
	"github.com/sasha-s/go-deadlock"
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
	canWrite  utils.TAtomBool
	mu        *deadlock.RWMutex
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
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.mu.Unlock()

	log.Info("Connection: closing connection and chan")
	close(c.send)
	c.conn.SetWriteDeadline(time.Now().Add(writeWait))
	c.conn.WriteMessage(websocket.CloseMessage, []byte{})
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
		buffer:    queue.New(),
		OnReceive: OnReceiveStub,
		OnPumpEnd: OnPumpEndStub,
		mu:        new(deadlock.RWMutex),
		conn:      conn,
	}
	c.canWrite.Set(false)
	return c
}

func (c *Connection) StartReadPump() {
	defer func() {
		c.Close()
		log.Info("Connection: end of read pump")
	}()

	c.conn.SetReadLimit(inMessageSizeLimit)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(
		func(string) error {
			return c.conn.SetReadDeadline(time.Now().Add(pongWait))
		},
	)

	log.Info("Connection: starting read pump")
	for {
		messageType, message, err := c.conn.ReadMessage()
		if err != nil {
			if ce, ok := err.(*websocket.CloseError); ok {
				switch ce.Code {
				case websocket.CloseNormalClosure,
					websocket.CloseGoingAway,
					websocket.CloseNoStatusReceived:
					log.Info(errors.WithMessagef(err, "Connection: read pump: websocket closed by client"))
					return
				}
			}
			log.Debug(errors.WithMessage(err, "Connection: read pump: failed to read message from connection"))
			return
		}
		if messageType != websocket.BinaryMessage {
			log.Errorf("Connection: read pump: wrong incoming message type: %d", messageType)
		} else {
			c.OnReceive(posbus.MsgFromBytes(message))
		}
	}
}

func (c *Connection) EnableWriting() {
	c.canWrite.Set(true)
}

func (c *Connection) StartWritePump() {
	pingTicker := time.NewTicker(pingPeriod)
	defer func() {
		pingTicker.Stop()
		c.Close()
		c.OnPumpEnd()
		log.Info("Connection: end of write pump")
	}()

	log.Info("Connection: starting write pump")
	for {
		select {
		case message, ok := <-c.send:
			if !ok {
				log.Debug("Connection: write pump: chan closed")
				return
			}
			if c.canWrite.Get() {
				for c.buffer.Length() > 0 {
					if err := c.SendDirectly(utils.GetFromAny[*websocket.PreparedMessage](c.buffer.Remove(), nil)); err != nil {
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
					log.Error(errors.New("Connection: write pump:: buffer full, dropping connection!"))
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
		}
	}
}

func (c *Connection) Send(m *websocket.PreparedMessage) {
	if m == nil {
		return
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return
	}
	c.send <- m
}

func (c *Connection) SendDirectly(m *websocket.PreparedMessage) error {
	if m == nil {
		return nil
	}

	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return nil
	}
	c.mu.RUnlock()

	if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
		return errors.WithMessage(err, "failed to set write deadline")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.conn.WritePreparedMessage(m); err != nil {
		return errors.WithMessage(err, "failed to write prepared message")
	}

	return nil
}
