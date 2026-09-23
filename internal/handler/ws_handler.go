package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"ridematch-backend/internal/utils"
	"ridematch-backend/internal/ws"
)

// upgrader configures the WebSocket handshake. CheckOrigin always allows:
// this API is consumed by a native mobile app (React Native), not a
// browser page, so there is no cross-origin browser context to defend
// against here — the connection is authorized by the JWT in the query
// string instead (see Connect below), the same trust boundary every
// other endpoint in this API uses.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// WSHandler upgrades authenticated HTTP requests to WebSocket connections
// for real-time trip event push.
type WSHandler struct {
	hub *ws.Hub
	jwt *utils.JWTManager
}

// NewWSHandler constructs a WSHandler.
func NewWSHandler(hub *ws.Hub, jwtManager *utils.JWTManager) *WSHandler {
	return &WSHandler{hub: hub, jwt: jwtManager}
}

// Connect godoc
//
//	@Summary		Open a real-time event connection
//	@Description	Upgrades to a WebSocket connection that pushes trip lifecycle events (new offers, match confirmed, pickup confirmed, trip completed/cancelled) the instant they happen, instead of the app having to poll. Authenticate by passing the access token as a query parameter, since standard WebSocket clients cannot set an Authorization header during the handshake: `wss://.../ws?token=<access_token>`. Every event pushed here is also readable via the corresponding REST endpoint — treat this as a low-latency notification channel, not the source of truth. Message envelope: `{"type": "trip.matched", "data": {...}}`.
//	@Tags			Realtime
//	@Param			token	query	string	true	"Access token"
//	@Success		101	{string}	string	"Switching Protocols"
//	@Failure		401	{object}	utils.APIResponse
//	@Router			/ws [get]
func (h *WSHandler) Connect(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		utils.Fail(c, http.StatusUnauthorized, "missing token query parameter")
		return
	}

	claims, err := h.jwt.ParseAccessToken(token)
	if err != nil {
		utils.Fail(c, http.StatusUnauthorized, "invalid or expired access token")
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		// Upgrade already wrote its own error response to the client.
		return
	}

	h.hub.Connect(claims.UserID, conn)
}
