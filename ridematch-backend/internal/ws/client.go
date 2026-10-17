// Package ws implements a lightweight WebSocket push layer: one
// connection per authenticated user, used to deliver trip lifecycle
// events (new offers, match confirmation, pickup, completion) instantly
// instead of the client having to poll REST endpoints. REST remains the
// source of truth for all state — a push that fails to deliver (client
// offline, buffer full) is never the only way a client learns something
// happened; every event is also readable via the corresponding REST
// resource (GET /trips/:id, etc.).
package ws

import (
	"time"

	"github.com/gorilla/websocket"
)

const (
	// writeWait is the max time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// pongWait is how long we wait for a pong before considering the
	// connection dead.
	pongWait = 60 * time.Second

	// pingPeriod must be less than pongWait; pings are sent on this
	// interval to keep the connection alive and detect dead peers early.
	pingPeriod = (pongWait * 9) / 10

	// maxMessageSize caps incoming messages. Clients aren't expected to
	// send application data over this socket (it's server push only), so
	// this just bounds accidental/malicious oversized frames.
	maxMessageSize = 512

	// sendBufferSize is how many outbound events can queue per client
	// before a slow consumer starts having pushes dropped rather than
	// blocking the sender.
	sendBufferSize = 16
)

// Client wraps one authenticated user's live WebSocket connection.
type Client struct {
	UserID string

	hub  *Hub
	conn *websocket.Conn
	send chan []byte
}

// writePump serializes all writes to the connection (gorilla/websocket
// requires at most one concurrent writer) and sends periodic pings.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// readPump keeps the connection's read side pumping so pong frames (and
// the close handshake) are processed, and detects a dead/closed
// connection so the client can be unregistered promptly. Inbound
// application messages, if any arrive, are discarded — this channel is
// server-to-client push only in v1.
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister(c)
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			break
		}
	}
}
