package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

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
	wallet   *service.WalletService
}

// SetWallet routes wallet top-up and payout events to the wallet service.
func (h *PaymentHandler) SetWallet(w *service.WalletService) { h.wallet = w }

// NewPaymentHandler constructs a PaymentHandler.
func NewPaymentHandler(payments *service.PaymentService) *PaymentHandler {
	return &PaymentHandler{payments: payments}
}

// ListPlans godoc
//
//	@Summary		Get the available payment plans for my vehicle type
//	@Description	Returns every billing cycle (daily/weekly/monthly/yearly) an admin currently has enabled for the calling driver's own vehicle type, with its price. A vehicle type with only one cycle turned on returns exactly one plan.
//	@Tags			Payments
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	utils.APIResponse{data=dto.DriverSubscriptionPlansResponse}
//	@Failure		404	{object}	utils.APIResponse	"Not registered as a driver yet"
//	@Router			/driver/subscription/plans [get]
func (h *PaymentHandler) ListPlans(c *gin.Context) {
	resp, err := h.payments.ListPlans(c.Request.Context(), middleware.UserIDFromContext(c))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "subscription plans loaded", resp)
}

// InitiateCheckout godoc
//
//	@Summary		Start a driver subscription payment
//	@Description	Creates a Flutterwave hosted-checkout link for one billing cycle of platform access — the cycle must be one GET /driver/subscription/plans currently lists as enabled for this driver's vehicle type. Nothing is granted yet — access activates only once the payment webhook confirms the charge; poll GET /driver/profile afterward to see `has_active_subscription` flip to true.
//	@Tags			Payments
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		dto.InitiateSubscriptionRequest	true	"Billing cycle to pay for"
//	@Success		201		{object}	utils.APIResponse{data=dto.InitiateSubscriptionResponse}
//	@Failure		404		{object}	utils.APIResponse	"Not registered as a driver yet"
//	@Router			/driver/subscription/checkout [post]
func (h *PaymentHandler) InitiateCheckout(c *gin.Context) {
	var req dto.InitiateSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, http.StatusBadRequest, "a valid billing_cycle (daily, weekly, monthly, or yearly) is required, and redirect_url (if given) must be a valid URL")
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
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		utils.Success(c, http.StatusOK, "ignored: unreadable body", nil)
		return
	}

	// Authenticate before looking at anything else.
	signature := c.GetHeader("verif-hash")
	if err := h.payments.VerifySignature(signature); err != nil {
		handleServiceError(c, err)
		return
	}

	var payload dto.FlutterwaveWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		// Malformed body from an authenticated sender: acknowledge so it
		// isn't retried forever, but do nothing with it.
		utils.Success(c, http.StatusOK, "ignored: unparseable payload", nil)
		return
	}

	switch payload.Event {
	case "transfer.completed":
		var t struct {
			Data struct {
				ID        int64  `json:"id"`
				Reference string `json:"reference"`
			} `json:"data"`
		}
		_ = json.Unmarshal(body, &t)
		if h.wallet != nil && t.Data.Reference != "" {
			id := ""
			if t.Data.ID != 0 {
				id = strconv.FormatInt(t.Data.ID, 10)
			}
			if err := h.wallet.HandleTransferWebhook(c.Request.Context(), t.Data.Reference, id); err != nil {
				handleServiceError(c, err)
				return
			}
		}
	case "charge.completed":
		if h.wallet != nil && strings.HasPrefix(payload.Data.TxRef, "topup_") {
			if payload.Data.Status == "successful" {
				err := h.wallet.HandleChargeWebhook(c.Request.Context(), payload.Data.TxRef, strconv.FormatInt(payload.Data.ID, 10))
				if err != nil && !errors.Is(err, service.ErrTopupNotFound) {
					handleServiceError(c, err)
					return
				}
			}
			break
		}
		// Driver subscription payment (original flow).
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		if err := h.payments.HandleWebhook(c.Request.Context(), signature, payload); err != nil {
			handleServiceError(c, err)
			return
		}
	}

	utils.Success(c, http.StatusOK, "webhook processed", nil)
}
