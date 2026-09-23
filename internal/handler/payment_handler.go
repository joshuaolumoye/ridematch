package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"ridematch-backend/internal/dto"
	"ridematch-backend/internal/middleware"
	"ridematch-backend/internal/service"
	"ridematch-backend/internal/utils"
)

// PaymentHandler exposes the driver subscription checkout flow and the
// Flutterwave webhook.
type PaymentHandler struct {
	payments *service.PaymentService
}

// NewPaymentHandler constructs a PaymentHandler.
func NewPaymentHandler(payments *service.PaymentService) *PaymentHandler {
	return &PaymentHandler{payments: payments}
}

// InitiateCheckout godoc
//
//	@Summary		Start a driver subscription payment
//	@Description	Creates a Flutterwave hosted-checkout link for N days of platform access (default 1, max 30). Nothing is granted yet — access activates only once the payment webhook confirms the charge; poll GET /driver/profile afterward to see `has_active_subscription` flip to true.
//	@Tags			Payments
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		dto.InitiateSubscriptionRequest	false	"Days to pay for (default 1)"
//	@Success		201		{object}	utils.APIResponse{data=dto.InitiateSubscriptionResponse}
//	@Failure		404		{object}	utils.APIResponse	"Not registered as a driver yet"
//	@Router			/driver/subscription/checkout [post]
func (h *PaymentHandler) InitiateCheckout(c *gin.Context) {
	var req dto.InitiateSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, http.StatusBadRequest, "days must be between 1 and 30, and redirect_url (if given) must be a valid URL")
		return
	}

	resp, err := h.payments.InitiateSubscriptionCheckout(c.Request.Context(), middleware.UserIDFromContext(c), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusCreated, "checkout link created", resp)
}

// FlutterwaveWebhook godoc
//
//	@Summary		Flutterwave payment webhook
//	@Description	Called by Flutterwave when a charge completes. Verified two ways before anything is credited: the `verif-hash` header must match FLW_WEBHOOK_SECRET_HASH, and the transaction is independently re-fetched from Flutterwave's API before the driver's subscription is activated. Configure this URL as your webhook endpoint in the Flutterwave dashboard. Not for use by API clients directly.
//	@Tags			Payments
//	@Accept			json
//	@Produce		json
//	@Param			verif-hash	header	string							true	"Shared secret configured in the Flutterwave dashboard"
//	@Param			request		body	dto.FlutterwaveWebhookPayload	true	"Flutterwave webhook event"
//	@Success		200	{object}	utils.APIResponse
//	@Failure		401	{object}	utils.APIResponse	"Invalid or missing verif-hash"
//	@Router			/webhooks/flutterwave [post]
func (h *PaymentHandler) FlutterwaveWebhook(c *gin.Context) {
	var payload dto.FlutterwaveWebhookPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		// Malformed body from a party that (by definition, since we
		// haven't checked the signature yet) might not even be
		// Flutterwave — acknowledge with 200 so nothing keeps retrying a
		// request that will never parse, but do nothing with it.
		utils.Success(c, http.StatusOK, "ignored: unparseable payload", nil)
		return
	}

	signature := c.GetHeader("verif-hash")
	if err := h.payments.HandleWebhook(c.Request.Context(), signature, payload); err != nil {
		handleServiceError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "webhook processed", nil)
}
