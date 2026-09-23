// Package handler contains gin HTTP handlers. Handlers are responsible
// only for: binding/validating the request, calling exactly one service
// method, and translating the result (or error) into an HTTP response.
// No business logic lives here.
package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"ridematch-backend/internal/service"
	"ridematch-backend/internal/utils"
)

// errorStatusMap maps a known service-layer sentinel error to the HTTP
// status code it should produce. Unrecognized errors fall back to 500 so
// that unexpected failures never accidentally leak as a 4xx.
var errorStatusMap = map[error]int{
	service.ErrOTPCooldown:         http.StatusTooManyRequests,
	service.ErrOTPNotFound:         http.StatusBadRequest,
	service.ErrOTPIncorrect:        http.StatusBadRequest,
	service.ErrOTPTooManyAttempts:  http.StatusTooManyRequests,
	service.ErrAccountSuspended:    http.StatusForbidden,
	service.ErrAccountBanned:       http.StatusForbidden,
	service.ErrInvalidRefreshToken: http.StatusUnauthorized,
	utils.ErrInvalidPhone:          http.StatusBadRequest,
	utils.ErrInvalidEmail:          http.StatusBadRequest,
	utils.ErrInvalidIdentifier:     http.StatusBadRequest,

	service.ErrDriverProfileNotFound:   http.StatusNotFound,
	service.ErrDriverAlreadyRegistered: http.StatusConflict,
	service.ErrPlateNumberTaken:        http.StatusConflict,
	service.ErrDriverNotApproved:       http.StatusForbidden,
	service.ErrSubscriptionInactive:    http.StatusForbidden,
	service.ErrNotOnline:               http.StatusBadRequest,

	service.ErrTripNotFound:      http.StatusNotFound,
	service.ErrTripNotOpen:       http.StatusConflict,
	service.ErrTripAlreadyActive: http.StatusConflict,
	service.ErrOfferNotFound:     http.StatusNotFound,
	service.ErrOfferNotPending:   http.StatusConflict,
	service.ErrNotTripOwner:      http.StatusForbidden,
	service.ErrNotMatchedDriver:  http.StatusForbidden,
	service.ErrTripNotMatched:    http.StatusBadRequest,
	service.ErrTripNotPickedUp:   http.StatusBadRequest,
	service.ErrTripNotCancelable: http.StatusConflict,
	service.ErrInvalidPickupPIN:  http.StatusBadRequest,
	service.ErrTripNotCompleted:  http.StatusBadRequest,
	service.ErrAlreadyRated:      http.StatusConflict,

	service.ErrInvalidWebhookSignature:   http.StatusUnauthorized,
	service.ErrPaymentNotFound:           http.StatusNotFound,
	service.ErrPaymentVerificationFailed: http.StatusUnprocessableEntity,
}

// handleServiceError writes the appropriate error response for an error
// returned by a service-layer call.
func handleServiceError(c *gin.Context, err error) {
	for known, status := range errorStatusMap {
		if errors.Is(err, known) {
			utils.Fail(c, status, err.Error())
			return
		}
	}
	utils.Fail(c, http.StatusInternalServerError, "something went wrong, please try again")
}
