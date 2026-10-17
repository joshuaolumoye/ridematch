package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"ridematch-backend/internal/dto"
	"ridematch-backend/internal/flutterwave"
	"ridematch-backend/internal/models"
	"ridematch-backend/internal/repository"
)

// --- fakes -----------------------------------------------------------

type fakeDriverRepo struct {
	byID     map[string]*models.DriverProfile
	byUserID map[string]*models.DriverProfile
}

func newFakeDriverRepo(profiles ...*models.DriverProfile) *fakeDriverRepo {
	r := &fakeDriverRepo{byID: map[string]*models.DriverProfile{}, byUserID: map[string]*models.DriverProfile{}}
	for _, p := range profiles {
		r.byID[p.ID] = p
		r.byUserID[p.UserID] = p
	}
	return r
}

func (r *fakeDriverRepo) Create(_ context.Context, p *models.DriverProfile) error {
	r.byID[p.ID] = p
	r.byUserID[p.UserID] = p
	return nil
}
func (r *fakeDriverRepo) FindByUserID(_ context.Context, userID string) (*models.DriverProfile, error) {
	p, ok := r.byUserID[userID]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return p, nil
}
func (r *fakeDriverRepo) FindByPlateNumber(_ context.Context, plate string) (*models.DriverProfile, error) {
	for _, p := range r.byID {
		if p.PlateNumber == plate {
			return p, nil
		}
	}
	return nil, repository.ErrNotFound
}
func (r *fakeDriverRepo) FindByID(_ context.Context, id string) (*models.DriverProfile, error) {
	p, ok := r.byID[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return p, nil
}
func (r *fakeDriverRepo) Update(_ context.Context, p *models.DriverProfile) error {
	r.byID[p.ID] = p
	r.byUserID[p.UserID] = p
	return nil
}
func (r *fakeDriverRepo) FindAll(_ context.Context, _ repository.DriverFilter, _, _ int) ([]models.DriverProfile, int64, error) {
	return nil, 0, nil
}
func (r *fakeDriverRepo) CountByVehicleType(_ context.Context) (map[string]int64, error) {
	return map[string]int64{}, nil
}
func (r *fakeDriverRepo) CountByVerificationStatus(_ context.Context) (map[string]int64, error) {
	return map[string]int64{}, nil
}
func (r *fakeDriverRepo) CountOnline(_ context.Context) (int64, error) {
	return 0, nil
}

type fakePaymentRepo struct {
	byTxRef map[string]*models.PaymentTransaction
}

func newFakePaymentRepo() *fakePaymentRepo {
	return &fakePaymentRepo{byTxRef: map[string]*models.PaymentTransaction{}}
}
func (r *fakePaymentRepo) Create(_ context.Context, tx *models.PaymentTransaction) error {
	if tx.ID == "" {
		tx.ID = "tx-" + tx.TxRef
	}
	r.byTxRef[tx.TxRef] = tx
	return nil
}
func (r *fakePaymentRepo) FindByTxRef(_ context.Context, txRef string) (*models.PaymentTransaction, error) {
	tx, ok := r.byTxRef[txRef]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return tx, nil
}
func (r *fakePaymentRepo) Update(_ context.Context, tx *models.PaymentTransaction) error {
	r.byTxRef[tx.TxRef] = tx
	return nil
}

// fakeGateway lets each test control exactly what "Flutterwave" says when
// the transaction is independently re-verified.
type fakeGateway struct {
	initiateLink string
	initiateErr  error
	verifyResult *flutterwave.VerifyResult
	verifyErr    error
}

func (g *fakeGateway) InitiatePayment(_ context.Context, _ flutterwave.InitiatePaymentRequest) (string, error) {
	return g.initiateLink, g.initiateErr
}
func (g *fakeGateway) VerifyTransaction(_ context.Context, _ string) (*flutterwave.VerifyResult, error) {
	return g.verifyResult, g.verifyErr
}

// fakeSubscriptionPlanRepo is an in-memory stand-in for
// SubscriptionPlanRepository — only what PaymentService's tests need
// (look up one vehicle type's plans by cycle).
type fakeSubscriptionPlanRepo struct {
	plans map[string]*models.SubscriptionPlan
}

func newFakeSubscriptionPlanRepo() *fakeSubscriptionPlanRepo {
	return &fakeSubscriptionPlanRepo{plans: map[string]*models.SubscriptionPlan{}}
}

func planKey(vt models.VehicleType, c models.BillingCycle) string {
	return string(vt) + "|" + string(c)
}

func (r *fakeSubscriptionPlanRepo) set(vt models.VehicleType, c models.BillingCycle, enabled bool, priceKobo int64) {
	r.plans[planKey(vt, c)] = &models.SubscriptionPlan{VehicleType: vt, BillingCycle: c, Enabled: enabled, PriceKobo: priceKobo}
}
func (r *fakeSubscriptionPlanRepo) FindAll(_ context.Context) ([]models.SubscriptionPlan, error) {
	out := make([]models.SubscriptionPlan, 0, len(r.plans))
	for _, p := range r.plans {
		out = append(out, *p)
	}
	return out, nil
}
func (r *fakeSubscriptionPlanRepo) FindByVehicleType(_ context.Context, vt models.VehicleType) ([]models.SubscriptionPlan, error) {
	out := make([]models.SubscriptionPlan, 0)
	for _, p := range r.plans {
		if p.VehicleType == vt {
			out = append(out, *p)
		}
	}
	return out, nil
}
func (r *fakeSubscriptionPlanRepo) FindEnabledByVehicleType(_ context.Context, vt models.VehicleType) ([]models.SubscriptionPlan, error) {
	out := make([]models.SubscriptionPlan, 0)
	for _, p := range r.plans {
		if p.VehicleType == vt && p.Enabled {
			out = append(out, *p)
		}
	}
	return out, nil
}
func (r *fakeSubscriptionPlanRepo) FindByVehicleTypeAndCycle(_ context.Context, vt models.VehicleType, c models.BillingCycle) (*models.SubscriptionPlan, error) {
	p, ok := r.plans[planKey(vt, c)]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return p, nil
}
func (r *fakeSubscriptionPlanRepo) Upsert(_ context.Context, p *models.SubscriptionPlan) error {
	r.plans[planKey(p.VehicleType, p.BillingCycle)] = p
	return nil
}

// fakeVehicleTypeConfigRepo and fakeDocumentTypeRepo are unused by any
// payment-service test but SettingsService needs something satisfying
// both interfaces to construct.
type fakeVehicleTypeConfigRepo struct{}

func (fakeVehicleTypeConfigRepo) FindAll(_ context.Context) ([]models.VehicleTypeConfig, error) {
	return nil, nil
}
func (fakeVehicleTypeConfigRepo) FindByVehicleType(_ context.Context, _ models.VehicleType) (*models.VehicleTypeConfig, error) {
	return nil, repository.ErrNotFound
}
func (fakeVehicleTypeConfigRepo) Upsert(_ context.Context, _ *models.VehicleTypeConfig) error {
	return nil
}

type fakeDocumentTypeRepo struct{}

func (fakeDocumentTypeRepo) FindAll(_ context.Context) ([]models.DocumentType, error) {
	return nil, nil
}
func (fakeDocumentTypeRepo) FindByVehicleType(_ context.Context, _ models.VehicleType) ([]models.DocumentType, error) {
	return nil, nil
}
func (fakeDocumentTypeRepo) FindEnabledByVehicleType(_ context.Context, _ models.VehicleType) ([]models.DocumentType, error) {
	return nil, nil
}
func (fakeDocumentTypeRepo) FindByID(_ context.Context, _ string) (*models.DocumentType, error) {
	return nil, repository.ErrNotFound
}
func (fakeDocumentTypeRepo) Create(_ context.Context, _ *models.DocumentType) error { return nil }
func (fakeDocumentTypeRepo) Update(_ context.Context, _ *models.DocumentType) error { return nil }
func (fakeDocumentTypeRepo) Delete(_ context.Context, _ string) error               { return nil }

type fakeVehicleCatalogRepo struct{}

func (fakeVehicleCatalogRepo) FindMakesByVehicleType(_ context.Context, _ models.VehicleType) ([]models.VehicleMake, error) {
	return nil, nil
}
func (fakeVehicleCatalogRepo) FindMakeByID(_ context.Context, _ string) (*models.VehicleMake, error) {
	return nil, repository.ErrNotFound
}
func (fakeVehicleCatalogRepo) FindMakeByName(_ context.Context, _ models.VehicleType, _ string) (*models.VehicleMake, error) {
	return nil, repository.ErrNotFound
}
func (fakeVehicleCatalogRepo) CreateMake(_ context.Context, _ *models.VehicleMake) error { return nil }
func (fakeVehicleCatalogRepo) UpdateMake(_ context.Context, _ *models.VehicleMake) error { return nil }
func (fakeVehicleCatalogRepo) DeleteMake(_ context.Context, _ string) error              { return nil }
func (fakeVehicleCatalogRepo) FindModelByID(_ context.Context, _ string) (*models.VehicleModel, error) {
	return nil, repository.ErrNotFound
}
func (fakeVehicleCatalogRepo) FindModelByName(_ context.Context, _ string, _ string) (*models.VehicleModel, error) {
	return nil, repository.ErrNotFound
}
func (fakeVehicleCatalogRepo) CreateModel(_ context.Context, _ *models.VehicleModel) error {
	return nil
}
func (fakeVehicleCatalogRepo) UpdateModel(_ context.Context, _ *models.VehicleModel) error {
	return nil
}
func (fakeVehicleCatalogRepo) DeleteModel(_ context.Context, _ string) error { return nil }

func newTestSettingsService(plans *fakeSubscriptionPlanRepo) *SettingsService {
	return NewSettingsService(fakeVehicleTypeConfigRepo{}, plans, fakeDocumentTypeRepo{}, fakeVehicleCatalogRepo{})
}

func newTestDriverProfile() *models.DriverProfile {
	return &models.DriverProfile{
		Base:        models.Base{ID: "driver-1"},
		UserID:      "user-1",
		VehicleType: models.VehicleCar,
		PlateNumber: "TEST123",
		User:        &models.User{Base: models.Base{ID: "user-1"}, Phone: "+2348012345678", Name: "Test Driver"},
	}
}

// --- tests -------------------------------------------------------------

func TestInitiateSubscriptionCheckout_ComputesAmountAndPersistsTransaction(t *testing.T) {
	driver := newTestDriverProfile()
	driverRepo := newFakeDriverRepo(driver)
	paymentRepo := newFakePaymentRepo()
	gw := &fakeGateway{initiateLink: "https://checkout.flutterwave.com/pay/abc123"}
	admin := NewAdminService(driverRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0)
	plans := newFakeSubscriptionPlanRepo()
	plans.set(models.VehicleCar, models.BillingWeekly, true, 350000)
	settings := newTestSettingsService(plans)
	svc := NewPaymentService(paymentRepo, driverRepo, settings, admin, gw, "secret", "https://ridematch.app/callback")

	resp, err := svc.InitiateSubscriptionCheckout(context.Background(), "user-1", dto.InitiateSubscriptionRequest{BillingCycle: "weekly"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.AmountKobo != 350000 {
		t.Errorf("expected amount 350000 kobo (the admin-set weekly price), got %d", resp.AmountKobo)
	}
	if resp.Days != 7 {
		t.Errorf("expected days=7 (one week), got %d", resp.Days)
	}
	if resp.BillingCycle != "weekly" {
		t.Errorf("expected billing_cycle=weekly, got %s", resp.BillingCycle)
	}
	if resp.PaymentLink != "https://checkout.flutterwave.com/pay/abc123" {
		t.Errorf("unexpected payment link: %s", resp.PaymentLink)
	}

	stored, err := paymentRepo.FindByTxRef(context.Background(), resp.TxRef)
	if err != nil {
		t.Fatalf("expected transaction to be persisted: %v", err)
	}
	if stored.Status != models.PaymentInitiated {
		t.Errorf("expected status=initiated, got %s", stored.Status)
	}
	if stored.DriverID != "driver-1" {
		t.Errorf("expected driver-1, got %s", stored.DriverID)
	}
}

func TestInitiateSubscriptionCheckout_DailyPlan(t *testing.T) {
	driver := newTestDriverProfile()
	driverRepo := newFakeDriverRepo(driver)
	paymentRepo := newFakePaymentRepo()
	gw := &fakeGateway{initiateLink: "https://checkout.example/x"}
	admin := NewAdminService(driverRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0)
	plans := newFakeSubscriptionPlanRepo()
	plans.set(models.VehicleCar, models.BillingDaily, true, 100000)
	settings := newTestSettingsService(plans)
	svc := NewPaymentService(paymentRepo, driverRepo, settings, admin, gw, "secret", "https://x")

	resp, err := svc.InitiateSubscriptionCheckout(context.Background(), "user-1", dto.InitiateSubscriptionRequest{BillingCycle: "daily"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Days != 1 || resp.AmountKobo != 100000 {
		t.Errorf("expected 1 day / 100000 kobo for the daily plan, got days=%d amount=%d", resp.Days, resp.AmountKobo)
	}
}

func TestInitiateSubscriptionCheckout_DisabledCycleIsRejected(t *testing.T) {
	driver := newTestDriverProfile()
	driverRepo := newFakeDriverRepo(driver)
	paymentRepo := newFakePaymentRepo()
	gw := &fakeGateway{}
	admin := NewAdminService(driverRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0)
	plans := newFakeSubscriptionPlanRepo()
	// Only yearly is enabled for this vehicle type — daily exists as a row
	// but is turned off, exactly like an admin who's only turned on one
	// cycle.
	plans.set(models.VehicleCar, models.BillingDaily, false, 100000)
	plans.set(models.VehicleCar, models.BillingYearly, true, 2000000)
	settings := newTestSettingsService(plans)
	svc := NewPaymentService(paymentRepo, driverRepo, settings, admin, gw, "secret", "https://x")

	_, err := svc.InitiateSubscriptionCheckout(context.Background(), "user-1", dto.InitiateSubscriptionRequest{BillingCycle: "daily"})
	if !errors.Is(err, ErrSubscriptionPlanUnavailable) {
		t.Fatalf("expected ErrSubscriptionPlanUnavailable for a disabled cycle, got %v", err)
	}
}

func TestInitiateSubscriptionCheckout_UnknownDriverReturnsNotFound(t *testing.T) {
	driverRepo := newFakeDriverRepo()
	paymentRepo := newFakePaymentRepo()
	gw := &fakeGateway{}
	admin := NewAdminService(driverRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0)
	settings := newTestSettingsService(newFakeSubscriptionPlanRepo())
	svc := NewPaymentService(paymentRepo, driverRepo, settings, admin, gw, "secret", "https://x")

	_, err := svc.InitiateSubscriptionCheckout(context.Background(), "someone-not-a-driver", dto.InitiateSubscriptionRequest{BillingCycle: "daily"})
	if !errors.Is(err, ErrDriverProfileNotFound) {
		t.Fatalf("expected ErrDriverProfileNotFound, got %v", err)
	}
}

func webhookPayload(id int64, txRef, status string, amount float64) dto.FlutterwaveWebhookPayload {
	var p dto.FlutterwaveWebhookPayload
	p.Event = "charge.completed"
	p.Data.ID = id
	p.Data.TxRef = txRef
	p.Data.Status = status
	p.Data.Amount = amount
	p.Data.Currency = "NGN"
	return p
}

func TestHandleWebhook_RejectsBadSignature(t *testing.T) {
	driverRepo := newFakeDriverRepo(newTestDriverProfile())
	paymentRepo := newFakePaymentRepo()
	admin := NewAdminService(driverRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0)
	settings := newTestSettingsService(newFakeSubscriptionPlanRepo())
	svc := NewPaymentService(paymentRepo, driverRepo, settings, admin, &fakeGateway{}, "correct-secret", "https://x")

	err := svc.HandleWebhook(context.Background(), "wrong-secret", webhookPayload(1, "sub_x", "successful", 1000))
	if !errors.Is(err, ErrInvalidWebhookSignature) {
		t.Fatalf("expected ErrInvalidWebhookSignature, got %v", err)
	}
}

func TestHandleWebhook_SuccessfulPaymentActivatesSubscription(t *testing.T) {
	driver := newTestDriverProfile()
	driverRepo := newFakeDriverRepo(driver)
	paymentRepo := newFakePaymentRepo()
	admin := NewAdminService(driverRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0)

	// Seed a pending transaction exactly as InitiateSubscriptionCheckout would.
	tx := &models.PaymentTransaction{
		Base: models.Base{ID: "tx-1"}, DriverID: "driver-1", TxRef: "sub_abc",
		AmountKobo: 200000, Currency: "NGN", DaysRequested: 2, Status: models.PaymentInitiated,
	}
	_ = paymentRepo.Create(context.Background(), tx)

	gw := &fakeGateway{verifyResult: &flutterwave.VerifyResult{
		FlwTransactionID: "999", TxRef: "sub_abc", Status: "successful", AmountKobo: 200000, Currency: "NGN",
	}}
	settings := newTestSettingsService(newFakeSubscriptionPlanRepo())
	svc := NewPaymentService(paymentRepo, driverRepo, settings, admin, gw, "secret", "https://x")

	if driver.HasActiveSubscription() {
		t.Fatal("driver should not have an active subscription before the webhook fires")
	}

	err := svc.HandleWebhook(context.Background(), "secret", webhookPayload(999, "sub_abc", "successful", 2000))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stored, _ := paymentRepo.FindByTxRef(context.Background(), "sub_abc")
	if stored.Status != models.PaymentSuccessful {
		t.Errorf("expected transaction to be marked successful, got %s", stored.Status)
	}
	if stored.FlwTransactionID != "999" {
		t.Errorf("expected flw transaction id 999, got %s", stored.FlwTransactionID)
	}

	updatedDriver, _ := driverRepo.FindByUserID(context.Background(), "user-1")
	if !updatedDriver.HasActiveSubscription() {
		t.Error("expected driver subscription to be active after successful webhook")
	}
	if updatedDriver.SubscriptionActiveUntil.Before(time.Now().Add(47 * time.Hour)) {
		t.Error("expected ~2 days (48h) of access to have been granted")
	}
}

func TestHandleWebhook_IsIdempotentOnRetry(t *testing.T) {
	driver := newTestDriverProfile()
	driverRepo := newFakeDriverRepo(driver)
	paymentRepo := newFakePaymentRepo()
	admin := NewAdminService(driverRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0)

	tx := &models.PaymentTransaction{
		Base: models.Base{ID: "tx-1"}, DriverID: "driver-1", TxRef: "sub_abc",
		AmountKobo: 100000, Currency: "NGN", DaysRequested: 1, Status: models.PaymentInitiated,
	}
	_ = paymentRepo.Create(context.Background(), tx)

	callCount := 0
	gw := &countingGateway{fakeGateway: fakeGateway{verifyResult: &flutterwave.VerifyResult{
		FlwTransactionID: "1", TxRef: "sub_abc", Status: "successful", AmountKobo: 100000, Currency: "NGN",
	}}, calls: &callCount}
	settings := newTestSettingsService(newFakeSubscriptionPlanRepo())
	svc := NewPaymentService(paymentRepo, driverRepo, settings, admin, gw, "secret", "https://x")

	payload := webhookPayload(1, "sub_abc", "successful", 1000)

	if err := svc.HandleWebhook(context.Background(), "secret", payload); err != nil {
		t.Fatalf("first webhook call failed: %v", err)
	}
	firstExpiry := *driver.SubscriptionActiveUntil

	// Flutterwave commonly retries webhooks — a second delivery of the
	// SAME event must not grant a second day of access.
	if err := svc.HandleWebhook(context.Background(), "secret", payload); err != nil {
		t.Fatalf("second (retry) webhook call failed: %v", err)
	}

	if !driver.SubscriptionActiveUntil.Equal(firstExpiry) {
		t.Errorf("expected subscription expiry to be unchanged after a retried webhook, got %v then %v", firstExpiry, driver.SubscriptionActiveUntil)
	}
	if callCount != 1 {
		t.Errorf("expected VerifyTransaction to be called only once (idempotent short-circuit on retry), got %d calls", callCount)
	}
}

func TestHandleWebhook_AmountMismatchIsRejectedAndMarkedFailed(t *testing.T) {
	driver := newTestDriverProfile()
	driverRepo := newFakeDriverRepo(driver)
	paymentRepo := newFakePaymentRepo()
	admin := NewAdminService(driverRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0)

	tx := &models.PaymentTransaction{
		Base: models.Base{ID: "tx-1"}, DriverID: "driver-1", TxRef: "sub_abc",
		AmountKobo: 500000, Currency: "NGN", DaysRequested: 5, Status: models.PaymentInitiated,
	}
	_ = paymentRepo.Create(context.Background(), tx)

	// Webhook claims success, but what Flutterwave's own verify endpoint
	// reports (a much smaller amount) doesn't match what we asked for —
	// e.g. a spoofed or tampered webhook call.
	gw := &fakeGateway{verifyResult: &flutterwave.VerifyResult{
		FlwTransactionID: "1", TxRef: "sub_abc", Status: "successful", AmountKobo: 100, Currency: "NGN",
	}}
	settings := newTestSettingsService(newFakeSubscriptionPlanRepo())
	svc := NewPaymentService(paymentRepo, driverRepo, settings, admin, gw, "secret", "https://x")

	err := svc.HandleWebhook(context.Background(), "secret", webhookPayload(1, "sub_abc", "successful", 5000))
	if !errors.Is(err, ErrPaymentVerificationFailed) {
		t.Fatalf("expected ErrPaymentVerificationFailed, got %v", err)
	}
	if driver.HasActiveSubscription() {
		t.Error("subscription must not be activated when server-side verification fails")
	}
	stored, _ := paymentRepo.FindByTxRef(context.Background(), "sub_abc")
	if stored.Status != models.PaymentFailed {
		t.Errorf("expected transaction to be marked failed, got %s", stored.Status)
	}
}

func TestHandleWebhook_IgnoresNonChargeCompletedEvents(t *testing.T) {
	driverRepo := newFakeDriverRepo(newTestDriverProfile())
	paymentRepo := newFakePaymentRepo()
	admin := NewAdminService(driverRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0)
	settings := newTestSettingsService(newFakeSubscriptionPlanRepo())
	svc := NewPaymentService(paymentRepo, driverRepo, settings, admin, &fakeGateway{}, "secret", "https://x")

	payload := dto.FlutterwaveWebhookPayload{}
	payload.Event = "transfer.completed" // not the event this app acts on
	payload.Data.Status = "successful"

	if err := svc.HandleWebhook(context.Background(), "secret", payload); err != nil {
		t.Fatalf("expected a no-op nil error for an unhandled event type, got %v", err)
	}
}

// countingGateway wraps fakeGateway to record how many times
// VerifyTransaction is actually invoked — used to prove the idempotent
// short-circuit on an already-terminal transaction skips the network call
// entirely on retry.
type countingGateway struct {
	fakeGateway
	calls *int
}

func (g *countingGateway) VerifyTransaction(ctx context.Context, id string) (*flutterwave.VerifyResult, error) {
	*g.calls++
	return g.fakeGateway.VerifyTransaction(ctx, id)
}
