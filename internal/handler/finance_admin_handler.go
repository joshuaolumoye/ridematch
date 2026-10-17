package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"ridematch-backend/internal/dto"
	"ridematch-backend/internal/service"
	"ridematch-backend/internal/utils"
)

// FinanceAdminHandler is the admin side of money: payouts, payout accounts,
// escrow disputes, wallets, the kill-switch, ledger integrity and audit log.
type FinanceAdminHandler struct {
	wallet *service.WalletService
	escrow *service.EscrowService
	audit  *service.AuditService
}

// NewFinanceAdminHandler constructs a FinanceAdminHandler.
func NewFinanceAdminHandler(w *service.WalletService, e *service.EscrowService, a *service.AuditService) *FinanceAdminHandler {
	return &FinanceAdminHandler{wallet: w, escrow: e, audit: a}
}

// FinanceOverviewResponse is the finance dashboard header.
type FinanceOverviewResponse struct {
	PayoutsEnabled     bool        `json:"payouts_enabled"`
	Integrity          interface{} `json:"integrity"`
	PendingWithdrawals int64       `json:"pending_withdrawals"`
	NeedsAttention     int64       `json:"needs_attention_withdrawals"`
	DisputedPayments   int64       `json:"disputed_payments"`
	PendingAccounts    int64       `json:"pending_payout_accounts"`
}

func (h *FinanceAdminHandler) Overview(c *gin.Context) {
	ctx := c.Request.Context()
	integrity, err := h.wallet.Integrity(ctx)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	_, pending, _ := h.wallet.AdminListWithdrawals(ctx, "pending_review", 1, 1)
	_, attention, _ := h.wallet.AdminListWithdrawals(ctx, "needs_attention", 1, 1)
	_, disputed, _ := h.escrow.AdminList(ctx, "disputed", 1, 1)
	_, accounts, _ := h.wallet.AdminListPayoutAccounts(ctx, "pending_review", 1, 1)
	utils.Success(c, http.StatusOK, "finance overview loaded", FinanceOverviewResponse{
		PayoutsEnabled: h.wallet.PayoutsEnabled(ctx), Integrity: integrity, PendingWithdrawals: pending,
		NeedsAttention: attention, DisputedPayments: disputed, PendingAccounts: accounts,
	})
}

func (h *FinanceAdminHandler) Withdrawals(c *gin.Context) {
	page, size := pageParams(c)
	items, total, err := h.wallet.AdminListWithdrawals(c.Request.Context(), c.Query("status"), page, size)
	writePage(c, "withdrawals loaded", items, total, page, size, err)
}

func (h *FinanceAdminHandler) DecideWithdrawal(c *gin.Context) {
	var req dto.AdminDecisionRequest
	if !bind(c, &req, "a decision and a note (3–500 characters) are required") {
		return
	}
	resp, err := h.wallet.AdminDecideWithdrawal(c.Request.Context(), actorFrom(c), c.Param("id"), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "withdrawal updated", resp)
}

func (h *FinanceAdminHandler) PayoutAccounts(c *gin.Context) {
	page, size := pageParams(c)
	items, total, err := h.wallet.AdminListPayoutAccounts(c.Request.Context(), c.Query("status"), page, size)
	writePage(c, "payout accounts loaded", items, total, page, size, err)
}

func (h *FinanceAdminHandler) DecidePayoutAccount(c *gin.Context) {
	var req dto.AdminDecisionRequest
	if !bind(c, &req, "a decision and a note (3–500 characters) are required") {
		return
	}
	if err := h.wallet.AdminDecidePayoutAccount(c.Request.Context(), actorFrom(c), c.Param("id"), req); err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "payout account updated", nil)
}

func (h *FinanceAdminHandler) Escrow(c *gin.Context) {
	page, size := pageParams(c)
	items, total, err := h.escrow.AdminList(c.Request.Context(), c.Query("status"), page, size)
	writePage(c, "payments loaded", items, total, page, size, err)
}

func (h *FinanceAdminHandler) ResolveEscrow(c *gin.Context) {
	var req dto.AdminDecisionRequest
	if !bind(c, &req, "a decision (release or refund) and a note are required") {
		return
	}
	if err := h.escrow.AdminResolve(c.Request.Context(), actorFrom(c), c.Param("tripId"), req); err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "payment resolved", nil)
}

func (h *FinanceAdminHandler) Wallet(c *gin.Context) {
	resp, err := h.wallet.AdminWallet(c.Request.Context(), actorFrom(c), c.Param("userId"))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "wallet loaded", resp)
}

func (h *FinanceAdminHandler) WalletTransactions(c *gin.Context) {
	page, size := pageParams(c)
	items, total, err := h.wallet.AdminStatement(c.Request.Context(), c.Param("userId"), page, size)
	writePage(c, "transactions loaded", items, total, page, size, err)
}

func (h *FinanceAdminHandler) FreezeWallet(c *gin.Context) {
	var req dto.FreezeWalletRequest
	if !bind(c, &req, "a reason (3–255 characters) is required") {
		return
	}
	if err := h.wallet.SetWalletFrozen(c.Request.Context(), actorFrom(c), c.Param("userId"), req.Frozen, req.Reason); err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "wallet updated", nil)
}

func (h *FinanceAdminHandler) PayoutSwitch(c *gin.Context) {
	var req dto.PayoutSwitchRequest
	if !bind(c, &req, "enabled and a reason (3–300 characters) are required") {
		return
	}
	if err := h.wallet.SetPayoutsEnabled(c.Request.Context(), actorFrom(c), req.Enabled, req.Reason); err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "payout switch updated", gin.H{"payouts_enabled": h.wallet.PayoutsEnabled(c.Request.Context())})
}

func (h *FinanceAdminHandler) AuditLogs(c *gin.Context) {
	page, size := pageParams(c)
	items, total, err := h.audit.List(c.Request.Context(), c.Query("action"), c.Query("target_type"), c.Query("target_id"), page, size)
	writePage(c, "audit log loaded", items, total, page, size, err)
}
