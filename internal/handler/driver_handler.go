package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"ridematch-backend/internal/dto"
	"ridematch-backend/internal/middleware"
	"ridematch-backend/internal/service"
	"ridematch-backend/internal/utils"
)

// DriverHandler exposes driver registration, online/offline, live
// location, and the passenger-facing nearby-drivers endpoints.
type DriverHandler struct {
	drivers *service.DriverService
}

// NewDriverHandler constructs a DriverHandler.
func NewDriverHandler(drivers *service.DriverService) *DriverHandler {
	return &DriverHandler{drivers: drivers}
}

// Register godoc
//
//	@Summary		Register as a driver
//	@Description	Creates a driver profile (vehicle type, plate number, ID document) for the authenticated account and promotes it to the "driver" role. A vehicle photo and ID document must already be hosted somewhere reachable by URL. The profile starts in "pending" verification status — see the admin verification endpoint.
//	@Tags			Driver
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		dto.RegisterDriverRequest	true	"Vehicle and document details"
//	@Success		201		{object}	utils.APIResponse{data=dto.DriverProfileResponse}
//	@Failure		400		{object}	utils.APIResponse
//	@Failure		409		{object}	utils.APIResponse	"Already registered, or plate number taken"
//	@Router			/driver/register [post]
func (h *DriverHandler) Register(c *gin.Context) {
	var req dto.RegisterDriverRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, http.StatusBadRequest, "vehicle_type, plate_number, and id_document_url are required")
		return
	}

	resp, err := h.drivers.Register(c.Request.Context(), middleware.UserIDFromContext(c), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	utils.Success(c, http.StatusCreated, "driver profile created, pending verification", resp)
}

// Me godoc
//
//	@Summary		Get the authenticated driver's profile
//	@Tags			Driver
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	utils.APIResponse{data=dto.DriverProfileResponse}
//	@Failure		404	{object}	utils.APIResponse	"Not registered as a driver"
//	@Router			/driver/profile [get]
func (h *DriverHandler) Me(c *gin.Context) {
	resp, err := h.drivers.GetMyProfile(c.Request.Context(), middleware.UserIDFromContext(c))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "driver profile fetched", resp)
}

// GoOnline godoc
//
//	@Summary		Go online
//	@Description	Marks the driver online and publishes their live position so they appear in nearby-driver searches. Requires an approved driver profile and an active subscription.
//	@Tags			Driver
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		dto.GoOnlineRequest	true	"Current position"
//	@Success		200		{object}	utils.APIResponse{data=dto.DriverProfileResponse}
//	@Failure		403		{object}	utils.APIResponse	"Not verified or subscription inactive"
//	@Failure		404		{object}	utils.APIResponse	"Not registered as a driver"
//	@Router			/driver/online [post]
func (h *DriverHandler) GoOnline(c *gin.Context) {
	var req dto.GoOnlineRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, http.StatusBadRequest, "a valid latitude and longitude are required")
		return
	}

	resp, err := h.drivers.GoOnline(c.Request.Context(), middleware.UserIDFromContext(c), req.Latitude, req.Longitude)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "you are now online", resp)
}

// GoOffline godoc
//
//	@Summary		Go offline
//	@Description	Marks the driver offline and removes them from nearby-driver searches.
//	@Tags			Driver
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	utils.APIResponse
//	@Failure		404	{object}	utils.APIResponse	"Not registered as a driver"
//	@Router			/driver/offline [post]
func (h *DriverHandler) GoOffline(c *gin.Context) {
	if err := h.drivers.GoOffline(c.Request.Context(), middleware.UserIDFromContext(c)); err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "you are now offline", nil)
}

// PingLocation godoc
//
//	@Summary		Send a location update
//	@Description	Updates the driver's live position. Call this periodically (every 5-10 seconds) while online — a driver who stops pinging is automatically treated as stale and dropped from nearby-driver results after the configured timeout.
//	@Tags			Driver
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		dto.LocationPingRequest	true	"Current position"
//	@Success		200		{object}	utils.APIResponse
//	@Failure		400		{object}	utils.APIResponse	"Not currently online"
//	@Router			/driver/location [post]
func (h *DriverHandler) PingLocation(c *gin.Context) {
	var req dto.LocationPingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, http.StatusBadRequest, "a valid latitude and longitude are required")
		return
	}

	if err := h.drivers.PingLocation(c.Request.Context(), middleware.UserIDFromContext(c), req.Latitude, req.Longitude); err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "location updated", nil)
}

// FindNearby godoc
//
//	@Summary		Find nearby online drivers
//	@Description	Passenger-facing endpoint: returns online drivers of the given vehicle type near a point, nearest first. Coordinates in the response are rounded (not exact) to limit live-tracking exposure until a match is confirmed.
//	@Tags			Driver
//	@Produce		json
//	@Security		BearerAuth
//	@Param			vehicle_type	query		string	true	"car | okada | keke | bus"
//	@Param			lat				query		number	true	"Latitude"
//	@Param			lng				query		number	true	"Longitude"
//	@Param			radius_km		query		number	false	"Search radius in km (default 5, max 20)"
//	@Param			limit			query		int		false	"Max results (default 20)"
//	@Success		200	{object}	utils.APIResponse{data=[]dto.NearbyDriverResponse}
//	@Failure		400	{object}	utils.APIResponse
//	@Router			/drivers/nearby [get]
func (h *DriverHandler) FindNearby(c *gin.Context) {
	vehicleType := c.Query("vehicle_type")
	if vehicleType == "" {
		utils.Fail(c, http.StatusBadRequest, "vehicle_type is required")
		return
	}

	lat, err1 := strconv.ParseFloat(c.Query("lat"), 64)
	lng, err2 := strconv.ParseFloat(c.Query("lng"), 64)
	if err1 != nil || err2 != nil {
		utils.Fail(c, http.StatusBadRequest, "valid lat and lng query parameters are required")
		return
	}

	radiusKM, _ := strconv.ParseFloat(c.Query("radius_km"), 64)
	limit, _ := strconv.Atoi(c.Query("limit"))

	resp, err := h.drivers.FindNearbyDrivers(c.Request.Context(), vehicleType, lat, lng, radiusKM, limit)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "nearby drivers fetched", resp)
}
