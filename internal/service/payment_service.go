package service

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"ridematch-backend/internal/dto"
	"ridematch-backend/internal/flutterwave"
	"ridematch-backend/internal/models"
	"ridematch-backend/internal/repository"
)

// PaymentService handles the driver daily-access subscription payment
// flow: creating a Flutterwave hosted-checkout link, and processing the
// resulting webhook once payment completes.
//
// Security model for the webhook: Flutterwave signs nothing cryptographic
// on the payload itself — it sends back a shared secret string (configured
// in both the Flutterwave dashboard and FLW_WEBHOOK_SECRET_HASH) in the
// "verif-hash" header. That proves the request came from a party who knows
// the secret, but a webhook body is still not trusted at face value:
// before crediting anything, HandleWebhook independently re-fetches the
// transaction directly from Flutterwave's API using the secret key
// (VerifyTransaction) and checks status/amount/currency there too. This
// two-factor check (valid header + server-to-server verify) is
// Flutterwave's own recommended pattern and is what makes this endpoint
// safe to expose publicly with no other auth.
type PaymentService struct {
	payments repository.PaymentRepository
	drivers  repository.DriverRepository
	admin    *AdminService
	gateway  flutterwave.Gateway

	dailyFeeKobo       int64
	webhookSecret      string
	defaultRedirectURL string
}

// NewPaymentService constructs a PaymentService.
func NewPaymentService(
	payments repository.PaymentRepository,
	drivers repository.DriverRepository,
	admin *AdminService,
	gateway flutterwave.Gateway,
	dailyFeeKobo int64,
	webhookSecret string,
	defaultRedirectURL string,
) *PaymentService {
	return &PaymentService{
		payments:           payments,
		drivers:            drivers,
		admin:              admin,
		gateway:            gateway,
		dailyFeeKobo:       dailyFeeKobo,
		webhookSecret:      webhookSecret,
		defaultRedirectURL: defaultRedirectURL,
	}
}

// InitiateSubscriptionCheckout creates a Flutterwave hosted-checkout link
// for a driver to pay for N days of platform access. Nothing is granted
// yet — access is only activated once the webhook confirms payment.
func (s *PaymentService) InitiateSubscriptionCheckout(ctx context.Context, driverUserID string, req dto.InitiateSubscriptionRequest) (*dto.InitiateSubscriptionResponse, error) {
	profile, err := s.drivers.FindByUserID(ctx, driverUserID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrDriverProfileNotFound
		}
		return nil, fmt.Errorf("service: failed to load driver profile: %w", err)
	}

	days := req.Days
	if days <= 0 {
		days = 1
	}
	amountKobo := s.dailyFeeKobo * int64(days)

	redirectURL := req.RedirectURL
	if redirectURL == "" {
		redirectURL = s.defaultRedirectURL
	}

	txRef := "sub_" + uuid.NewString()

	// Flutterwave requires a customer email. Use the driver's real one if
	// they signed up with email; otherwise (a phone-only account) it
	// commonly has none on file, so synthesize a stable, non-deliverable
	// placeholder instead. It's only used to populate the hosted checkout
	// page and Flutterwave's own receipt — never relied on for anything
	// in this app.
	email := profile.User.Email
	if email == "" {
		key := profile.User.Phone
		if key == "" {
			key = profile.User.ID
		}
		email = key + "@ridematch.local"
	}

	paymentLink, err := s.gateway.InitiatePayment(ctx, flutterwave.InitiatePaymentRequest{
		TxRef:       txRef,
		Amount:      formatNaira(amountKobo),
		Currency:    "NGN",
		RedirectURL: redirectURL,
		Customer: flutterwave.Customer{
			Email:       email,
			PhoneNumber: profile.User.Phone,
			Name:        profile.User.Name,
		},
		Customizations: &flutterwave.Customizations{
			Title:       "RideMatch Driver Access",
			Description: fmt.Sprintf("%d day(s) of platform access", days),
		},
		Meta: map[string]string{
			"driver_id": profile.ID,
			"purpose":   string(models.PurposeDriverSubscription),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("service: failed to initiate payment: %w", err)
	}

	tx := &models.PaymentTransaction{
		DriverID:      profile.ID,
		TxRef:         txRef,
		Purpose:       models.PurposeDriverSubscription,
		AmountKobo:    amountKobo,
		Currency:      "NGN",
		DaysRequested: days,
		Status:        models.PaymentInitiated,
	}
	if err := s.payments.Create(ctx, tx); err != nil {
		return nil, fmt.Errorf("service: failed to record payment transaction: %w", err)
	}

	return &dto.InitiateSubscriptionResponse{
		PaymentLink: paymentLink,
		TxRef:       txRef,
		AmountKobo:  amountKobo,
		Days:        days,
	}, nil
}

// HandleWebhook processes a Flutterwave webhook call. See the package-level
// doc comment for the two-factor verification approach. Returns nil for
// any event this app doesn't act on (e.g. a different event type, or a
// non-successful charge) — that's a deliberate no-op, not an error, so the
// handler still acknowledges receipt with 200 and Flutterwave doesn't
// retry indefinitely.
func (s *PaymentService) HandleWebhook(ctx context.Context, signatureHeader string, payload dto.FlutterwaveWebhookPayload) error {
	if s.webhookSecret == "" || subtle.ConstantTimeCompare([]byte(signatureHeader), []byte(s.webhookSecret)) != 1 {
		return ErrInvalidWebhookSignature
	}

	if payload.Event != "charge.completed" || payload.Data.Status != "successful" {
		return nil
	}

	tx, err := s.payments.FindByTxRef(ctx, payload.Data.TxRef)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrPaymentNotFound
		}
		return fmt.Errorf("service: failed to load payment transaction: %w", err)
	}

	if tx.IsTerminal() {
		// Already processed (Flutterwave retries webhooks) — idempotent
		// no-op, never grant subscription days twice for one payment.
		return nil
	}

	// Never trust the webhook body alone: re-fetch the transaction
	// directly from Flutterwave using the secret key and cross-check it
	// against what we actually asked for.
	verified, err := s.gateway.VerifyTransaction(ctx, fmt.Sprintf("%d", payload.Data.ID))
	if err != nil {
		return fmt.Errorf("service: failed to verify transaction with flutterwave: %w", err)
	}

	if verified.Status != "successful" ||
		verified.TxRef != tx.TxRef ||
		verified.Currency != tx.Currency ||
		verified.AmountKobo < tx.AmountKobo {
		tx.Status = models.PaymentFailed
		tx.FailureReason = "server-side verification did not match the expected transaction"
		_ = s.payments.Update(ctx, tx)
		return ErrPaymentVerificationFailed
	}

	now := time.Now()
	tx.Status = models.PaymentSuccessful
	tx.FlwTransactionID = verified.FlwTransactionID
	tx.CompletedAt = &now
	if err := s.payments.Update(ctx, tx); err != nil {
		return fmt.Errorf("service: failed to record successful payment: %w", err)
	}

	if _, err := s.admin.ActivateSubscription(ctx, tx.DriverID, tx.DaysRequested); err != nil {
		return fmt.Errorf("service: payment recorded but failed to activate subscription: %w", err)
	}

	return nil
}

// formatNaira converts an integer kobo amount to the "%.2f" Naira string
// Flutterwave's API expects (e.g. 150000 kobo -> "1500.00").
func formatNaira(kobo int64) string {
	return fmt.Sprintf("%d.%02d", kobo/100, kobo%100)
}
