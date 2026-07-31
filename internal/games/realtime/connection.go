package realtime

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 4096
	sendBuffer     = 32
)

// Client is a single WebSocket connection watching a lobby.
type Client struct {
	conn    *websocket.Conn
	userID  uuid.UUID
	lobbyID uuid.UUID
	gameKey string
	send    chan []byte
	closed  chan struct{}
	once    sync.Once
}

func newClient(conn *websocket.Conn, userID, lobbyID uuid.UUID, gameKey string) *Client {
	return &Client{
		conn:    conn,
		userID:  userID,
		lobbyID: lobbyID,
		gameKey: gameKey,
		send:    make(chan []byte, sendBuffer),
		closed:  make(chan struct{}),
	}
}

// sendJSON queues a message for delivery, dropping it (and closing the client)
// if the buffer is full — a slow/stuck client must not block the hub.
func (c *Client) sendJSON(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	select {
	case c.send <- b:
	case <-c.closed:
	default:
		c.close()
	}
}

func (c *Client) close() {
	c.once.Do(func() {
		close(c.closed)
		_ = c.conn.Close()
	})
}

// writePump delivers queued messages and sends periodic pings.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.close()
	}()
	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-c.closed:
			return
		}
	}
}

// readPump reads client messages until the socket closes, invoking onMessage
// for each. It runs on the handler goroutine and returns on disconnect.
func (c *Client) readPump(onMessage func(ClientMessage)) {
	defer c.close()
	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		var msg ClientMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			c.sendJSON(ServerMessage{Type: "error", Error: "malformed message"})
			continue
		}
		onMessage(msg)
	}
}
