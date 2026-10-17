package dto

// InitiateSubscriptionRequest is the payload for
// POST /driver/subscription/checkout. BillingCycle must be one the
// driver's own vehicle type currently has enabled (see
// DriverSubscriptionPlansResponse) — any other cycle is rejected even if
// it's a normally-valid value, since admins control which cycles each
// vehicle type actually offers.
type InitiateSubscriptionRequest struct {
	BillingCycle string `json:"billing_cycle" binding:"required,oneof=daily weekly monthly yearly" example:"weekly"`
	// RedirectURL is where Flutterwave sends the driver's browser/app
	// after payment. Optional — falls back to FLW_REDIRECT_URL. Pass your
	// app's deep link here once the mobile app exists (e.g.
	// "ridematch://payment-callback").
	RedirectURL string `json:"redirect_url" binding:"omitempty,url"`
}

// SubscriptionPlanOption is one billing cycle a vehicle type currently
// offers, with its admin-set price.
type SubscriptionPlanOption struct {
	BillingCycle string `json:"billing_cycle"`
	PriceKobo    int64  `json:"price_kobo"`
}

// DriverSubscriptionPlansResponse is every currently-enabled billing
// cycle for the calling driver's own vehicle type — e.g. if an admin has
// only turned on "yearly" for car, a car driver's Plans has exactly one
// entry. The app renders exactly these as the plan picker; nothing is
// hardcoded client-side.
type DriverSubscriptionPlansResponse struct {
	VehicleType string                   `json:"vehicle_type"`
	Plans       []SubscriptionPlanOption `json:"plans"`
}

// InitiateSubscriptionResponse is returned after a checkout link is
// created — hand `payment_link` to the driver (open it in a browser/
// WebView) to complete payment.
type InitiateSubscriptionResponse struct {
	PaymentLink  string `json:"payment_link"`
	TxRef        string `json:"tx_ref"`
	AmountKobo   int64  `json:"amount_kobo"`
	BillingCycle string `json:"billing_cycle"`
	Days         int    `json:"days"`
}

// FlutterwaveWebhookPayload is the subset of Flutterwave's webhook body
// this app reads. Flutterwave sends more fields than this; unknown ones
// are simply ignored by json.Unmarshal.
type FlutterwaveWebhookPayload struct {
	Event string `json:"event"`
	Data  struct {
		ID       int64   `json:"id"`
		TxRef    string  `json:"tx_ref"`
		Status   string  `json:"status"`
		Amount   float64 `json:"amount"`
		Currency string  `json:"currency"`
	} `json:"data"`
}

// StepUpOTPResponse is returned when a security code is sent.
type StepUpOTPResponse struct {
	SentTo           string `json:"sent_to"`
	ExpiresInSeconds int    `json:"expires_in_seconds"`
	ResendInSeconds  int    `json:"resend_in_seconds"`
}
