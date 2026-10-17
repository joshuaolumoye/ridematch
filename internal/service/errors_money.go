package service

import "errors"

// Wallet, escrow, payout and next-of-kin errors. Wrap with fmt.Errorf("%w: detail")
// to add specifics; handler/errors.go maps the sentinel to an HTTP status.
var (
	ErrNoContactOnFile = errors.New("no phone number or email is on file for this account")

	ErrOnlyDrivers           = errors.New("only registered drivers can do this")
	ErrInsufficientBalance   = errors.New("your wallet balance is too low")
	ErrTopupAmountInvalid    = errors.New("invalid top-up amount")
	ErrTopupNotFound         = errors.New("top-up not found")
	ErrTooManyPendingTopups  = errors.New("you already have open top-up requests — use one of them or wait for it to expire")
	ErrPaymentProviderFailed = errors.New("the payment provider is unavailable right now, please try again shortly")

	ErrPINNotSet    = errors.New("set a withdrawal PIN first")
	ErrPINIncorrect = errors.New("incorrect PIN")
	ErrPINLocked    = errors.New("too many wrong PIN attempts — try again later")
	ErrPINWeak      = errors.New("PIN is not acceptable")

	ErrPayoutsDisabled          = errors.New("withdrawals are temporarily unavailable, please try again later")
	ErrPayoutAccountNotFound    = errors.New("payout account not found")
	ErrPayoutAccountNotApproved = errors.New("this payout account is waiting for review and can't be used yet")
	ErrPayoutAccountLimit       = errors.New("you can save at most 3 payout accounts")
	ErrInvalidBank              = errors.New("unknown bank")
	ErrBankResolveFailed        = errors.New("we couldn't verify that bank account — check the number and bank")
	ErrBankServiceUnavailable   = errors.New("bank verification is unavailable right now, please try again shortly")

	ErrWithdrawalNotFound  = errors.New("withdrawal not found")
	ErrWithdrawalAmount    = errors.New("invalid withdrawal amount")
	ErrDailyLimitExceeded  = errors.New("this would exceed your daily withdrawal limit")
	ErrCooldownLimit       = errors.New("for your security, withdrawals are limited for a while after you change your PIN or bank account")
	ErrWalletFrozen        = errors.New("withdrawals on this wallet are on hold — contact support")
	ErrDuplicateWithdrawal = errors.New("an identical withdrawal was just submitted")
	ErrWithdrawalState     = errors.New("this withdrawal can't be changed in its current state")

	ErrEscrowNotFound = errors.New("no payment is held for this trip")
	ErrEscrowState    = errors.New("this payment can't be changed in its current state")
	ErrNotPayer       = errors.New("only the passenger who paid can do this")

	ErrNoNextOfKin        = errors.New("no next of kin on file")
	ErrNextOfKinInvalid   = errors.New("invalid next of kin details")
	ErrAppealNotFound     = errors.New("appeal not found")
	ErrAppealState        = errors.New("this appeal can't be changed in its current state")
	ErrAppealTokenInvalid = errors.New("invalid or expired appeal link")
	ErrSameApprover       = errors.New("a different admin must give the final approval")
	ErrAppealInvalid      = errors.New("invalid appeal")
	ErrNothingToAppeal    = errors.New("there is no balance to release")
)
