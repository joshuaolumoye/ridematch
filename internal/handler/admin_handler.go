package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"ridematch-backend/internal/dto"
	"ridematch-backend/internal/repository"
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

// Overview godoc
//
//	@Summary		Dashboard overview: everything going on across the platform
//	@Tags			Admin
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	utils.APIResponse{data=dto.AdminOverviewResponse}
//	@Failure		403	{object}	utils.APIResponse	"Caller is not an admin"
//	@Router			/admin/overview [get]
func (h *AdminHandler) Overview(c *gin.Context) {
	resp, err := h.admin.Overview(c.Request.Context())
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "overview loaded", resp)
}

// ListDrivers godoc
//
//	@Summary		List registered riders, filterable by vehicle type
//	@Tags			Admin
//	@Produce		json
//	@Security		BearerAuth
//	@Param			vehicle_type		query		string	false	"car | okada | keke | bus"
//	@Param			verification_status	query		string	false	"pending | approved | rejected"
//	@Param			online				query		bool	false	"filter by online status"
//	@Param			q					query		string	false	"search name, phone, email, or plate number"
//	@Param			page				query		int		false	"page number (default 1)"
//	@Param			page_size			query		int		false	"items per page (default 20, max 100)"
//	@Success		200	{object}	utils.APIResponse{data=dto.AdminDriverListResponse}
//	@Failure		403	{object}	utils.APIResponse	"Caller is not an admin"
//	@Router			/admin/drivers [get]
func (h *AdminHandler) ListDrivers(c *gin.Context) {
	filter := repository.DriverFilter{
		VehicleType:        c.Query("vehicle_type"),
		VerificationStatus: c.Query("verification_status"),
		Query:              c.Query("q"),
	}
	if raw := c.Query("online"); raw != "" {
		if b, err := strconv.ParseBool(raw); err == nil {
			filter.Online = &b
		}
	}
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))

	resp, err := h.admin.ListDrivers(c.Request.Context(), filter, page, pageSize)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "drivers loaded", resp)
}

// GetDriver godoc
//
//	@Summary		A rider's detail page
//	@Tags			Admin
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string	true	"Driver profile ID"
//	@Success		200	{object}	utils.APIResponse{data=dto.AdminDriverDetailResponse}
//	@Failure		403	{object}	utils.APIResponse	"Caller is not an admin"
//	@Failure		404	{object}	utils.APIResponse
//	@Router			/admin/drivers/{id} [get]
func (h *AdminHandler) GetDriver(c *gin.Context) {
	resp, err := h.admin.GetDriver(c.Request.Context(), c.Param("id"))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "driver loaded", resp)
}

// NotifyDriver godoc
//
//	@Summary		Notify a driver about their submitted documents
//	@Description	Sends the driver an SMS and/or email (whichever the account has on file) — e.g. asking them to resubmit an unclear document.
//	@Tags			Admin
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string							true	"Driver profile ID"
//	@Param			request	body		dto.AdminNotifyDriverRequest	true	"Message to send"
//	@Success		200		{object}	utils.APIResponse{data=dto.AdminNotifyDriverResponse}
//	@Failure		403		{object}	utils.APIResponse	"Caller is not an admin"
//	@Failure		404		{object}	utils.APIResponse
//	@Router			/admin/drivers/{id}/notify [post]
func (h *AdminHandler) NotifyDriver(c *gin.Context) {
	var req dto.AdminNotifyDriverRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, http.StatusBadRequest, "message is required")
		return
	}

	resp, err := h.admin.NotifyDriver(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "notification sent", resp)
}

// DriverLocations godoc
//
//	@Summary		Live positions of every online driver, for the riders-by-location map
//	@Tags			Admin
//	@Produce		json
//	@Security		BearerAuth
//	@Param			vehicle_type	query		string	false	"car | okada | keke | bus (all types if omitted)"
//	@Success		200	{object}	utils.APIResponse{data=[]dto.AdminDriverLocationItem}
//	@Failure		403	{object}	utils.APIResponse	"Caller is not an admin"
//	@Router			/admin/drivers/locations [get]
func (h *AdminHandler) DriverLocations(c *gin.Context) {
	resp, err := h.admin.DriverLocations(c.Request.Context(), c.Query("vehicle_type"))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "driver locations loaded", resp)
}

// ListUsers godoc
//
//	@Summary		List all registered users, filterable by role and status
//	@Tags			Admin
//	@Produce		json
//	@Security		BearerAuth
//	@Param			role		query		string	false	"user | driver | admin"
//	@Param			status		query		string	false	"active | suspended | banned"
//	@Param			q			query		string	false	"search name, phone, or email"
//	@Param			page		query		int		false	"page number (default 1)"
//	@Param			page_size	query		int		false	"items per page (default 20, max 100)"
//	@Success		200	{object}	utils.APIResponse{data=dto.AdminUserListResponse}
//	@Failure		403	{object}	utils.APIResponse	"Caller is not an admin"
//	@Router			/admin/users [get]
func (h *AdminHandler) ListUsers(c *gin.Context) {
	filter := repository.UserFilter{
		Role:   c.Query("role"),
		Status: c.Query("status"),
		Query:  c.Query("q"),
	}
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))

	resp, err := h.admin.ListUsers(c.Request.Context(), filter, page, pageSize)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "users loaded", resp)
}

// GetUser godoc
//
//	@Summary		A user's detail page
//	@Tags			Admin
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string	true	"User ID"
//	@Success		200	{object}	utils.APIResponse{data=dto.AdminUserDetailResponse}
//	@Failure		403	{object}	utils.APIResponse	"Caller is not an admin"
//	@Failure		404	{object}	utils.APIResponse
//	@Router			/admin/users/{id} [get]
func (h *AdminHandler) GetUser(c *gin.Context) {
	resp, err := h.admin.GetUser(c.Request.Context(), c.Param("id"))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "user loaded", resp)
}

// UserTrips godoc
//
//	@Summary		A user's bookings (as passenger, and as driver if applicable)
//	@Tags			Admin
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id			path		string	true	"User ID"
//	@Param			page		query		int		false	"page number (default 1)"
//	@Param			page_size	query		int		false	"items per page (default 20, max 100)"
//	@Success		200	{object}	utils.APIResponse{data=dto.AdminTripListResponse}
//	@Failure		403	{object}	utils.APIResponse	"Caller is not an admin"
//	@Failure		404	{object}	utils.APIResponse
//	@Router			/admin/users/{id}/trips [get]
func (h *AdminHandler) UserTrips(c *gin.Context) {
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))

	resp, err := h.admin.UserTrips(c.Request.Context(), c.Param("id"), page, pageSize)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "user trips loaded", resp)
}

// UpdateAccountStatus godoc
//
//	@Summary		Suspend, reactivate, or ban a user account
//	@Description	Operates on the underlying account, so it applies to both plain passengers and driver accounts — suspending a driver blocks them from riding and driving alike.
//	@Tags			Admin
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string									true	"User ID"
//	@Param			request	body		dto.AdminUpdateAccountStatusRequest	true	"New status"
//	@Success		200		{object}	utils.APIResponse{data=dto.AdminUserListItem}
//	@Failure		403		{object}	utils.APIResponse	"Caller is not an admin"
//	@Failure		404		{object}	utils.APIResponse
//	@Router			/admin/users/{id}/status [patch]
func (h *AdminHandler) UpdateAccountStatus(c *gin.Context) {
	var req dto.AdminUpdateAccountStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, http.StatusBadRequest, "a valid status (active, suspended, or banned) is required")
		return
	}

	resp, err := h.admin.UpdateAccountStatus(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "account status updated", resp)
}

// ListTrips godoc
//
//	@Summary		List all bookings platform-wide, filterable by status/vehicle type/date
//	@Tags			Admin
//	@Produce		json
//	@Security		BearerAuth
//	@Param			status			query		string	false	"requested | matched | picked_up | completed | cancelled | expired"
//	@Param			vehicle_type	query		string	false	"car | okada | keke | bus"
//	@Param			q				query		string	false	"search passenger or driver name"
//	@Param			from			query		string	false	"RFC3339 start date"
//	@Param			to				query		string	false	"RFC3339 end date"
//	@Param			page			query		int		false	"page number (default 1)"
//	@Param			page_size		query		int		false	"items per page (default 20, max 100)"
//	@Success		200	{object}	utils.APIResponse{data=dto.AdminTripListResponse}
//	@Failure		403	{object}	utils.APIResponse	"Caller is not an admin"
//	@Router			/admin/trips [get]
func (h *AdminHandler) ListTrips(c *gin.Context) {
	filter := repository.TripFilter{
		Status:      c.Query("status"),
		VehicleType: c.Query("vehicle_type"),
		Query:       c.Query("q"),
	}
	if raw := c.Query("from"); raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			filter.From = &t
		}
	}
	if raw := c.Query("to"); raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			filter.To = &t
		}
	}
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))

	resp, err := h.admin.ListTrips(c.Request.Context(), filter, page, pageSize)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "trips loaded", resp)
}

// ListSubscriptionPrices godoc
//
//	@Summary		List the daily platform-access price for every vehicle type
//	@Tags			Admin
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	utils.APIResponse{data=[]dto.AdminSubscriptionPriceItem}
//	@Failure		403	{object}	utils.APIResponse	"Caller is not an admin"
//	@Router			/admin/settings/subscription-prices [get]
func (h *AdminHandler) ListSubscriptionPrices(c *gin.Context) {
	resp, err := h.admin.ListSubscriptionPrices(c.Request.Context())
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "subscription prices loaded", resp)
}

// UpdateSubscriptionPrice godoc
//
//	@Summary		Set the daily platform-access price for one vehicle type
//	@Description	Okada, keke, car, and bus can each have a different daily price. Takes effect on a driver's next subscription checkout — never changes a subscription already paid for.
//	@Tags			Admin
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			vehicle_type	path		string									true	"car | okada | keke | bus"
//	@Param			request			body		dto.AdminUpdateSubscriptionPriceRequest	true	"New daily price, in kobo"
//	@Success		200	{object}	utils.APIResponse{data=dto.AdminSubscriptionPriceItem}
//	@Failure		400	{object}	utils.APIResponse	"Invalid vehicle type"
//	@Failure		403	{object}	utils.APIResponse	"Caller is not an admin"
//	@Router			/admin/settings/subscription-prices/{vehicle_type} [put]
func (h *AdminHandler) UpdateSubscriptionPrice(c *gin.Context) {
	var req dto.AdminUpdateSubscriptionPriceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, http.StatusBadRequest, "price_kobo_per_day (>= 0) is required")
		return
	}

	resp, err := h.admin.UpdateSubscriptionPrice(c.Request.Context(), c.Param("vehicle_type"), req.PriceKoboPerDay)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "subscription price updated", resp)
}

// GetTrip godoc
//
//	@Summary		A booking's detail row
//	@Tags			Admin
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string	true	"Trip ID"
//	@Success		200	{object}	utils.APIResponse{data=dto.AdminTripListItem}
//	@Failure		403	{object}	utils.APIResponse	"Caller is not an admin"
//	@Failure		404	{object}	utils.APIResponse
//	@Router			/admin/trips/{id} [get]
func (h *AdminHandler) GetTrip(c *gin.Context) {
	resp, err := h.admin.GetTrip(c.Request.Context(), c.Param("id"))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "trip loaded", resp)
}
