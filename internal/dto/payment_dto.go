package dto

// InitiateSubscriptionRequest is the payload for
// POST /driver/subscription/checkout.
type InitiateSubscriptionRequest struct {
	// Days is how many days of platform access to pay for. Defaults to 1
	// if omitted; capped server-side at 30.
	Days int `json:"days" binding:"omitempty,min=1,max=30" example:"1"`
	// RedirectURL is where Flutterwave sends the driver's browser/app
	// after payment. Optional — falls back to FLW_REDIRECT_URL. Pass your
	// app's deep link here once the mobile app exists (e.g.
	// "ridematch://payment-callback").
	RedirectURL string `json:"redirect_url" binding:"omitempty,url"`
}

// DriverSubscriptionPriceResponse is the daily platform-access price for
// the calling driver's own vehicle type — read-only, admin-configured
// (see AdminUpdateSubscriptionPriceRequest). The app multiplies
// PriceKoboPerDay by however many days the driver picks to show the total
// before they ever hit checkout.
type DriverSubscriptionPriceResponse struct {
	VehicleType     string `json:"vehicle_type"`
	PriceKoboPerDay int64  `json:"price_kobo_per_day"`
}

// InitiateSubscriptionResponse is returned after a checkout link is
// created — hand `payment_link` to the driver (open it in a browser/
// WebView) to complete payment.
type InitiateSubscriptionResponse struct {
	PaymentLink string `json:"payment_link"`
	TxRef       string `json:"tx_ref"`
	AmountKobo  int64  `json:"amount_kobo"`
	Days        int    `json:"days"`
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
