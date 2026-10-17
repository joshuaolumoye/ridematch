// Package handler contains gin HTTP handlers. Handlers are responsible
// only for: binding/validating the request, calling exactly one service
// method, and translating the result (or error) into an HTTP response.
// No business logic lives here.
package handler

import (
	"errors"
	"log"
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

	service.ErrUserNotFound:         http.StatusNotFound,
	service.ErrNotificationNotFound: http.StatusNotFound,
	service.ErrInvalidVehicleType:   http.StatusBadRequest,

	service.ErrVehicleTypeDisabled:         http.StatusBadRequest,
	service.ErrMissingRequiredDocuments:    http.StatusBadRequest,
	service.ErrDocumentTypeNotFound:        http.StatusNotFound,
	service.ErrSubscriptionPlanUnavailable: http.StatusBadRequest,

	service.ErrVehicleMakeNotFound:         http.StatusNotFound,
	service.ErrVehicleModelNotFound:        http.StatusNotFound,
	service.ErrVehicleModelMakeMismatch:    http.StatusBadRequest,
	service.ErrVehicleCatalogEntryDisabled: http.StatusBadRequest,
	service.ErrDuplicateVehicleMake:        http.StatusConflict,
	service.ErrDuplicateVehicleModel:       http.StatusConflict,

	service.ErrNoContactOnFile:          http.StatusBadRequest,
	service.ErrOnlyDrivers:              http.StatusForbidden,
	service.ErrInsufficientBalance:      http.StatusPaymentRequired,
	service.ErrTopupAmountInvalid:       http.StatusBadRequest,
	service.ErrTopupNotFound:            http.StatusNotFound,
	service.ErrTooManyPendingTopups:     http.StatusTooManyRequests,
	service.ErrPaymentProviderFailed:    http.StatusBadGateway,
	service.ErrPINNotSet:                http.StatusConflict,
	service.ErrPINIncorrect:             http.StatusForbidden, // never 401: the app treats 401 as an expired session
	service.ErrPINLocked:                http.StatusTooManyRequests,
	service.ErrPINWeak:                  http.StatusBadRequest,
	service.ErrPayoutsDisabled:          http.StatusServiceUnavailable,
	service.ErrPayoutAccountNotFound:    http.StatusNotFound,
	service.ErrPayoutAccountNotApproved: http.StatusForbidden,
	service.ErrPayoutAccountLimit:       http.StatusConflict,
	service.ErrInvalidBank:              http.StatusBadRequest,
	service.ErrBankResolveFailed:        http.StatusUnprocessableEntity,
	service.ErrBankServiceUnavailable:   http.StatusServiceUnavailable,
	service.ErrWithdrawalNotFound:       http.StatusNotFound,
	service.ErrWithdrawalAmount:         http.StatusBadRequest,
	service.ErrDailyLimitExceeded:       http.StatusUnprocessableEntity,
	service.ErrCooldownLimit:            http.StatusUnprocessableEntity,
	service.ErrWalletFrozen:             http.StatusForbidden,
	service.ErrDuplicateWithdrawal:      http.StatusConflict,
	service.ErrWithdrawalState:          http.StatusConflict,
	service.ErrEscrowNotFound:           http.StatusNotFound,
	service.ErrEscrowState:              http.StatusConflict,
	service.ErrNotPayer:                 http.StatusForbidden,
	service.ErrNoNextOfKin:              http.StatusNotFound,
	service.ErrNextOfKinInvalid:         http.StatusBadRequest,
	service.ErrAppealNotFound:           http.StatusNotFound,
	service.ErrAppealState:              http.StatusConflict,
	service.ErrAppealTokenInvalid:       http.StatusForbidden,
	service.ErrSameApprover:             http.StatusForbidden,
	service.ErrAppealInvalid:            http.StatusBadRequest,
	service.ErrNothingToAppeal:          http.StatusConflict,
	service.ErrShiftNotFound:            http.StatusNotFound,
	service.ErrShiftClosed:              http.StatusConflict,
	service.ErrInvalidPoint:             http.StatusBadRequest,
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
	// Unrecognized errors become a generic 500 to the client (never leak
	// internals like "smtp authentication failed" to an API consumer),
	// but the real cause still needs to be visible somewhere — otherwise
	// a failure like a misconfigured mail server or a dropped DB
	// connection is undiagnosable from the server's own logs. Gin's
	// request logger only records the status code, not this error.
	log.Printf("handler: unexpected error on %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
	utils.Fail(c, http.StatusInternalServerError, "something went wrong, please try again")
}
