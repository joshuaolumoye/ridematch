package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"ridematch-backend/internal/dto"
	"ridematch-backend/internal/service"
	"ridematch-backend/internal/utils"
)

// AdminHandler exposes staff-only endpoints: driver verification and
// subscription activation. Every route here is mounted behind
// middleware.RequireRole("admin") — see internal/router.
type AdminHandler struct {
	admin *service.AdminService
}

// NewAdminHandler constructs an AdminHandler.
func NewAdminHandler(admin *service.AdminService) *AdminHandler {
	return &AdminHandler{admin: admin}
}

// VerifyDriver godoc
//
//	@Summary		Approve or reject a driver's documents
//	@Tags			Admin
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string							true	"Driver profile ID"
//	@Param			request	body		dto.AdminVerifyDriverRequest	true	"Decision"
//	@Success		200		{object}	utils.APIResponse{data=dto.DriverProfileResponse}
//	@Failure		403		{object}	utils.APIResponse	"Caller is not an admin"
//	@Failure		404		{object}	utils.APIResponse
//	@Router			/admin/drivers/{id}/verify [patch]
func (h *AdminHandler) VerifyDriver(c *gin.Context) {
	driverID := c.Param("id")

	var req dto.AdminVerifyDriverRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, http.StatusBadRequest, "approve (bool) is required")
		return
	}

	resp, err := h.admin.VerifyDriver(c.Request.Context(), driverID, req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "driver verification updated", resp)
}

// ActivateSubscription godoc
//
//	@Summary		Manually activate/extend a driver's subscription
//	@Description	Stand-in for the Flutterwave payment webhook (not yet wired up) — grants the driver N days of platform access from now, or extends from their current expiry if still active.
//	@Tags			Admin
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string								true	"Driver profile ID"
//	@Param			request	body		dto.AdminActivateSubscriptionRequest	true	"Number of days to grant"
//	@Success		200		{object}	utils.APIResponse{data=dto.DriverProfileResponse}
//	@Failure		403		{object}	utils.APIResponse	"Caller is not an admin"
//	@Failure		404		{object}	utils.APIResponse
//	@Router			/admin/drivers/{id}/subscription [patch]
func (h *AdminHandler) ActivateSubscription(c *gin.Context) {
	driverID := c.Param("id")

	var req dto.AdminActivateSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, http.StatusBadRequest, "days (1-365) is required")
		return
	}

	resp, err := h.admin.ActivateSubscription(c.Request.Context(), driverID, req.Days)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "subscription activated", resp)
}
