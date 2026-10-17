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
	admin    *service.AdminService
	settings *service.SettingsService
}

// NewAdminHandler constructs an AdminHandler.
func NewAdminHandler(admin *service.AdminService, settings *service.SettingsService) *AdminHandler {
	return &AdminHandler{admin: admin, settings: settings}
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

// ListVehicleTypes godoc
//
//	@Summary		List every vehicle type and whether it's accepting new drivers
//	@Tags			Admin
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	utils.APIResponse{data=[]dto.AdminVehicleTypeItem}
//	@Failure		403	{object}	utils.APIResponse	"Caller is not an admin"
//	@Router			/admin/settings/vehicle-types [get]
func (h *AdminHandler) ListVehicleTypes(c *gin.Context) {
	resp, err := h.settings.ListVehicleTypes(c.Request.Context())
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "vehicle types loaded", resp)
}

// UpdateVehicleType godoc
//
//	@Summary		Turn a vehicle type on or off for new driver registration
//	@Description	Disabling a vehicle type hides it from registration and the passenger booking picker immediately; it does not affect drivers already registered under it.
//	@Tags			Admin
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			vehicle_type	path		string									true	"car | okada | keke | bus"
//	@Param			request			body		dto.AdminUpdateVehicleTypeRequest	true	"Enabled flag"
//	@Success		200	{object}	utils.APIResponse{data=dto.AdminVehicleTypeItem}
//	@Failure		400	{object}	utils.APIResponse	"Invalid vehicle type"
//	@Failure		403	{object}	utils.APIResponse	"Caller is not an admin"
//	@Router			/admin/settings/vehicle-types/{vehicle_type} [put]
func (h *AdminHandler) UpdateVehicleType(c *gin.Context) {
	var req dto.AdminUpdateVehicleTypeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, http.StatusBadRequest, "enabled (bool) is required")
		return
	}

	resp, err := h.settings.SetVehicleTypeEnabled(c.Request.Context(), c.Param("vehicle_type"), req.Enabled)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "vehicle type updated", resp)
}

// ListSubscriptionPlans godoc
//
//	@Summary		List every (vehicle type, billing cycle) payment plan
//	@Tags			Admin
//	@Produce		json
//	@Security		BearerAuth
//	@Param			vehicle_type	query		string	false	"car | okada | keke | bus (every vehicle type if omitted)"
//	@Success		200	{object}	utils.APIResponse{data=[]dto.AdminSubscriptionPlanItem}
//	@Failure		403	{object}	utils.APIResponse	"Caller is not an admin"
//	@Router			/admin/settings/subscription-plans [get]
func (h *AdminHandler) ListSubscriptionPlans(c *gin.Context) {
	resp, err := h.settings.ListSubscriptionPlans(c.Request.Context(), c.Query("vehicle_type"))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "subscription plans loaded", resp)
}

// UpdateSubscriptionPlan godoc
//
//	@Summary		Turn one billing cycle on/off and set its price for a vehicle type
//	@Description	Each vehicle type has its own daily/weekly/monthly/yearly plans, each independently enabled and priced. If only "yearly" is enabled for a vehicle type, that type's drivers see exactly one plan to pay for.
//	@Tags			Admin
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			vehicle_type	path		string									true	"car | okada | keke | bus"
//	@Param			billing_cycle	path		string									true	"daily | weekly | monthly | yearly"
//	@Param			request			body		dto.AdminUpdateSubscriptionPlanRequest	true	"Enabled flag and price in kobo"
//	@Success		200	{object}	utils.APIResponse{data=dto.AdminSubscriptionPlanItem}
//	@Failure		400	{object}	utils.APIResponse	"Invalid vehicle type or billing cycle"
//	@Failure		403	{object}	utils.APIResponse	"Caller is not an admin"
//	@Router			/admin/settings/subscription-plans/{vehicle_type}/{billing_cycle} [put]
func (h *AdminHandler) UpdateSubscriptionPlan(c *gin.Context) {
	var req dto.AdminUpdateSubscriptionPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, http.StatusBadRequest, "enabled (bool) and price_kobo (>= 0) are required")
		return
	}

	resp, err := h.settings.UpsertSubscriptionPlan(c.Request.Context(), c.Param("vehicle_type"), c.Param("billing_cycle"), req.Enabled, req.PriceKobo)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "subscription plan updated", resp)
}

// ListDocumentTypes godoc
//
//	@Summary		List the documents required/offered for a vehicle type
//	@Tags			Admin
//	@Produce		json
//	@Security		BearerAuth
//	@Param			vehicle_type	query		string	false	"car | okada | keke | bus (every vehicle type if omitted)"
//	@Success		200	{object}	utils.APIResponse{data=[]dto.AdminDocumentTypeItem}
//	@Failure		403	{object}	utils.APIResponse	"Caller is not an admin"
//	@Router			/admin/settings/document-types [get]
func (h *AdminHandler) ListDocumentTypes(c *gin.Context) {
	resp, err := h.settings.ListDocumentTypes(c.Request.Context(), c.Query("vehicle_type"))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "document types loaded", resp)
}

// CreateDocumentType godoc
//
//	@Summary		Add a required/optional document to a vehicle type's registration list
//	@Tags			Admin
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		dto.AdminCreateDocumentTypeRequest	true	"Document type"
//	@Success		201		{object}	utils.APIResponse{data=dto.AdminDocumentTypeItem}
//	@Failure		400		{object}	utils.APIResponse
//	@Failure		403		{object}	utils.APIResponse	"Caller is not an admin"
//	@Router			/admin/settings/document-types [post]
func (h *AdminHandler) CreateDocumentType(c *gin.Context) {
	var req dto.AdminCreateDocumentTypeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, http.StatusBadRequest, "vehicle_type and name are required")
		return
	}

	resp, err := h.settings.CreateDocumentType(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusCreated, "document type created", resp)
}

// UpdateDocumentType godoc
//
//	@Summary		Edit a document requirement (name, required/optional, enabled, order)
//	@Tags			Admin
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string									true	"Document type ID"
//	@Param			request	body		dto.AdminUpdateDocumentTypeRequest	true	"Updated fields"
//	@Success		200		{object}	utils.APIResponse{data=dto.AdminDocumentTypeItem}
//	@Failure		400		{object}	utils.APIResponse
//	@Failure		403		{object}	utils.APIResponse	"Caller is not an admin"
//	@Failure		404		{object}	utils.APIResponse
//	@Router			/admin/settings/document-types/{id} [put]
func (h *AdminHandler) UpdateDocumentType(c *gin.Context) {
	var req dto.AdminUpdateDocumentTypeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, http.StatusBadRequest, "name is required")
		return
	}

	resp, err := h.settings.UpdateDocumentType(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "document type updated", resp)
}

// DeleteDocumentType godoc
//
//	@Summary		Remove a document requirement from a vehicle type
//	@Tags			Admin
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string	true	"Document type ID"
//	@Success		200	{object}	utils.APIResponse
//	@Failure		403	{object}	utils.APIResponse	"Caller is not an admin"
//	@Failure		404	{object}	utils.APIResponse
//	@Router			/admin/settings/document-types/{id} [delete]
func (h *AdminHandler) DeleteDocumentType(c *gin.Context) {
	if err := h.settings.DeleteDocumentType(c.Request.Context(), c.Param("id")); err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "document type deleted", nil)
}

// --- Vehicle make/model catalog ---

// ListVehicleMakes godoc
//
//	@Summary		List one vehicle type's full make/model catalog (admin view)
//	@Tags			Admin
//	@Produce		json
//	@Security		BearerAuth
//	@Param			vehicle_type	query		string	true	"car | okada | keke | bus"
//	@Success		200	{object}	utils.APIResponse{data=[]dto.AdminVehicleMakeItem}
//	@Failure		400	{object}	utils.APIResponse
//	@Failure		403	{object}	utils.APIResponse	"Caller is not an admin"
//	@Router			/admin/vehicle-catalog/makes [get]
func (h *AdminHandler) ListVehicleMakes(c *gin.Context) {
	resp, err := h.settings.ListVehicleMakes(c.Request.Context(), c.Query("vehicle_type"))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "vehicle makes loaded", resp)
}

// CreateVehicleMake godoc
//
//	@Summary		Add a vehicle manufacturer under a vehicle type
//	@Tags			Admin
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		dto.AdminCreateMakeRequest	true	"Vehicle type and make name"
//	@Success		201		{object}	utils.APIResponse{data=dto.AdminVehicleMakeItem}
//	@Failure		400		{object}	utils.APIResponse
//	@Failure		403		{object}	utils.APIResponse	"Caller is not an admin"
//	@Failure		409		{object}	utils.APIResponse	"This make already exists for this vehicle type"
//	@Router			/admin/vehicle-catalog/makes [post]
func (h *AdminHandler) CreateVehicleMake(c *gin.Context) {
	var req dto.AdminCreateMakeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, http.StatusBadRequest, "vehicle_type and name are required")
		return
	}
	resp, err := h.settings.CreateVehicleMake(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusCreated, "vehicle make created", resp)
}

// UpdateVehicleMake godoc
//
//	@Summary		Rename a make, or enable/disable it
//	@Tags			Admin
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string							true	"Vehicle make ID"
//	@Param			request	body		dto.AdminUpdateMakeRequest	true	"Updated fields"
//	@Success		200		{object}	utils.APIResponse{data=dto.AdminVehicleMakeItem}
//	@Failure		400		{object}	utils.APIResponse
//	@Failure		403		{object}	utils.APIResponse	"Caller is not an admin"
//	@Failure		404		{object}	utils.APIResponse
//	@Router			/admin/vehicle-catalog/makes/{id} [put]
func (h *AdminHandler) UpdateVehicleMake(c *gin.Context) {
	var req dto.AdminUpdateMakeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, http.StatusBadRequest, "name is required")
		return
	}
	resp, err := h.settings.UpdateVehicleMake(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "vehicle make updated", resp)
}

// DeleteVehicleMake godoc
//
//	@Summary		Remove a make and every model under it
//	@Tags			Admin
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string	true	"Vehicle make ID"
//	@Success		200	{object}	utils.APIResponse
//	@Failure		403	{object}	utils.APIResponse	"Caller is not an admin"
//	@Failure		404	{object}	utils.APIResponse
//	@Router			/admin/vehicle-catalog/makes/{id} [delete]
func (h *AdminHandler) DeleteVehicleMake(c *gin.Context) {
	if err := h.settings.DeleteVehicleMake(c.Request.Context(), c.Param("id")); err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "vehicle make deleted", nil)
}

// CreateVehicleModel godoc
//
//	@Summary		Add a model/trim under an existing make
//	@Tags			Admin
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		dto.AdminCreateModelRequest	true	"Make ID, model name, and (car only) category"
//	@Success		201		{object}	utils.APIResponse{data=dto.AdminVehicleModelItem}
//	@Failure		400		{object}	utils.APIResponse
//	@Failure		403		{object}	utils.APIResponse	"Caller is not an admin"
//	@Failure		404		{object}	utils.APIResponse	"Make not found"
//	@Failure		409		{object}	utils.APIResponse	"This model already exists under this make"
//	@Router			/admin/vehicle-catalog/models [post]
func (h *AdminHandler) CreateVehicleModel(c *gin.Context) {
	var req dto.AdminCreateModelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, http.StatusBadRequest, "make_id and name are required")
		return
	}
	resp, err := h.settings.CreateVehicleModel(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusCreated, "vehicle model created", resp)
}

// UpdateVehicleModel godoc
//
//	@Summary		Edit a model's name, category, or enabled flag
//	@Tags			Admin
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string							true	"Vehicle model ID"
//	@Param			request	body		dto.AdminUpdateModelRequest	true	"Updated fields"
//	@Success		200		{object}	utils.APIResponse{data=dto.AdminVehicleModelItem}
//	@Failure		400		{object}	utils.APIResponse
//	@Failure		403		{object}	utils.APIResponse	"Caller is not an admin"
//	@Failure		404		{object}	utils.APIResponse
//	@Router			/admin/vehicle-catalog/models/{id} [put]
func (h *AdminHandler) UpdateVehicleModel(c *gin.Context) {
	var req dto.AdminUpdateModelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, http.StatusBadRequest, "name is required")
		return
	}
	resp, err := h.settings.UpdateVehicleModel(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "vehicle model updated", resp)
}

// DeleteVehicleModel godoc
//
//	@Summary		Remove a single model, leaving its make untouched
//	@Tags			Admin
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string	true	"Vehicle model ID"
//	@Success		200	{object}	utils.APIResponse
//	@Failure		403	{object}	utils.APIResponse	"Caller is not an admin"
//	@Failure		404	{object}	utils.APIResponse
//	@Router			/admin/vehicle-catalog/models/{id} [delete]
func (h *AdminHandler) DeleteVehicleModel(c *gin.Context) {
	if err := h.settings.DeleteVehicleModel(c.Request.Context(), c.Param("id")); err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "vehicle model deleted", nil)
}

// BulkImportVehicleCatalog godoc
//
//	@Summary		Create many make+model rows at once (the "paste a sheet" importer)
//	@Description	Idempotent — a row whose make+model already exists (case-insensitive) is silently skipped, not duplicated or errored, so the same sheet can be re-pasted safely as it grows.
//	@Tags			Admin
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		dto.AdminBulkCatalogRequest	true	"Vehicle type and rows"
//	@Success		200		{object}	utils.APIResponse{data=dto.AdminBulkCatalogResult}
//	@Failure		400		{object}	utils.APIResponse
//	@Failure		403		{object}	utils.APIResponse	"Caller is not an admin"
//	@Router			/admin/vehicle-catalog/bulk [post]
func (h *AdminHandler) BulkImportVehicleCatalog(c *gin.Context) {
	var req dto.AdminBulkCatalogRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, http.StatusBadRequest, "vehicle_type and at least one row (make, model) are required")
		return
	}
	resp, err := h.settings.BulkImportVehicleCatalog(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "vehicle catalog imported", resp)
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
