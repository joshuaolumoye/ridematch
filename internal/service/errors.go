// Package service contains business logic. Handlers call into services;
// services call into repositories. Services never touch gin.Context or
// GORM directly, which keeps business rules testable and transport-agnostic.
package service

import "errors"

var (
	// ErrOTPCooldown is returned when a new OTP is requested before the
	// resend cooldown on the previous one has elapsed.
	ErrOTPCooldown = errors.New("please wait before requesting another code")

	// ErrOTPNotFound is returned when there is no active OTP challenge to
	// verify against (never requested, or already expired/consumed).
	ErrOTPNotFound = errors.New("no active verification code found, please request a new one")

	// ErrOTPIncorrect is returned when the submitted code doesn't match.
	ErrOTPIncorrect = errors.New("incorrect verification code")

	// ErrOTPTooManyAttempts is returned when too many wrong codes have
	// been submitted against the current challenge.
	ErrOTPTooManyAttempts = errors.New("too many incorrect attempts, please request a new code")

	// ErrAccountSuspended / ErrAccountBanned surface account-status
	// restrictions at login time.
	ErrAccountSuspended = errors.New("this account has been suspended")
	ErrAccountBanned    = errors.New("this account has been banned")

	// ErrInvalidRefreshToken covers a refresh token that doesn't exist,
	// has expired, or was already revoked.
	ErrInvalidRefreshToken = errors.New("invalid or expired refresh token")

	// ErrDriverProfileNotFound is returned when an operation requires an
	// existing DriverProfile (e.g. going online) but the caller hasn't
	// registered as a driver yet.
	ErrDriverProfileNotFound = errors.New("driver profile not found, please register as a driver first")

	// ErrDriverAlreadyRegistered is returned when a user who already has
	// a DriverProfile calls the registration endpoint again.
	ErrDriverAlreadyRegistered = errors.New("this account is already registered as a driver")

	// ErrPlateNumberTaken is returned when the submitted plate number is
	// already registered to another driver.
	ErrPlateNumberTaken = errors.New("this plate number is already registered")

	// ErrDriverNotApproved is returned when a driver tries to go online
	// before their submitted documents have passed manual verification.
	ErrDriverNotApproved = errors.New("your driver account is still pending verification")

	// ErrSubscriptionInactive is returned when a driver tries to go
	// online without an active daily platform-access subscription.
	ErrSubscriptionInactive = errors.New("your platform access subscription is inactive, please renew to go online")

	// ErrNotOnline is returned when a location ping arrives for a driver
	// who is not currently marked online.
	ErrNotOnline = errors.New("you must go online before sending location updates")

	// ErrTripNotFound is returned when a trip ID doesn't exist.
	ErrTripNotFound = errors.New("trip not found")

	// ErrTripNotOpen is returned when an action requires the trip to
	// still be accepting offers (status=requested) but it no longer is.
	ErrTripNotOpen = errors.New("this trip is no longer open")

	// ErrTripAlreadyActive is returned when a passenger tries to create a
	// new trip while they already have one in progress, or a driver tries
	// to accept/be matched while already on an active trip.
	ErrTripAlreadyActive = errors.New("you already have an active trip in progress")

	// ErrOfferNotFound is returned when an offer ID doesn't exist on the
	// given trip.
	ErrOfferNotFound = errors.New("offer not found")

	// ErrOfferNotPending is returned when an action (accept/reject)
	// targets an offer that's already been resolved.
	ErrOfferNotPending = errors.New("this offer is no longer pending")

	// ErrNotTripOwner / ErrNotMatchedDriver are authorization errors: the
	// caller isn't the passenger who owns the trip, or isn't the driver
	// matched to it.
	ErrNotTripOwner     = errors.New("you do not have access to this trip")
	ErrNotMatchedDriver = errors.New("you are not the driver matched to this trip")

	// ErrTripNotMatched / ErrTripNotPickedUp gate actions that require a
	// specific prior state (confirming pickup requires "matched";
	// completing requires "picked_up").
	ErrTripNotMatched    = errors.New("this trip has not been matched to a driver yet")
	ErrTripNotPickedUp   = errors.New("pickup has not been confirmed for this trip yet")
	ErrTripNotCancelable = errors.New("this trip can no longer be cancelled")

	// ErrInvalidPickupPIN is returned when the submitted PIN doesn't
	// match the one generated for this trip.
	ErrInvalidPickupPIN = errors.New("incorrect pickup PIN")

	// ErrTripNotCompleted is returned when a rating is submitted for a
	// trip that hasn't reached "completed" status yet.
	ErrTripNotCompleted = errors.New("this trip has not been completed yet")

	// ErrAlreadyRated is returned when the caller's side of a trip has
	// already submitted a rating for it.
	ErrAlreadyRated = errors.New("you have already rated this trip")

	// ErrInvalidWebhookSignature is returned when a Flutterwave webhook
	// request's "verif-hash" header doesn't match the configured secret
	// — either a misconfiguration or (more likely) not actually from
	// Flutterwave. The request is rejected before any business logic
	// runs.
	ErrInvalidWebhookSignature = errors.New("invalid webhook signature")

	// ErrPaymentNotFound is returned when a webhook or verification
	// references a tx_ref we have no PaymentTransaction record for.
	ErrPaymentNotFound = errors.New("payment transaction not found")

	// ErrPaymentVerificationFailed is returned when Flutterwave's own
	// server-to-server transaction-verify call doesn't corroborate what
	// the webhook claimed (status, amount, or currency mismatch) — the
	// payment is never credited in this case.
	ErrPaymentVerificationFailed = errors.New("payment could not be verified")
)
