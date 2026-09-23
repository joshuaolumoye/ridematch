package ws

import (
	"encoding/json"
	"sync"

	"github.com/gorilla/websocket"
)

// Event is the JSON envelope every pushed message uses:
// {"type": "trip.offer_received", "data": {...}}. Handlers/services never
// build raw JSON by hand — they call Hub.SendToUser with a typed Event.
type Event struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// Event type constants — the vocabulary the React Native app listens for.
const (
	EventNewTripRequest = "trip.new_request"    // -> nearby drivers, on trip creation
	EventOfferReceived  = "trip.offer_received" // -> passenger, on new driver offer
	EventOfferRejected  = "trip.offer_rejected" // -> driver, when passenger rejects
	EventTripMatched    = "trip.matched"        // -> passenger and matched driver
	EventTripClosed     = "trip.closed"         // -> other offering drivers, once matched/cancelled
	EventTripPickedUp   = "trip.picked_up"      // -> passenger, on PIN confirmation
	EventTripCompleted  = "trip.completed"      // -> passenger, on completion
	EventTripCancelled  = "trip.cancelled"      // -> the other party, on cancellation
)

// Hub tracks one live connection per user and routes targeted pushes to
// them. A new connection for a user replaces any existing one (a user is
// assumed to have at most one active app session).
type Hub struct {
	mu      sync.RWMutex
	clients map[string]*Client
}

// NewHub constructs an empty Hub.
func NewHub() *Hub {
	return &Hub{clients: make(map[string]*Client)}
}

// Connect registers a new authenticated connection for a user and starts
// its read/write pumps. Returns the Client so callers (tests, mainly)
// can hold a reference if needed.
func (h *Hub) Connect(userID string, conn *websocket.Conn) *Client {
	client := &Client{
		UserID: userID,
		hub:    h,
		conn:   conn,
		send:   make(chan []byte, sendBufferSize),
	}

	h.mu.Lock()
	if old, ok := h.clients[userID]; ok {
		// Replacing an existing session: close its send channel so its
		// writePump exits cleanly rather than leaking a goroutine.
		close(old.send)
	}
	h.clients[userID] = client
	h.mu.Unlock()

	go client.writePump()
	go client.readPump()

	return client
}

// unregister removes a client, but only if it's still the current
// connection for that user — prevents a stale disconnect from evicting a
// newer connection that already replaced it.
func (h *Hub) unregister(c *Client) {
	h.mu.Lock()
	if current, ok := h.clients[c.UserID]; ok && current == c {
		delete(h.clients, c.UserID)
	}
	h.mu.Unlock()
}

// SendToUser pushes an event to a user's live connection, if any.
// Delivery is best-effort and non-blocking: if the user isn't connected,
// or their outbound buffer is full (a slow/stalled client), the event is
// dropped rather than blocking the caller — callers must never depend on
// a push as the only way state reaches the client (REST is the source of
// truth). Returns whether a live connection actually received it, purely
// for logging/metrics.
func (h *Hub) SendToUser(userID string, event Event) bool {
	payload, err := json.Marshal(event)
	if err != nil {
		return false
	}

	h.mu.RLock()
	client, ok := h.clients[userID]
	h.mu.RUnlock()
	if !ok {
		return false
	}

	select {
	case client.send <- payload:
		return true
	default:
		return false
	}
}

// IsOnline reports whether a user currently has a live WebSocket
// connection.
func (h *Hub) IsOnline(userID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.clients[userID]
	return ok
}
