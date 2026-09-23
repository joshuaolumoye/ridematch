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

// TripHandler exposes the trip request → negotiation → match → pickup →
// completion lifecycle.
type TripHandler struct {
	trips *service.TripService
}

// NewTripHandler constructs a TripHandler.
func NewTripHandler(trips *service.TripService) *TripHandler {
	return &TripHandler{trips: trips}
}

// Create godoc
//
//	@Summary		Request a trip
//	@Description	Opens a new trip request and instantly notifies nearby online drivers of the matching vehicle type over WebSocket (and indexes it for GET /trips/nearby polling). A passenger may only have one active (requested/matched/picked_up) trip at a time.
//	@Tags			Trips
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		dto.CreateTripRequest	true	"Trip details"
//	@Success		201		{object}	utils.APIResponse{data=dto.TripResponse}
//	@Failure		400		{object}	utils.APIResponse
//	@Failure		409		{object}	utils.APIResponse	"Already have an active trip"
//	@Failure		429		{object}	utils.APIResponse	"Rate limited"
//	@Router			/trips [post]
func (h *TripHandler) Create(c *gin.Context) {
	var req dto.CreateTripRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, http.StatusBadRequest, "vehicle_type, pickup/destination coordinates, and offered_price_kobo are required")
		return
	}

	resp, err := h.trips.CreateTrip(c.Request.Context(), middleware.UserIDFromContext(c), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusCreated, "trip requested", resp)
}

// Get godoc
//
//	@Summary		Get a trip
//	@Tags			Trips
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string	true	"Trip ID"
//	@Success		200	{object}	utils.APIResponse{data=dto.TripResponse}
//	@Failure		403	{object}	utils.APIResponse
//	@Failure		404	{object}	utils.APIResponse
//	@Router			/trips/{id} [get]
func (h *TripHandler) Get(c *gin.Context) {
	resp, err := h.trips.GetTrip(c.Request.Context(), middleware.UserIDFromContext(c), c.Param("id"))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "trip fetched", resp)
}

// MyActive godoc
//
//	@Summary		Get my current active trip
//	@Description	Returns the caller's in-progress trip (requested/matched/picked_up), if any — lets the app resume showing trip state after a restart.
//	@Tags			Trips
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	utils.APIResponse{data=dto.TripResponse}
//	@Failure		404	{object}	utils.APIResponse	"No active trip"
//	@Router			/trips/active [get]
func (h *TripHandler) MyActive(c *gin.Context) {
	resp, err := h.trips.MyActiveTrip(c.Request.Context(), middleware.UserIDFromContext(c))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "active trip fetched", resp)
}

// History godoc
//
//	@Summary		List my trip history
//	@Description	Every trip the caller has requested as a passenger, newest first, across all statuses (completed, cancelled, expired, or still active).
//	@Tags			Trips
//	@Produce		json
//	@Security		BearerAuth
//	@Param			page		query		int	false	"Page number (default 1)"
//	@Param			page_size	query		int	false	"Results per page (default 20, max 50)"
//	@Success		200	{object}	utils.APIResponse{data=dto.TripHistoryResponse}
//	@Router			/trips/history [get]
func (h *TripHandler) History(c *gin.Context) {
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))

	resp, err := h.trips.History(c.Request.Context(), middleware.UserIDFromContext(c), page, pageSize)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "trip history fetched", resp)
}

// DriverActive godoc
//
//	@Summary		Get the authenticated driver's current active trip
//	@Description	Returns the driver's current matched/picked-up trip, if any — symmetric to GET /trips/active for passengers. Lets the app rehydrate "trip in progress" state on cold start.
//	@Tags			Driver
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	utils.APIResponse{data=dto.TripResponse}
//	@Failure		404	{object}	utils.APIResponse	"No active trip, or not registered as a driver"
//	@Router			/driver/trips/active [get]
func (h *TripHandler) DriverActive(c *gin.Context) {
	resp, err := h.trips.MyActiveTripAsDriver(c.Request.Context(), middleware.UserIDFromContext(c))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "active trip fetched", resp)
}

// Rate godoc
//
//	@Summary		Rate the other party on a completed trip
//	@Description	Either side of a completed trip may submit one 1-5 rating of the other. The rating folds into that party's running average immediately. Each side may rate a given trip only once.
//	@Tags			Trips
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		dto.SubmitRatingRequest	true	"Rating (1-5) and optional comment"
//	@Success		200		{object}	utils.APIResponse{data=dto.TripResponse}
//	@Failure		400		{object}	utils.APIResponse	"Trip not completed yet"
//	@Failure		403		{object}	utils.APIResponse	"Not a participant in this trip"
//	@Failure		409		{object}	utils.APIResponse	"Already rated"
//	@Router			/trips/:id/rate [post]
func (h *TripHandler) Rate(c *gin.Context) {
	var req dto.SubmitRatingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, http.StatusBadRequest, "a rating between 1 and 5 is required")
		return
	}

	resp, err := h.trips.SubmitRating(c.Request.Context(), middleware.UserIDFromContext(c), c.Param("id"), req.Rating, req.Comment)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "rating submitted", resp)
}

// DriverHistory godoc
//
//	@Summary		Get the authenticated driver's own trip history
//	@Description	A page of the calling driver's trips (every status, newest first) — symmetric to GET /trips/history for passengers.
//	@Tags			Driver
//	@Produce		json
//	@Security		BearerAuth
//	@Param			page		query		int	false	"Page number (default 1)"
//	@Param			page_size	query		int	false	"Results per page (default 20, max 50)"
//	@Success		200	{object}	utils.APIResponse{data=dto.TripHistoryResponse}
//	@Failure		404	{object}	utils.APIResponse	"Not registered as a driver"
//	@Router			/driver/trips/history [get]
func (h *TripHandler) DriverHistory(c *gin.Context) {
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))

	resp, err := h.trips.DriverHistory(c.Request.Context(), middleware.UserIDFromContext(c), page, pageSize)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "driver trip history fetched", resp)
}

// Earnings godoc
//
//	@Summary		Get the authenticated driver's earnings summary
//	@Description	Real, SQL-aggregated totals over the driver's own completed trips (all-time and today), plus a short recent-trips list.
//	@Tags			Driver
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	utils.APIResponse{data=dto.DriverEarningsResponse}
//	@Failure		404	{object}	utils.APIResponse	"Not registered as a driver"
//	@Router			/driver/earnings [get]
func (h *TripHandler) Earnings(c *gin.Context) {
	resp, err := h.trips.DriverEarnings(c.Request.Context(), middleware.UserIDFromContext(c))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "driver earnings fetched", resp)
}

// Nearby godoc
//
//	@Summary		Find nearby open trip requests
//	@Description	Driver-facing REST fallback for the WebSocket push — open (unmatched) trip requests near a point, for the given vehicle type, nearest first.
//	@Tags			Trips
//	@Produce		json
//	@Security		BearerAuth
//	@Param			vehicle_type	query		string	true	"car | okada | keke | bus"
//	@Param			lat				query		number	true	"Latitude"
//	@Param			lng				query		number	true	"Longitude"
//	@Param			radius_km		query		number	false	"Search radius in km"
//	@Param			limit			query		int		false	"Max results"
//	@Success		200	{object}	utils.APIResponse{data=[]dto.NearbyOpenTripResponse}
//	@Failure		400	{object}	utils.APIResponse
//	@Router			/trips/nearby [get]
func (h *TripHandler) Nearby(c *gin.Context) {
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

	resp, err := h.trips.FindNearbyOpenTrips(c.Request.Context(), vehicleType, lat, lng, radiusKM, limit)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "nearby trips fetched", resp)
}

// MakeOffer godoc
//
//	@Summary		Make a price offer on a trip
//	@Description	A driver proposes a price for an open trip request. Placing a new offer while one is already pending from the same driver updates that offer's price rather than creating a duplicate. Requires the driver to be online, verified, and subscribed.
//	@Tags			Trips
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string					true	"Trip ID"
//	@Param			request	body		dto.MakeOfferRequest	true	"Proposed price"
//	@Success		201		{object}	utils.APIResponse{data=dto.OfferResponse}
//	@Failure		400		{object}	utils.APIResponse
//	@Failure		403		{object}	utils.APIResponse	"Not eligible to make offers"
//	@Failure		429		{object}	utils.APIResponse	"Rate limited"
//	@Router			/trips/{id}/offers [post]
func (h *TripHandler) MakeOffer(c *gin.Context) {
	var req dto.MakeOfferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, http.StatusBadRequest, "price_kobo is required")
		return
	}

	resp, err := h.trips.MakeOffer(c.Request.Context(), middleware.UserIDFromContext(c), c.Param("id"), req.PriceKobo)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusCreated, "offer sent", resp)
}

// ListOffers godoc
//
//	@Summary		List pending offers on a trip
//	@Tags			Trips
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string	true	"Trip ID"
//	@Success		200	{object}	utils.APIResponse{data=[]dto.OfferResponse}
//	@Failure		403	{object}	utils.APIResponse
//	@Router			/trips/{id}/offers [get]
func (h *TripHandler) ListOffers(c *gin.Context) {
	resp, err := h.trips.ListOffers(c.Request.Context(), middleware.UserIDFromContext(c), c.Param("id"))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "offers fetched", resp)
}

// AcceptOffer godoc
//
//	@Summary		Accept a driver's offer
//	@Description	Matches the trip to the offering driver and generates a one-time pickup PIN, returned only in this response — show it to the driver in person at pickup. Every other offering driver is notified the trip has closed.
//	@Tags			Trips
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id			path		string	true	"Trip ID"
//	@Param			offerId		path		string	true	"Offer ID"
//	@Success		200			{object}	utils.APIResponse{data=dto.AcceptOfferResponse}
//	@Failure		400			{object}	utils.APIResponse	"Trip or offer no longer open"
//	@Failure		403			{object}	utils.APIResponse
//	@Router			/trips/{id}/offers/{offerId}/accept [post]
func (h *TripHandler) AcceptOffer(c *gin.Context) {
	resp, err := h.trips.AcceptOffer(c.Request.Context(), middleware.UserIDFromContext(c), c.Param("id"), c.Param("offerId"))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "driver matched, share the pickup PIN with them in person", resp)
}

// RejectOffer godoc
//
//	@Summary		Reject a driver's offer
//	@Tags			Trips
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id			path	string	true	"Trip ID"
//	@Param			offerId		path	string	true	"Offer ID"
//	@Success		200			{object}	utils.APIResponse
//	@Failure		403			{object}	utils.APIResponse
//	@Router			/trips/{id}/offers/{offerId}/reject [post]
func (h *TripHandler) RejectOffer(c *gin.Context) {
	if err := h.trips.RejectOffer(c.Request.Context(), middleware.UserIDFromContext(c), c.Param("id"), c.Param("offerId")); err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "offer rejected", nil)
}

// ConfirmPickup godoc
//
//	@Summary		Confirm pickup with the passenger's PIN
//	@Description	The driver enters the 4-digit PIN the passenger shows them in person, proving the pickup actually happened. This is the feature the whole trip-tracking flow exists to support — a trip cannot skip straight from "matched" to "completed" without it.
//	@Tags			Trips
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string						true	"Trip ID"
//	@Param			request	body		dto.ConfirmPickupRequest	true	"4-digit pickup PIN"
//	@Success		200		{object}	utils.APIResponse{data=dto.TripResponse}
//	@Failure		400		{object}	utils.APIResponse	"Incorrect PIN"
//	@Failure		403		{object}	utils.APIResponse	"Not the matched driver"
//	@Router			/trips/{id}/confirm-pickup [post]
func (h *TripHandler) ConfirmPickup(c *gin.Context) {
	var req dto.ConfirmPickupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, http.StatusBadRequest, "a 4-digit pin is required")
		return
	}

	resp, err := h.trips.ConfirmPickup(c.Request.Context(), middleware.UserIDFromContext(c), c.Param("id"), req.PIN)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "pickup confirmed", resp)
}

// Complete godoc
//
//	@Summary		Mark a trip complete
//	@Tags			Trips
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string	true	"Trip ID"
//	@Success		200	{object}	utils.APIResponse{data=dto.TripResponse}
//	@Failure		403	{object}	utils.APIResponse
//	@Router			/trips/{id}/complete [post]
func (h *TripHandler) Complete(c *gin.Context) {
	resp, err := h.trips.CompleteTrip(c.Request.Context(), middleware.UserIDFromContext(c), c.Param("id"))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "trip completed", resp)
}

// Cancel godoc
//
//	@Summary		Cancel a trip
//	@Description	Either the passenger or the matched driver may cancel, but only before pickup is confirmed.
//	@Tags			Trips
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path	string					true	"Trip ID"
//	@Param			request	body	dto.CancelTripRequest	false	"Optional reason"
//	@Success		200		{object}	utils.APIResponse{data=dto.TripResponse}
//	@Failure		400		{object}	utils.APIResponse	"Trip can no longer be cancelled"
//	@Failure		403		{object}	utils.APIResponse
//	@Router			/trips/{id}/cancel [post]
func (h *TripHandler) Cancel(c *gin.Context) {
	var req dto.CancelTripRequest
	_ = c.ShouldBindJSON(&req) // body is optional; ignore a missing/empty one

	resp, err := h.trips.CancelTrip(c.Request.Context(), middleware.UserIDFromContext(c), c.Param("id"), req.Reason)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "trip cancelled", resp)
}
