package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"ridematch-backend/internal/dto"
	"ridematch-backend/internal/middleware"
	"ridematch-backend/internal/service"
	"ridematch-backend/internal/utils"
)

// WalletHandler exposes the caller's wallet, top-ups, payout accounts, PIN
// and withdrawals, plus trip payment confirmation/dispute.
type WalletHandler struct {
	wallet *service.WalletService
	escrow *service.EscrowService
}

// NewWalletHandler constructs a WalletHandler.
func NewWalletHandler(w *service.WalletService, e *service.EscrowService) *WalletHandler {
	return &WalletHandler{wallet: w, escrow: e}
}

func uid(c *gin.Context) string { return middleware.UserIDFromContext(c) }

func (h *WalletHandler) Get(c *gin.Context) {
	resp, err := h.wallet.Wallet(c.Request.Context(), uid(c))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "wallet loaded", resp)
}

func (h *WalletHandler) Transactions(c *gin.Context) {
	page, size := pageParams(c)
	items, total, err := h.wallet.Statement(c.Request.Context(), uid(c), page, size)
	writePage(c, "transactions loaded", items, total, page, size, err)
}

func (h *WalletHandler) TopupQuote(c *gin.Context) {
	amt, _ := strconv.ParseInt(c.Query("amount_kobo"), 10, 64)
	resp, err := h.wallet.QuoteTopup(amt)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "quote ready", resp)
}

func (h *WalletHandler) CreateTopup(c *gin.Context) {
	var req dto.CreateTopupRequest
	if !bind(c, &req, "amount_kobo is required") {
		return
	}
	resp, err := h.wallet.CreateTopup(c.Request.Context(), uid(c), req.AmountKobo)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusCreated, "transfer to the account below to fund your wallet", resp)
}

func (h *WalletHandler) GetTopup(c *gin.Context) {
	resp, err := h.wallet.GetTopup(c.Request.Context(), uid(c), c.Param("id"))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "top-up loaded", resp)
}

func (h *WalletHandler) ListTopups(c *gin.Context) {
	page, size := pageParams(c)
	items, total, err := h.wallet.ListTopups(c.Request.Context(), uid(c), page, size)
	writePage(c, "top-ups loaded", items, total, page, size, err)
}

func (h *WalletHandler) Banks(c *gin.Context) {
	resp, err := h.wallet.Banks(c.Request.Context())
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "banks loaded", resp)
}

func (h *WalletHandler) PINCode(c *gin.Context) {
	resp, err := h.wallet.RequestPINCode(c.Request.Context(), uid(c))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "security code sent", resp)
}

func (h *WalletHandler) SetPIN(c *gin.Context) {
	var req dto.SetPINRequest
	if !bind(c, &req, "pin and otp are required") {
		return
	}
	if err := h.wallet.SetPIN(c.Request.Context(), uid(c), req.PIN, req.OTP); err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "withdrawal PIN saved", nil)
}

func (h *WalletHandler) ResolveAccount(c *gin.Context) {
	var req dto.ResolveAccountRequest
	if !bind(c, &req, "a bank and a 10-digit account number are required") {
		return
	}
	resp, err := h.wallet.ResolveAccount(c.Request.Context(), uid(c), req.BankCode, req.AccountNumber)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "account found", resp)
}

func (h *WalletHandler) PayoutAccountCode(c *gin.Context) {
	resp, err := h.wallet.RequestPayoutAccountCode(c.Request.Context(), uid(c))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "security code sent", resp)
}

func (h *WalletHandler) AddPayoutAccount(c *gin.Context) {
	var req dto.AddPayoutAccountRequest
	if !bind(c, &req, "bank, a 10-digit account number and the security code are required") {
		return
	}
	resp, err := h.wallet.AddPayoutAccount(c.Request.Context(), uid(c), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusCreated, "bank account saved", resp)
}

func (h *WalletHandler) ListPayoutAccounts(c *gin.Context) {
	resp, err := h.wallet.ListPayoutAccounts(c.Request.Context(), uid(c))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "bank accounts loaded", resp)
}

func (h *WalletHandler) RemovePayoutAccount(c *gin.Context) {
	var req dto.RemovePayoutAccountRequest
	if !bind(c, &req, "your PIN is required") {
		return
	}
	if err := h.wallet.RemovePayoutAccount(c.Request.Context(), uid(c), c.Param("id"), req.PIN); err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "bank account removed", nil)
}

func (h *WalletHandler) WithdrawQuote(c *gin.Context) {
	amt, _ := strconv.ParseInt(c.Query("amount_kobo"), 10, 64)
	resp, err := h.wallet.QuoteWithdrawal(amt)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "quote ready", resp)
}

func (h *WalletHandler) Withdraw(c *gin.Context) {
	var req dto.WithdrawRequest
	if !bind(c, &req, "a bank account, an amount and your PIN are required") {
		return
	}
	resp, err := h.wallet.Withdraw(c.Request.Context(), uid(c), req, c.ClientIP())
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusCreated, "withdrawal requested", resp)
}

func (h *WalletHandler) ListWithdrawals(c *gin.Context) {
	page, size := pageParams(c)
	items, total, err := h.wallet.ListWithdrawals(c.Request.Context(), uid(c), page, size)
	writePage(c, "withdrawals loaded", items, total, page, size, err)
}

// ---- trip payment ----

func (h *WalletHandler) TripPayment(c *gin.Context) {
	resp, err := h.escrow.ForTrip(c.Request.Context(), uid(c), c.Param("id"))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "payment loaded", resp)
}

func (h *WalletHandler) ConfirmTripPayment(c *gin.Context) {
	resp, err := h.escrow.ConfirmTrip(c.Request.Context(), uid(c), c.Param("id"))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "payment released to your driver", resp)
}

func (h *WalletHandler) DisputeTripPayment(c *gin.Context) {
	var req dto.DisputeTripRequest
	if !bind(c, &req, "please describe the problem (5–500 characters)") {
		return
	}
	resp, err := h.escrow.DisputeTrip(c.Request.Context(), uid(c), c.Param("id"), req.Reason)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "we've paused the payment and will review it", resp)
}
