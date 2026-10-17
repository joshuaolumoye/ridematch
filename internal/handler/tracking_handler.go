package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"ridematch-backend/internal/dto"
	"ridematch-backend/internal/repository"
	"ridematch-backend/internal/service"
	"ridematch-backend/internal/utils"
)

// TrackingHandler receives driver GPS batches and serves them to admins.
type TrackingHandler struct {
	tracking *service.TrackingService
}

// NewTrackingHandler constructs a TrackingHandler.
func NewTrackingHandler(t *service.TrackingService) *TrackingHandler {
	return &TrackingHandler{tracking: t}
}

// Ingest accepts a batch of buffered GPS samples from the driver's phone.
func (h *TrackingHandler) Ingest(c *gin.Context) {
	var req dto.TrackingBatchRequest
	if !bind(c, &req, "shift_id and 1–200 valid points are required") {
		return
	}
	resp, err := h.tracking.Ingest(c.Request.Context(), uid(c), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "points recorded", resp)
}

func parseDay(v string, endOfDay bool) *time.Time {
	if v == "" {
		return nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return &t
	}
	if t, err := time.Parse("2006-01-02", v); err == nil {
		if endOfDay {
			t = t.Add(24*time.Hour - time.Second)
		}
		return &t
	}
	return nil
}

// AdminShifts lists shifts (filter by driver, status, date range).
func (h *TrackingHandler) AdminShifts(c *gin.Context) {
	page, size := pageParams(c)
	f := repository.ShiftFilter{
		DriverID: c.Query("driver_id"), Status: c.Query("status"),
		From: parseDay(c.Query("from"), false), To: parseDay(c.Query("to"), true),
	}
	items, total, err := h.tracking.ListShifts(c.Request.Context(), f, page, size)
	writePage(c, "shifts loaded", items, total, page, size, err)
}

// AdminShift returns one shift with its trail and integrity verdict.
func (h *TrackingHandler) AdminShift(c *gin.Context) {
	resp, err := h.tracking.ShiftDetail(c.Request.Context(), actorFrom(c), c.Param("id"))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "shift loaded", resp)
}

// AdminExportShift downloads a shift's evidence (json | geojson | csv).
func (h *TrackingHandler) AdminExportShift(c *gin.Context) {
	ct, name, body, err := h.tracking.ExportShift(c.Request.Context(), actorFrom(c), c.Param("id"), c.DefaultQuery("format", "json"))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="`+name+`"`)
	c.Data(http.StatusOK, ct, body)
}

// AdminTripRoute returns the recorded route of one trip.
func (h *TrackingHandler) AdminTripRoute(c *gin.Context) {
	resp, err := h.tracking.TripRoute(c.Request.Context(), actorFrom(c), c.Param("id"))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "route loaded", resp)
}
