package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"ridematch-backend/internal/middleware"
	"ridematch-backend/internal/service"
	"ridematch-backend/internal/utils"
)

// NotificationHandler exposes the authenticated caller's own in-app
// notifications inbox — riders, drivers, and admins alike each see only
// their own notifications, scoped by the JWT's user ID.
type NotificationHandler struct {
	notifications *service.NotificationService
}

// NewNotificationHandler constructs a NotificationHandler.
func NewNotificationHandler(notifications *service.NotificationService) *NotificationHandler {
	return &NotificationHandler{notifications: notifications}
}

// List godoc
//
//	@Summary		List the authenticated user's notifications
//	@Tags			Notifications
//	@Produce		json
//	@Security		BearerAuth
//	@Param			page		query		int	false	"page number (default 1)"
//	@Param			page_size	query		int	false	"items per page (default 20, max 100)"
//	@Success		200	{object}	utils.APIResponse{data=dto.NotificationListResponse}
//	@Router			/notifications [get]
func (h *NotificationHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))

	resp, err := h.notifications.List(c.Request.Context(), middleware.UserIDFromContext(c), page, pageSize)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "notifications loaded", resp)
}

// UnreadCount godoc
//
//	@Summary		Just the unread notification count, for a badge
//	@Tags			Notifications
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	utils.APIResponse{data=dto.UnreadNotificationCountResponse}
//	@Router			/notifications/unread-count [get]
func (h *NotificationHandler) UnreadCount(c *gin.Context) {
	count, err := h.notifications.UnreadCount(c.Request.Context(), middleware.UserIDFromContext(c))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "unread count loaded", gin.H{"unread_count": count})
}

// MarkRead godoc
//
//	@Summary		Mark one notification read
//	@Tags			Notifications
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string	true	"Notification ID"
//	@Success		200	{object}	utils.APIResponse
//	@Failure		404	{object}	utils.APIResponse
//	@Router			/notifications/{id}/read [patch]
func (h *NotificationHandler) MarkRead(c *gin.Context) {
	if err := h.notifications.MarkRead(c.Request.Context(), c.Param("id"), middleware.UserIDFromContext(c)); err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "notification marked read", nil)
}

// MarkAllRead godoc
//
//	@Summary		Mark every notification read
//	@Tags			Notifications
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	utils.APIResponse
//	@Router			/notifications/read-all [post]
func (h *NotificationHandler) MarkAllRead(c *gin.Context) {
	if err := h.notifications.MarkAllRead(c.Request.Context(), middleware.UserIDFromContext(c)); err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "all notifications marked read", nil)
}
