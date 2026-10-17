package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"ridematch-backend/internal/dto"
	"ridematch-backend/internal/flutterwave"
	"ridematch-backend/internal/models"
	"ridematch-backend/internal/money"
	"ridematch-backend/internal/repository"
)

// WalletConfig carries the tunable money rules (see config.Config).
type WalletConfig struct {
	TopupFeeBps       int
	TopupMinKobo      int64
	TopupMaxKobo      int64
	TopupTTL          time.Duration
	PayoutsEnabled    bool
	PayoutMinKobo     int64
	PayoutDailyLimit  int64
	PayoutCooldown    time.Duration
	PayoutCooldownCap int64
	PayoutReviewKobo  int64
	PINMaxAttempts    int
	PINLockDuration   time.Duration
}

const (
	settingPayoutsEnabled = "payouts_enabled"
	maxPendingTopups      = 3
	maxPayoutAccounts     = 3
	maxDispatchAttempts   = 10
)

// WalletService owns wallets, top-ups (Flutterwave one-time accounts),
// payout accounts, PINs and driver withdrawals. Every balance change goes
// through repository.LedgerRepository, which is atomic and idempotent;
// this layer decides WHETHER a movement is allowed.
type WalletService struct {
	ledger   repository.LedgerRepository
	accounts repository.PayoutAccountRepository
	users    repository.UserRepository
	drivers  repository.DriverRepository
	settings repository.PlatformSettingRepository
	gateway  flutterwave.WalletGateway
	stepUp   *StepUpService
	notify   *NotificationService
	audit    *AuditService
	cfg      WalletConfig

	banksMu    sync.Mutex
	banks      []flutterwave.Bank
	banksUntil time.Time
}

// NewWalletService constructs a WalletService.
func NewWalletService(
	ledger repository.LedgerRepository, accounts repository.PayoutAccountRepository,
	users repository.UserRepository, drivers repository.DriverRepository,
	settings repository.PlatformSettingRepository, gateway flutterwave.WalletGateway,
	stepUp *StepUpService, notify *NotificationService, audit *AuditService, cfg WalletConfig,
) *WalletService {
	return &WalletService{ledger: ledger, accounts: accounts, users: users, drivers: drivers, settings: settings,
		gateway: gateway, stepUp: stepUp, notify: notify, audit: audit, cfg: cfg}
}

// ---------------- helpers ----------------

func (s *WalletService) payoutsEnabled(ctx context.Context) bool {
	if !s.cfg.PayoutsEnabled {
		return false
	}
	v, ok, err := s.settings.Get(ctx, settingPayoutsEnabled)
	if err != nil {
		log.Printf("wallet: failed to read payout switch, failing closed: %v", err)
		return false
	}
	return !ok || v != "false"
}

func (s *WalletService) limits() dto.WalletLimits {
	return dto.WalletLimits{
		TopupMinKobo: s.cfg.TopupMinKobo, TopupMaxKobo: s.cfg.TopupMaxKobo, TopupFeeBps: s.cfg.TopupFeeBps,
		WithdrawMinKobo: s.cfg.PayoutMinKobo, DailyLimitKobo: s.cfg.PayoutDailyLimit,
		CooldownCapKobo: s.cfg.PayoutCooldownCap, ReviewThresholdKobo: s.cfg.PayoutReviewKobo,
	}
}

func (s *WalletService) driverProfile(ctx context.Context, userID string) (*models.DriverProfile, error) {
	p, err := s.drivers.FindByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrOnlyDrivers
		}
		return nil, fmt.Errorf("service: failed to load driver profile: %w", err)
	}
	return p, nil
}

func (s *WalletService) activeUser(ctx context.Context, userID string) (*models.User, error) {
	u, err := s.users.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("service: failed to load user: %w", err)
	}
	switch u.Status {
	case models.StatusSuspended:
		return nil, ErrAccountSuspended
	case models.StatusBanned:
		return nil, ErrAccountBanned
	}
	return u, nil
}

func (s *WalletService) push(ctx context.Context, userID, title, body string, data map[string]string) {
	if s.notify == nil {
		return
	}
	if _, err := s.notify.SendToUser(ctx, userID, models.NotificationWallet, title, body, data); err != nil {
		log.Printf("wallet: notification to %s failed: %v", userID, err)
	}
}

var nowFn = time.Now

func maskAccount(n string) string {
	if len(n) <= 4 {
		return n
	}
	return strings.Repeat("•", len(n)-4) + n[len(n)-4:]
}

// Naira formats kobo as ₦1,234.56 for messages.
func Naira(kobo int64) string {
	whole, frac := kobo/100, kobo%100
	s := fmt.Sprintf("%d", whole)
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)
	out := "₦" + strings.Join(parts, ",")
	if frac != 0 {
		out += fmt.Sprintf(".%02d", frac)
	}
	return out
}

// ---------------- wallet overview & statement ----------------

// Wallet returns the caller's wallet overview (creating it on first use).
func (s *WalletService) Wallet(ctx context.Context, userID string) (*dto.WalletResponse, error) {
	w, err := s.ledger.EnsureWallet(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("service: failed to load wallet: %w", err)
	}
	resp, err := s.walletResponse(ctx, userID, w)
	return resp, err
}

func (s *WalletService) walletResponse(ctx context.Context, userID string, w *models.Wallet) (*dto.WalletResponse, error) {
	heldFor, incoming, pending, err := s.ledger.WalletAggregates(ctx, userID, w.ID)
	if err != nil {
		return nil, fmt.Errorf("service: failed to load wallet totals: %w", err)
	}
	_, drvErr := s.drivers.FindByUserID(ctx, userID)
	isDriver := drvErr == nil
	enabled := s.payoutsEnabled(ctx)
	now := time.Now()
	resp := &dto.WalletResponse{
		BalanceKobo: w.BalanceKobo, HeldForTripsKobo: heldFor, IncomingKobo: incoming, PendingWithdrawalKobo: pending,
		Currency: w.Currency, IsDriver: isDriver, PINSet: w.PINHash != "",
		WithdrawalsFrozen: w.WithdrawalsFrozen, FrozenReason: w.FrozenReason, PayoutsEnabled: enabled,
		CanWithdraw: isDriver && enabled && !w.WithdrawalsFrozen, Limits: s.limits(),
	}
	if w.PINLockedUntil != nil && w.PINLockedUntil.After(now) {
		resp.PINLockedUntil = w.PINLockedUntil
	}
	if w.CooldownUntil != nil && w.CooldownUntil.After(now) {
		resp.CooldownUntil = w.CooldownUntil
	}
	return resp, nil
}

var kindTitles = map[string]string{
	"wallet_topup":      "Wallet top-up",
	"escrow_hold":       "Trip payment held",
	"escrow_release":    "Trip payment received",
	"escrow_refund":     "Trip payment refunded",
	"withdrawal_lock":   "Withdrawal",
	"withdrawal_return": "Withdrawal returned",
}

// Statement returns the caller's wallet history, newest first.
func (s *WalletService) Statement(ctx context.Context, userID string, page, pageSize int) ([]dto.StatementEntryResponse, int64, error) {
	w, err := s.ledger.EnsureWallet(ctx, userID)
	if err != nil {
		return nil, 0, fmt.Errorf("service: failed to load wallet: %w", err)
	}
	page, pageSize = normalizePage(page, pageSize)
	rows, total, err := s.ledger.Statement(ctx, w.ID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("service: failed to load statement: %w", err)
	}
	out := make([]dto.StatementEntryResponse, len(rows))
	for i, r := range rows {
		title := kindTitles[r.Kind]
		if title == "" {
			title = r.Description
		}
		out[i] = dto.StatementEntryResponse{ID: r.ID, Kind: r.Kind, Title: title, AmountKobo: r.AmountKobo,
			BalanceAfter: r.BalanceAfter, RefType: r.RefType, RefID: r.RefID, CreatedAt: r.CreatedAt}
	}
	return out, total, nil
}

// ---------------- top-ups ----------------

// QuoteTopup shows what the payer must transfer for a given wallet credit.
func (s *WalletService) QuoteTopup(amountKobo int64) (*dto.TopupQuoteResponse, error) {
	if err := s.validateTopupAmount(amountKobo); err != nil {
		return nil, err
	}
	gross := money.GrossUp(amountKobo, s.cfg.TopupFeeBps)
	return &dto.TopupQuoteResponse{RequestedKobo: amountKobo, FeeKobo: gross - amountKobo, GrossKobo: gross, FeeBps: s.cfg.TopupFeeBps}, nil
}

func (s *WalletService) validateTopupAmount(a int64) error {
	switch {
	case a < s.cfg.TopupMinKobo:
		return fmt.Errorf("%w: the minimum top-up is %s", ErrTopupAmountInvalid, Naira(s.cfg.TopupMinKobo))
	case a > s.cfg.TopupMaxKobo:
		return fmt.Errorf("%w: the maximum top-up is %s", ErrTopupAmountInvalid, Naira(s.cfg.TopupMaxKobo))
	case a%100 != 0:
		return fmt.Errorf("%w: use a whole-naira amount", ErrTopupAmountInvalid)
	}
	return nil
}

func toTopupResponse(t *models.WalletTopup) dto.TopupResponse {
	r := dto.TopupResponse{
		ID: t.ID, Status: string(t.Status), RequestedKobo: t.RequestedKobo, FeeKobo: t.FeeKobo, GrossKobo: t.GrossKobo,
		CreditedKobo: t.CreditedKobo, CompletedAt: t.CompletedAt, CreatedAt: t.CreatedAt,
	}
	if t.Status == models.TopupPending {
		r.AccountNumber, r.BankName, r.ExpiresAt = t.AccountNumber, t.BankName, t.ExpiresAt
	}
	return r
}

var expiryLayouts = []string{time.RFC3339, "2006-01-02 3:04:05 PM", "2006-01-02 15:04:05", "2006-01-02T15:04:05"}

func parseFlwTime(v string, fallback time.Time) time.Time {
	v = strings.TrimSpace(v)
	for _, l := range expiryLayouts {
		if t, err := time.ParseInLocation(l, v, time.FixedZone("WAT", 3600)); err == nil {
			return t
		}
	}
	return fallback
}

// CreateTopup generates a one-time Flutterwave account number for the payer.
func (s *WalletService) CreateTopup(ctx context.Context, userID string, amountKobo int64) (*dto.TopupResponse, error) {
	if err := s.validateTopupAmount(amountKobo); err != nil {
		return nil, err
	}
	user, err := s.activeUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	// Bound open requests per user: each one is a live bank account number.
	existing, _, err := s.ledger.ListTopupsByUser(ctx, userID, 10, 0)
	if err != nil {
		return nil, fmt.Errorf("service: failed to list top-ups: %w", err)
	}
	open := 0
	for _, t := range existing {
		if t.Status == models.TopupPending && t.ExpiresAt != nil && t.ExpiresAt.After(time.Now()) {
			open++
		}
	}
	if open >= maxPendingTopups {
		return nil, ErrTooManyPendingTopups
	}

	gross := money.GrossUp(amountKobo, s.cfg.TopupFeeBps)
	txRef := "topup_" + uuid.NewString()
	emailAddr := user.Email
	if emailAddr == "" {
		emailAddr = user.ID + "@ridematch.local"
	}
	name := user.Name
	if strings.TrimSpace(name) == "" {
		name = "MakeWeRide User"
	}

	va, err := s.gateway.CreateVirtualAccount(ctx, flutterwave.VirtualAccountRequest{
		TxRef: txRef, AmountKobo: gross, Email: emailAddr, PhoneNumber: user.Phone, FullName: name,
		Narration: "MakeWeRide wallet top-up",
	})
	if err != nil {
		log.Printf("wallet: create virtual account failed for user %s: %v", userID, err)
		return nil, ErrPaymentProviderFailed
	}
	// If Flutterwave adjusted the expected amount, trust it for display and
	// crediting math: the fee is whatever makes the credit exact.
	if va.AmountKobo > 0 && va.AmountKobo != gross {
		log.Printf("wallet: flutterwave expects %d kobo for %s (asked %d)", va.AmountKobo, txRef, gross)
		gross = va.AmountKobo
	}
	exp := parseFlwTime(va.ExpiresAt, time.Now().Add(s.cfg.TopupTTL))
	if exp.After(time.Now().Add(24 * time.Hour)) {
		exp = time.Now().Add(24 * time.Hour)
	}
	t := &models.WalletTopup{
		UserID: userID, TxRef: txRef, Status: models.TopupPending, RequestedKobo: amountKobo,
		FeeKobo: gross - amountKobo, GrossKobo: gross, AccountNumber: va.AccountNumber, BankName: va.BankName, ExpiresAt: &exp,
	}
	if err := s.ledger.CreateTopup(ctx, t); err != nil {
		return nil, fmt.Errorf("service: failed to save top-up: %w", err)
	}
	resp := toTopupResponse(t)
	return &resp, nil
}

// GetTopup returns one top-up. A still-pending one is re-checked directly
// with Flutterwave so a delayed or missed webhook can't strand a payment.
func (s *WalletService) GetTopup(ctx context.Context, userID, id string) (*dto.TopupResponse, error) {
	t, err := s.ledger.FindTopupByID(ctx, id)
	if err != nil || t.UserID != userID {
		if err != nil && !errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("service: failed to load top-up: %w", err)
		}
		return nil, ErrTopupNotFound
	}
	if t.Status == models.TopupPending || t.Status == models.TopupExpired {
		if updated, err := s.settleFromGateway(ctx, t); err != nil {
			log.Printf("wallet: gateway re-check of %s failed: %v", t.TxRef, err)
		} else if updated != nil {
			t = updated
		}
	}
	resp := toTopupResponse(t)
	return &resp, nil
}

// ListTopups returns the caller's top-up history.
func (s *WalletService) ListTopups(ctx context.Context, userID string, page, pageSize int) ([]dto.TopupResponse, int64, error) {
	page, pageSize = normalizePage(page, pageSize)
	rows, total, err := s.ledger.ListTopupsByUser(ctx, userID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("service: failed to list top-ups: %w", err)
	}
	out := make([]dto.TopupResponse, len(rows))
	for i := range rows {
		out[i] = toTopupResponse(&rows[i])
	}
	return out, total, nil
}

// settleFromGateway asks Flutterwave about a top-up by our reference and
// credits the wallet if (and only if) a successful payment is confirmed.
func (s *WalletService) settleFromGateway(ctx context.Context, t *models.WalletTopup) (*models.WalletTopup, error) {
	v, err := s.gateway.VerifyByReference(ctx, t.TxRef)
	if err != nil {
		if errors.Is(err, flutterwave.ErrRejected) {
			return nil, nil // not found there yet: nothing paid
		}
		return nil, err
	}
	return s.creditIfVerified(ctx, t, v)
}

func (s *WalletService) creditIfVerified(ctx context.Context, t *models.WalletTopup, v *flutterwave.VerifyResult) (*models.WalletTopup, error) {
	if !strings.EqualFold(v.Status, "successful") || v.TxRef != t.TxRef || v.Currency != "NGN" || v.AmountKobo <= 0 {
		return nil, nil
	}
	credit := money.NetAfterFee(v.AmountKobo, s.cfg.TopupFeeBps)
	if credit <= 0 {
		return nil, nil
	}
	updated, applied, err := s.ledger.CreditTopup(ctx, t.ID, v.AmountKobo, credit, v.FlwTransactionID)
	if err != nil {
		return nil, err
	}
	if applied {
		s.push(ctx, t.UserID, "Wallet funded", fmt.Sprintf("%s has been added to your wallet.", Naira(credit)),
			map[string]string{"topup_id": t.ID})
	}
	return updated, nil
}

// HandleChargeWebhook processes a verified-signature charge.completed event
// for a wallet top-up. The webhook body is never trusted for the amount: the
// transaction is re-fetched from Flutterwave by id.
func (s *WalletService) HandleChargeWebhook(ctx context.Context, txRef string, flwID string) error {
	t, err := s.ledger.FindTopupByTxRef(ctx, txRef)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrTopupNotFound
		}
		return fmt.Errorf("service: failed to load top-up: %w", err)
	}
	if t.Status == models.TopupSuccessful {
		return nil
	}
	v, err := s.gateway.VerifyTransaction(ctx, flwID)
	if err != nil {
		return fmt.Errorf("service: failed to verify top-up with flutterwave: %w", err)
	}
	if v.TxRef != t.TxRef {
		return ErrPaymentVerificationFailed
	}
	if _, err := s.creditIfVerified(ctx, t, v); err != nil {
		return fmt.Errorf("service: failed to credit top-up: %w", err)
	}
	return nil
}

// ExpireTopups marks stale pending top-ups expired (housekeeping).
func (s *WalletService) ExpireTopups(ctx context.Context) {
	// A grace period: a payer may transfer right at expiry and the bank may
	// settle late. Expired rows can still be credited by GetTopup/webhook.
	if _, err := s.ledger.ExpireStaleTopups(ctx, time.Now().Add(-10*time.Minute)); err != nil {
		log.Printf("wallet: expire top-ups failed: %v", err)
	}
}

// ---------------- PIN ----------------

// RequestPINCode sends the step-up code for setting/resetting the PIN.
func (s *WalletService) RequestPINCode(ctx context.Context, userID string) (*dto.StepUpOTPResponse, error) {
	if _, err := s.driverProfile(ctx, userID); err != nil {
		return nil, err
	}
	return s.stepUp.Request(ctx, userID, models.OTPPurposeSetPIN)
}

// SetPIN sets or resets the withdrawal PIN (drivers only). Because a PIN
// change is exactly what an account-takeover would do first, it triggers the
// withdrawal cooldown.
func (s *WalletService) SetPIN(ctx context.Context, userID, pin, otp string) error {
	if _, err := s.driverProfile(ctx, userID); err != nil {
		return err
	}
	if issue := money.PINIssue(pin); issue != "" {
		return fmt.Errorf("%w: %s", ErrPINWeak, issue)
	}
	if err := s.stepUp.Verify(ctx, userID, models.OTPPurposeSetPIN, otp); err != nil {
		return err
	}
	w, err := s.ledger.EnsureWallet(ctx, userID)
	if err != nil {
		return fmt.Errorf("service: failed to load wallet: %w", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("service: failed to hash PIN: %w", err)
	}
	now := time.Now()
	cool := now.Add(s.cfg.PayoutCooldown)
	w.PINHash, w.PINSetAt, w.PINFailedAttempts, w.PINLockedUntil, w.CooldownUntil = string(hash), &now, 0, nil, &cool
	if err := s.ledger.UpdateWalletSecurity(ctx, w); err != nil {
		return fmt.Errorf("service: failed to save PIN: %w", err)
	}
	s.audit.Record(ctx, Actor{UserID: userID, Role: "driver"}, "wallet.pin_set", "wallet", w.ID, nil)
	s.push(ctx, userID, "Withdrawal PIN updated", "Your withdrawal PIN was set. For your security, withdrawals are limited for the next 24 hours. If this wasn't you, contact support immediately.", nil)
	return nil
}

func (s *WalletService) checkPIN(ctx context.Context, w *models.Wallet, pin string) error {
	if w.PINHash == "" {
		return ErrPINNotSet
	}
	if w.PINLockedUntil != nil && w.PINLockedUntil.After(time.Now()) {
		return fmt.Errorf("%w (until %s)", ErrPINLocked, w.PINLockedUntil.In(time.FixedZone("WAT", 3600)).Format("15:04"))
	}
	if bcrypt.CompareHashAndPassword([]byte(w.PINHash), []byte(pin)) != nil {
		attempts, lockedUntil, err := s.ledger.RecordPINFailure(ctx, w.ID, s.cfg.PINMaxAttempts, s.cfg.PINLockDuration)
		if err != nil {
			return fmt.Errorf("service: failed to record PIN failure: %w", err)
		}
		if lockedUntil != nil {
			s.audit.Record(ctx, Actor{UserID: w.UserID}, "wallet.pin_locked", "wallet", w.ID, nil)
			s.push(ctx, w.UserID, "Withdrawal PIN locked", "Too many wrong PIN attempts. Withdrawals are paused for a while. If this wasn't you, contact support.", nil)
			return ErrPINLocked
		}
		return fmt.Errorf("%w (%d attempts left)", ErrPINIncorrect, s.cfg.PINMaxAttempts-attempts)
	}
	if w.PINFailedAttempts > 0 {
		w.PINFailedAttempts = 0
		if err := s.ledger.UpdateWalletSecurity(ctx, w); err != nil {
			log.Printf("wallet: failed to reset PIN attempts: %v", err)
		}
	}
	return nil
}

// ---------------- banks & payout accounts ----------------

// Banks returns Nigeria's bank list (cached).
func (s *WalletService) Banks(ctx context.Context) ([]dto.BankResponse, error) {
	s.banksMu.Lock()
	defer s.banksMu.Unlock()
	if len(s.banks) == 0 || time.Now().After(s.banksUntil) {
		banks, err := s.gateway.ListBanks(ctx)
		if err != nil {
			if len(s.banks) == 0 {
				log.Printf("wallet: bank list unavailable: %v", err)
				return nil, ErrBankServiceUnavailable
			}
		} else {
			s.banks, s.banksUntil = banks, time.Now().Add(6*time.Hour)
		}
	}
	out := make([]dto.BankResponse, len(s.banks))
	for i, b := range s.banks {
		out[i] = dto.BankResponse{Code: b.Code, Name: b.Name}
	}
	return out, nil
}

func (s *WalletService) bankName(ctx context.Context, code string) (string, error) {
	banks, err := s.Banks(ctx)
	if err != nil {
		return "", err
	}
	for _, b := range banks {
		if b.Code == code {
			return b.Name, nil
		}
	}
	return "", ErrInvalidBank
}

func (s *WalletService) resolve(ctx context.Context, bankCode, number string) (string, error) {
	name, err := s.gateway.ResolveAccount(ctx, number, bankCode)
	if err != nil {
		if errors.Is(err, flutterwave.ErrRejected) {
			return "", ErrBankResolveFailed
		}
		log.Printf("wallet: resolve account failed: %v", err)
		return "", ErrBankServiceUnavailable
	}
	return name, nil
}

// ResolveAccount looks up the name a bank holds for an account (drivers only).
func (s *WalletService) ResolveAccount(ctx context.Context, userID, bankCode, number string) (*dto.ResolveAccountResponse, error) {
	if _, err := s.driverProfile(ctx, userID); err != nil {
		return nil, err
	}
	if _, err := s.bankName(ctx, bankCode); err != nil {
		return nil, err
	}
	name, err := s.resolve(ctx, bankCode, number)
	if err != nil {
		return nil, err
	}
	return &dto.ResolveAccountResponse{AccountName: name}, nil
}

// RequestPayoutAccountCode sends the step-up code for adding a bank account.
func (s *WalletService) RequestPayoutAccountCode(ctx context.Context, userID string) (*dto.StepUpOTPResponse, error) {
	if _, err := s.driverProfile(ctx, userID); err != nil {
		return nil, err
	}
	return s.stepUp.Request(ctx, userID, models.OTPPurposePayoutAcct)
}

func toPayoutAccountResponse(a *models.PayoutAccount) dto.PayoutAccountResponse {
	note := ""
	switch a.Status {
	case models.PayoutAccountPendingReview:
		note = "The account name doesn't match your registered name, so it's being reviewed by our team."
	case models.PayoutAccountRejected:
		note = "This account was not approved."
	}
	return dto.PayoutAccountResponse{ID: a.ID, BankCode: a.BankCode, BankName: a.BankName, AccountNumber: maskAccount(a.AccountNumber),
		AccountName: a.AccountName, Status: string(a.Status), StatusNote: note, CreatedAt: a.CreatedAt}
}

// AddPayoutAccount saves a bank account after verifying the account holder
// name with the bank. The name must match the driver's registered name or the
// account waits for admin review. Adding an account triggers the cooldown.
func (s *WalletService) AddPayoutAccount(ctx context.Context, userID string, req dto.AddPayoutAccountRequest) (*dto.PayoutAccountResponse, error) {
	if _, err := s.driverProfile(ctx, userID); err != nil {
		return nil, err
	}
	user, err := s.activeUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	existing, err := s.accounts.ListByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("service: failed to list payout accounts: %w", err)
	}
	if len(existing) >= maxPayoutAccounts {
		return nil, ErrPayoutAccountLimit
	}
	bankName, err := s.bankName(ctx, req.BankCode)
	if err != nil {
		return nil, err
	}
	if err := s.stepUp.Verify(ctx, userID, models.OTPPurposePayoutAcct, req.OTP); err != nil {
		return nil, err
	}
	accName, err := s.resolve(ctx, req.BankCode, req.AccountNumber)
	if err != nil {
		return nil, err
	}

	status, note := models.PayoutAccountActive, ""
	if !money.NamesMatch(user.Name, accName) {
		status, note = models.PayoutAccountPendingReview, "account name does not match the registered name"
	}
	saved, err := s.accounts.Upsert(ctx, &models.PayoutAccount{
		UserID: userID, BankCode: req.BankCode, AccountNumber: req.AccountNumber, BankName: bankName,
		AccountName: accName, Status: status, NameMatchNote: note,
	})
	if err != nil {
		return nil, fmt.Errorf("service: failed to save payout account: %w", err)
	}

	w, err := s.ledger.EnsureWallet(ctx, userID)
	if err == nil {
		cool := time.Now().Add(s.cfg.PayoutCooldown)
		w.CooldownUntil = &cool
		if err := s.ledger.UpdateWalletSecurity(ctx, w); err != nil {
			log.Printf("wallet: failed to set cooldown: %v", err)
		}
	}
	s.audit.Record(ctx, Actor{UserID: userID, Role: "driver"}, "payout_account.add", "payout_account", saved.ID,
		map[string]interface{}{"status": status, "bank": bankName})
	s.push(ctx, userID, "Bank account added", fmt.Sprintf("%s ••••%s was added for withdrawals. If this wasn't you, contact support immediately.", bankName, req.AccountNumber[len(req.AccountNumber)-4:]), nil)
	resp := toPayoutAccountResponse(saved)
	return &resp, nil
}

// ListPayoutAccounts returns the caller's saved bank accounts.
func (s *WalletService) ListPayoutAccounts(ctx context.Context, userID string) ([]dto.PayoutAccountResponse, error) {
	rows, err := s.accounts.ListByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("service: failed to list payout accounts: %w", err)
	}
	out := make([]dto.PayoutAccountResponse, len(rows))
	for i := range rows {
		out[i] = toPayoutAccountResponse(&rows[i])
	}
	return out, nil
}

// RemovePayoutAccount removes a saved account (PIN required).
func (s *WalletService) RemovePayoutAccount(ctx context.Context, userID, id, pin string) error {
	if _, err := s.driverProfile(ctx, userID); err != nil {
		return err
	}
	w, err := s.ledger.EnsureWallet(ctx, userID)
	if err != nil {
		return fmt.Errorf("service: failed to load wallet: %w", err)
	}
	if err := s.checkPIN(ctx, w, pin); err != nil {
		return err
	}
	a, err := s.accounts.FindByID(ctx, id)
	if err != nil || a.UserID != userID || a.Status == models.PayoutAccountRemoved {
		return ErrPayoutAccountNotFound
	}
	a.Status = models.PayoutAccountRemoved
	if err := s.accounts.Update(ctx, a); err != nil {
		return fmt.Errorf("service: failed to remove payout account: %w", err)
	}
	s.audit.Record(ctx, Actor{UserID: userID, Role: "driver"}, "payout_account.remove", "payout_account", a.ID, nil)
	return nil
}

// ---------------- withdrawals ----------------

// QuoteWithdrawal previews fee and net for an amount.
func (s *WalletService) QuoteWithdrawal(amountKobo int64) (*dto.WithdrawQuoteResponse, error) {
	if amountKobo < s.cfg.PayoutMinKobo {
		return nil, fmt.Errorf("%w: the minimum withdrawal is %s", ErrWithdrawalAmount, Naira(s.cfg.PayoutMinKobo))
	}
	if amountKobo%100 != 0 {
		return nil, fmt.Errorf("%w: use a whole-naira amount", ErrWithdrawalAmount)
	}
	fee := money.TransferFee(amountKobo)
	if amountKobo <= fee {
		return nil, ErrWithdrawalAmount
	}
	return &dto.WithdrawQuoteResponse{AmountKobo: amountKobo, FeeKobo: fee, NetKobo: amountKobo - fee}, nil
}

func toWithdrawalResponse(w *models.Withdrawal) dto.WithdrawalResponse {
	note := ""
	switch w.Status {
	case models.WithdrawalPendingReview:
		note = "Waiting for review by our team"
	case models.WithdrawalQueued, models.WithdrawalProcessing:
		note = "On its way to your bank"
	case models.WithdrawalNeedsAttention:
		note = "Our team is confirming this payout with the bank"
	case models.WithdrawalFailed, models.WithdrawalRejected:
		note = "Your money was returned to your wallet"
	}
	return dto.WithdrawalResponse{ID: w.ID, Status: string(w.Status), AmountKobo: w.AmountKobo, FeeKobo: w.FeeKobo, NetKobo: w.NetKobo,
		BankName: w.BankName, AccountNumber: maskAccount(w.AccountNumber), AccountName: w.AccountName, StatusNote: note,
		CreatedAt: w.CreatedAt, CompletedAt: w.CompletedAt}
}

// Withdraw pays a driver's wallet balance out to one of their verified bank
// accounts. Only drivers may withdraw. Every guard below is enforced
// server-side; the money is locked in the same transaction that records the
// request, so a crash can never lose or duplicate it.
func (s *WalletService) Withdraw(ctx context.Context, userID string, req dto.WithdrawRequest, ip string) (*dto.WithdrawalResponse, error) {
	if _, err := s.driverProfile(ctx, userID); err != nil {
		return nil, err
	}
	if _, err := s.activeUser(ctx, userID); err != nil {
		return nil, err
	}
	if !s.payoutsEnabled(ctx) {
		return nil, ErrPayoutsDisabled
	}
	quote, err := s.QuoteWithdrawal(req.AmountKobo)
	if err != nil {
		return nil, err
	}
	w, err := s.ledger.EnsureWallet(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("service: failed to load wallet: %w", err)
	}
	if w.WithdrawalsFrozen {
		return nil, ErrWalletFrozen
	}
	if err := s.checkPIN(ctx, w, req.PIN); err != nil {
		return nil, err
	}
	acct, err := s.accounts.FindByID(ctx, req.PayoutAccountID)
	if err != nil || acct.UserID != userID || acct.Status == models.PayoutAccountRemoved {
		return nil, ErrPayoutAccountNotFound
	}
	if acct.Status != models.PayoutAccountActive {
		return nil, ErrPayoutAccountNotApproved
	}
	if w.BalanceKobo < req.AmountKobo {
		return nil, ErrInsufficientBalance
	}

	now := time.Now()
	guard := repository.WithdrawalGuard{DailyLimitKobo: s.cfg.PayoutDailyLimit, Now: now, DuplicateWindow: 45 * time.Second}
	flags := []string{}
	if w.CooldownUntil != nil && w.CooldownUntil.After(now) {
		guard.CooldownCap = s.cfg.PayoutCooldownCap
		flags = append(flags, "cooldown")
	}
	status := models.WithdrawalQueued
	if req.AmountKobo >= s.cfg.PayoutReviewKobo {
		status = models.WithdrawalPendingReview
		flags = append(flags, "large_amount")
	}
	wd := &models.Withdrawal{
		UserID: userID, WalletID: w.ID, Kind: models.WithdrawalKindDriver, Status: status,
		AmountKobo: quote.AmountKobo, FeeKobo: quote.FeeKobo, NetKobo: quote.NetKobo,
		PayoutAccountID: &acct.ID, BankCode: acct.BankCode, BankName: acct.BankName,
		AccountNumber: acct.AccountNumber, AccountName: acct.AccountName,
		Reference: "wd_" + uuid.NewString(), RiskFlags: strings.Join(flags, ","), RequestIP: ip,
	}
	if err := s.ledger.CreateWithdrawal(ctx, wd, guard); err != nil {
		switch {
		case errors.Is(err, repository.ErrInsufficientFunds):
			return nil, ErrInsufficientBalance
		case errors.Is(err, repository.ErrDailyLimit):
			return nil, fmt.Errorf("%w (%s per 24 hours)", ErrDailyLimitExceeded, Naira(s.cfg.PayoutDailyLimit))
		case errors.Is(err, repository.ErrCooldownCap):
			return nil, fmt.Errorf("%w: up to %s for now", ErrCooldownLimit, Naira(s.cfg.PayoutCooldownCap))
		case errors.Is(err, repository.ErrWalletFrozen):
			return nil, ErrWalletFrozen
		case errors.Is(err, repository.ErrDuplicateWithdrawal):
			return nil, ErrDuplicateWithdrawal
		}
		return nil, fmt.Errorf("service: failed to create withdrawal: %w", err)
	}
	s.audit.Record(ctx, Actor{UserID: userID, Role: "driver", IP: ip}, "withdrawal.request", "withdrawal", wd.ID,
		map[string]interface{}{"amount_kobo": wd.AmountKobo, "status": status, "flags": wd.RiskFlags})

	if status == models.WithdrawalQueued {
		go s.dispatchDetached(wd.ID)
	} else {
		s.push(ctx, userID, "Withdrawal under review", fmt.Sprintf("Your withdrawal of %s is being reviewed and will be processed shortly.", Naira(wd.AmountKobo)), nil)
	}
	resp := toWithdrawalResponse(wd)
	return &resp, nil
}

// ListWithdrawals returns the caller's payout history.
func (s *WalletService) ListWithdrawals(ctx context.Context, userID string, page, pageSize int) ([]dto.WithdrawalResponse, int64, error) {
	page, pageSize = normalizePage(page, pageSize)
	rows, total, err := s.ledger.ListWithdrawalsByUser(ctx, userID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("service: failed to list withdrawals: %w", err)
	}
	out := make([]dto.WithdrawalResponse, len(rows))
	for i := range rows {
		out[i] = toWithdrawalResponse(&rows[i])
	}
	return out, total, nil
}

func (s *WalletService) dispatchDetached(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("wallet: panic dispatching withdrawal %s: %v", id, r)
		}
	}()
	s.Dispatch(ctx, id)
}

// Dispatch sends one queued withdrawal to Flutterwave. It is safe to call
// concurrently or repeatedly: the queued→processing compare-and-swap lets
// exactly one caller through, and the transfer reference is unique so
// Flutterwave itself refuses a duplicate.
//
// Failure policy (the important part for safety):
//   - a definitive refusal (HTTP 4xx) returns the money to the wallet;
//   - "insufficient balance" on OUR Flutterwave account is not the
//     driver's fault: the withdrawal goes back to the queue and is retried
//     once the float is topped up;
//   - anything ambiguous (timeout, 5xx) leaves the withdrawal "processing"
//     — the webhook or the poller settles it. It is NEVER auto-refunded
//     (the transfer may have gone through) and NEVER re-sent.
func (s *WalletService) Dispatch(ctx context.Context, id string) {
	moved, err := s.ledger.TransitionWithdrawal(ctx, id, []models.WithdrawalStatus{models.WithdrawalQueued}, models.WithdrawalProcessing,
		map[string]interface{}{"sent_at": time.Now()})
	if err != nil {
		log.Printf("wallet: dispatch %s: could not claim: %v", id, err)
		return
	}
	if !moved {
		return
	}
	wd, err := s.ledger.FindWithdrawalByID(ctx, id)
	if err != nil {
		log.Printf("wallet: dispatch %s: reload failed: %v", id, err)
		return
	}
	tr, err := s.gateway.CreateTransfer(ctx, flutterwave.TransferRequest{
		Reference: wd.Reference, BankCode: wd.BankCode, AccountNumber: wd.AccountNumber,
		AmountKobo: wd.NetKobo, Narration: "MakeWeRide withdrawal",
	})
	switch {
	case err == nil:
		s.applyTransferState(ctx, wd, tr)
	case errors.Is(err, flutterwave.ErrRejected):
		msg := err.Error()
		lower := strings.ToLower(msg)
		if strings.Contains(lower, "insufficient") || strings.Contains(lower, "balance") {
			log.Printf("ALERT wallet: flutterwave balance too low to pay withdrawal %s: %s", id, msg)
			if wd.AttemptCount >= maxDispatchAttempts {
				_, _ = s.ledger.TransitionWithdrawal(ctx, id, []models.WithdrawalStatus{models.WithdrawalProcessing}, models.WithdrawalNeedsAttention,
					map[string]interface{}{"failure_reason": "payout provider balance too low"})
				return
			}
			_, _ = s.ledger.TransitionWithdrawal(ctx, id, []models.WithdrawalStatus{models.WithdrawalProcessing}, models.WithdrawalQueued,
				map[string]interface{}{"failure_reason": "waiting for payout float"})
			return
		}
		s.failWithdrawal(ctx, wd, models.WithdrawalFailed, "the bank or payment provider rejected the transfer: "+msg, "")
	default:
		log.Printf("wallet: transfer for %s is in an unknown state (will be settled by webhook/poller): %v", id, err)
	}
}

func (s *WalletService) applyTransferState(ctx context.Context, wd *models.Withdrawal, tr *flutterwave.Transfer) {
	if tr.ID != "" && tr.ID != "0" && wd.FlwTransferID == "" {
		_, _ = s.ledger.TransitionWithdrawal(ctx, wd.ID,
			[]models.WithdrawalStatus{models.WithdrawalProcessing, models.WithdrawalNeedsAttention}, wd.Status,
			map[string]interface{}{"flw_transfer_id": tr.ID})
		wd.FlwTransferID = tr.ID
	}
	switch tr.Status {
	case "SUCCESSFUL":
		s.succeedWithdrawal(ctx, wd, tr.ID)
	case "FAILED":
		s.failWithdrawal(ctx, wd, models.WithdrawalFailed, "the transfer failed: "+tr.Message, "")
	}
}

func (s *WalletService) succeedWithdrawal(ctx context.Context, wd *models.Withdrawal, flwID string) {
	applied, err := s.ledger.FinalizeWithdrawalSuccess(ctx, wd.ID, flwID)
	if err != nil {
		log.Printf("ALERT wallet: could not finalise successful withdrawal %s: %v", wd.ID, err)
		_, _ = s.ledger.TransitionWithdrawal(ctx, wd.ID,
			[]models.WithdrawalStatus{models.WithdrawalProcessing, models.WithdrawalQueued}, models.WithdrawalNeedsAttention,
			map[string]interface{}{"failure_reason": "bank reported success but finalisation failed: " + err.Error()})
		return
	}
	if applied {
		s.push(ctx, wd.UserID, "Withdrawal successful", fmt.Sprintf("%s has been sent to %s ••••%s.", Naira(wd.NetKobo), wd.BankName, wd.AccountNumber[len(wd.AccountNumber)-4:]),
			map[string]string{"withdrawal_id": wd.ID})
	}
}

func (s *WalletService) failWithdrawal(ctx context.Context, wd *models.Withdrawal, final models.WithdrawalStatus, reason, reviewer string) {
	applied, err := s.ledger.FinalizeWithdrawalFailure(ctx, wd.ID, final, reason, reviewer)
	if err != nil {
		log.Printf("ALERT wallet: could not return funds for withdrawal %s: %v", wd.ID, err)
		return
	}
	if applied {
		s.push(ctx, wd.UserID, "Withdrawal not completed", fmt.Sprintf("Your withdrawal of %s couldn't be completed. The money is back in your wallet.", Naira(wd.AmountKobo)),
			map[string]string{"withdrawal_id": wd.ID})
	}
}

// HandleTransferWebhook processes a verified-signature transfer.completed
// event. The reported outcome is confirmed with Flutterwave before acting.
func (s *WalletService) HandleTransferWebhook(ctx context.Context, reference, flwID string) error {
	wd, err := s.ledger.FindWithdrawalByReference(ctx, reference)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil // not ours (e.g. another product on the same account)
		}
		return fmt.Errorf("service: failed to load withdrawal: %w", err)
	}
	if wd.IsTerminal() {
		return nil
	}
	if flwID == "" {
		flwID = wd.FlwTransferID
	}
	if flwID == "" {
		return nil
	}
	tr, err := s.gateway.GetTransfer(ctx, flwID)
	if err != nil {
		return fmt.Errorf("service: failed to verify transfer with flutterwave: %w", err)
	}
	if tr.Reference != wd.Reference {
		return ErrPaymentVerificationFailed
	}
	s.applyTransferState(ctx, wd, tr)
	return nil
}

// PollWithdrawals settles withdrawals whose webhook never arrived, resends
// queued ones, and flags ones stuck in an unknown state for an admin.
func (s *WalletService) PollWithdrawals(ctx context.Context) {
	queued, err := s.ledger.WithdrawalsToDispatch(ctx, time.Now().Add(-30*time.Second), 50)
	if err != nil {
		log.Printf("wallet: queued scan failed: %v", err)
	}
	if s.payoutsEnabled(ctx) {
		for _, wd := range queued {
			s.Dispatch(ctx, wd.ID)
		}
	}

	inflight, err := s.ledger.WithdrawalsInFlightBefore(ctx, time.Now().Add(-2*time.Minute), 100)
	if err != nil {
		log.Printf("wallet: in-flight scan failed: %v", err)
		return
	}
	for i := range inflight {
		wd := &inflight[i]
		if wd.FlwTransferID == "" {
			// We never learned the provider's id. After a generous window,
			// hand it to a human rather than guessing.
			if time.Since(wd.UpdatedAt) > 20*time.Minute {
				log.Printf("ALERT wallet: withdrawal %s has no provider id after 20 minutes", wd.ID)
				_, _ = s.ledger.TransitionWithdrawal(ctx, wd.ID, []models.WithdrawalStatus{models.WithdrawalProcessing}, models.WithdrawalNeedsAttention,
					map[string]interface{}{"failure_reason": "no confirmation from the payment provider"})
			}
			continue
		}
		tr, err := s.gateway.GetTransfer(ctx, wd.FlwTransferID)
		if err != nil {
			log.Printf("wallet: poll transfer %s failed: %v", wd.FlwTransferID, err)
			continue
		}
		if tr.Reference != wd.Reference {
			continue
		}
		s.applyTransferState(ctx, wd, tr)
	}
}
