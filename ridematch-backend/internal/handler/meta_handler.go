package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"ridematch-backend/internal/dto"
	"ridematch-backend/internal/models"
	"ridematch-backend/internal/service"
	"ridematch-backend/internal/utils"
)

// MetaHandler exposes read-only, admin-configured reference data that
// both the passenger and driver sides of the app need before a driver
// profile exists — which vehicle types currently accept new drivers, and
// which documents a given vehicle type requires at registration. Every
// route here sits behind plain authRequired (any logged-in account), not
// an admin check — see internal/router.
type MetaHandler struct {
	settings *service.SettingsService
}

// NewMetaHandler constructs a MetaHandler.
func NewMetaHandler(settings *service.SettingsService) *MetaHandler {
	return &MetaHandler{settings: settings}
}

// VehicleTypes godoc
//
//	@Summary		List vehicle types currently accepting new drivers
//	@Description	Returns only enabled vehicle types — render exactly this list on driver registration's vehicle-type picker and the passenger booking screen's mode picker, rather than a hardcoded car/okada/keke/bus set.
//	@Tags			Meta
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	utils.APIResponse{data=[]dto.VehicleTypeItem}
//	@Router			/meta/vehicle-types [get]
func (h *MetaHandler) VehicleTypes(c *gin.Context) {
	resp, err := h.settings.EnabledVehicleTypes(c.Request.Context())
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "vehicle types loaded", resp)
}

// DocumentTypes godoc
//
//	@Summary		List the documents a vehicle type currently requires/offers
//	@Description	Returns only enabled document types, in display order, for one vehicle type — render exactly this list as the registration screen's document-upload steps.
//	@Tags			Meta
//	@Produce		json
//	@Security		BearerAuth
//	@Param			vehicle_type	query		string	true	"car | okada | keke | bus"
//	@Success		200	{object}	utils.APIResponse{data=[]dto.DocumentTypeItem}
//	@Failure		400	{object}	utils.APIResponse	"vehicle_type is required or invalid"
//	@Router			/meta/document-types [get]
func (h *MetaHandler) DocumentTypes(c *gin.Context) {
	vehicleType := c.Query("vehicle_type")
	if !isKnownVehicleType(vehicleType) {
		utils.Fail(c, http.StatusBadRequest, "a valid vehicle_type (car, okada, keke, or bus) query parameter is required")
		return
	}

	rows, err := h.settings.EnabledDocumentTypes(c.Request.Context(), models.VehicleType(vehicleType))
	if err != nil {
		handleServiceError(c, err)
		return
	}

	items := make([]dto.DocumentTypeItem, 0, len(rows))
	for _, d := range rows {
		items = append(items, dto.DocumentTypeItem{ID: d.ID, Name: d.Name, Required: d.Required, SortOrder: d.SortOrder})
	}
	utils.Success(c, http.StatusOK, "document types loaded", items)
}

// VehicleCatalog godoc
//
//	@Summary		List the admin-curated make/model catalog for a vehicle type
//	@Description	Returns only enabled makes/models, nested, for the registration screen's make/model dropdowns and the passenger booking screen's category icons. Empty for a vehicle type admin hasn't populated yet — the registration screen falls back to free-text entry in that case.
//	@Tags			Meta
//	@Produce		json
//	@Security		BearerAuth
//	@Param			vehicle_type	query		string	true	"car | okada | keke | bus"
//	@Success		200	{object}	utils.APIResponse{data=[]dto.VehicleCatalogMakeItem}
//	@Failure		400	{object}	utils.APIResponse	"vehicle_type is required or invalid"
//	@Router			/meta/vehicle-catalog [get]
func (h *MetaHandler) VehicleCatalog(c *gin.Context) {
	vehicleType := c.Query("vehicle_type")
	if !isKnownVehicleType(vehicleType) {
		utils.Fail(c, http.StatusBadRequest, "a valid vehicle_type (car, okada, keke, or bus) query parameter is required")
		return
	}
	resp, err := h.settings.VehicleCatalog(c.Request.Context(), models.VehicleType(vehicleType))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "vehicle catalog loaded", resp)
}

func isKnownVehicleType(vehicleType string) bool {
	switch models.VehicleType(vehicleType) {
	case models.VehicleCar, models.VehicleOkada, models.VehicleKeke, models.VehicleBus:
		return true
	default:
		return false
	}
}
