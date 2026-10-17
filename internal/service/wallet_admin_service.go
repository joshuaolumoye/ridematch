package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"ridematch-backend/internal/dto"
	"ridematch-backend/internal/models"
	"ridematch-backend/internal/money"
	"ridematch-backend/internal/repository"
)

// ---- admin: withdrawals ----

func (s *WalletService) adminWithdrawal(ctx context.Context, w *models.Withdrawal, cache map[string]*models.User) dto.AdminWithdrawalResponse {
	u, ok := cache[w.UserID]
	if !ok {
		u, _ = s.users.FindByID(ctx, w.UserID)
		cache[w.UserID] = u
	}
	r := dto.AdminWithdrawalResponse{
		ID: w.ID, UserID: w.UserID, Kind: string(w.Kind), Status: string(w.Status), AmountKobo: w.AmountKobo,
		FeeKobo: w.FeeKobo, NetKobo: w.NetKobo, BankName: w.BankName, AccountNumber: w.AccountNumber,
		AccountName: w.AccountName, Reference: w.Reference, FlwTransferID: w.FlwTransferID, FailureReason: w.FailureReason,
		RiskFlags: w.RiskFlags, AppealID: w.AppealID, ReviewNote: w.ReviewNote, ReviewedBy: w.ReviewedBy,
		AttemptCount: w.AttemptCount, CreatedAt: w.CreatedAt, CompletedAt: w.CompletedAt,
	}
	if u != nil {
		r.DriverName, r.DriverPhone = u.Name, u.Phone
	}
	return r
}

// AdminListWithdrawals lists payouts, optionally by status.
func (s *WalletService) AdminListWithdrawals(ctx context.Context, status string, page, pageSize int) ([]dto.AdminWithdrawalResponse, int64, error) {
	page, pageSize = normalizePage(page, pageSize)
	rows, total, err := s.ledger.ListWithdrawals(ctx, status, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("service: failed to list withdrawals: %w", err)
	}
	cache := map[string]*models.User{}
	out := make([]dto.AdminWithdrawalResponse, len(rows))
	for i := range rows {
		out[i] = s.adminWithdrawal(ctx, &rows[i], cache)
	}
	return out, total, nil
}

// AdminDecideWithdrawal handles a pending_review withdrawal ("approve" |
// "reject") or resolves a needs_attention one ("mark_paid" | "mark_failed").
// Resolving a stuck payout by hand is the highest-risk admin action in the
// system, so it always requires a note and is audited.
func (s *WalletService) AdminDecideWithdrawal(ctx context.Context, actor Actor, id string, req dto.AdminDecisionRequest) (*dto.AdminWithdrawalResponse, error) {
	wd, err := s.ledger.FindWithdrawalByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrWithdrawalNotFound
		}
		return nil, fmt.Errorf("service: failed to load withdrawal: %w", err)
	}
	note := strings.TrimSpace(req.Note)
	meta := map[string]interface{}{"decision": req.Decision, "note": note, "amount_kobo": wd.AmountKobo, "from": wd.Status}

	switch req.Decision {
	case "approve":
		if wd.Status != models.WithdrawalPendingReview {
			return nil, ErrWithdrawalState
		}
		// A person who requested a payout cannot be the admin approving it.
		if wd.UserID == actor.UserID {
			return nil, ErrSameApprover
		}
		ok, err := s.ledger.TransitionWithdrawal(ctx, id, []models.WithdrawalStatus{models.WithdrawalPendingReview}, models.WithdrawalQueued,
			map[string]interface{}{"reviewed_by": actor.UserID, "review_note": note})
		if err != nil {
			return nil, fmt.Errorf("service: failed to approve withdrawal: %w", err)
		}
		if !ok {
			return nil, ErrWithdrawalState
		}
		go s.dispatchDetached(id)
	case "reject":
		if wd.Status != models.WithdrawalPendingReview {
			return nil, ErrWithdrawalState
		}
		s.failWithdrawalReviewed(ctx, wd, models.WithdrawalRejected, "rejected by review: "+note, actor.UserID, note)
	case "mark_paid":
		if wd.Status != models.WithdrawalNeedsAttention {
			return nil, ErrWithdrawalState
		}
		s.succeedWithdrawal(ctx, wd, wd.FlwTransferID)
	case "mark_failed":
		if wd.Status != models.WithdrawalNeedsAttention {
			return nil, ErrWithdrawalState
		}
		s.failWithdrawalReviewed(ctx, wd, models.WithdrawalFailed, "confirmed not paid: "+note, actor.UserID, note)
	default:
		return nil, fmt.Errorf("%w: unknown decision", ErrWithdrawalState)
	}
	s.audit.Record(ctx, actor, "withdrawal."+req.Decision, "withdrawal", id, meta)
	updated, err := s.ledger.FindWithdrawalByID(ctx, id)
	if err != nil {
		return nil, err
	}
	resp := s.adminWithdrawal(ctx, updated, map[string]*models.User{})
	return &resp, nil
}

func (s *WalletService) failWithdrawalReviewed(ctx context.Context, wd *models.Withdrawal, final models.WithdrawalStatus, reason, reviewer, note string) {
	_, _ = s.ledger.TransitionWithdrawal(ctx, wd.ID,
		[]models.WithdrawalStatus{models.WithdrawalPendingReview, models.WithdrawalNeedsAttention, models.WithdrawalProcessing, models.WithdrawalQueued},
		wd.Status, map[string]interface{}{"review_note": note})
	s.failWithdrawal(ctx, wd, final, reason, reviewer)
}

// ---- admin: payout accounts ----

// AdminListPayoutAccounts lists bank accounts by review status.
func (s *WalletService) AdminListPayoutAccounts(ctx context.Context, status string, page, pageSize int) ([]dto.AdminPayoutAccountResponse, int64, error) {
	page, pageSize = normalizePage(page, pageSize)
	rows, total, err := s.accounts.ListByStatus(ctx, status, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("service: failed to list payout accounts: %w", err)
	}
	out := make([]dto.AdminPayoutAccountResponse, len(rows))
	for i, a := range rows {
		r := dto.AdminPayoutAccountResponse{ID: a.ID, UserID: a.UserID, BankName: a.BankName, AccountNumber: a.AccountNumber,
			AccountName: a.AccountName, Status: string(a.Status), NameMatchNote: a.NameMatchNote, CreatedAt: a.CreatedAt}
		if u, err := s.users.FindByID(ctx, a.UserID); err == nil {
			r.DriverName, r.RegisteredName = u.Name, u.Name
		}
		out[i] = r
	}
	return out, total, nil
}

// AdminDecidePayoutAccount approves or rejects an account whose holder name
// did not match the driver's registered name.
func (s *WalletService) AdminDecidePayoutAccount(ctx context.Context, actor Actor, id string, req dto.AdminDecisionRequest) error {
	a, err := s.accounts.FindByID(ctx, id)
	if err != nil {
		return ErrPayoutAccountNotFound
	}
	if a.Status != models.PayoutAccountPendingReview {
		return ErrWithdrawalState
	}
	switch req.Decision {
	case "approve":
		a.Status = models.PayoutAccountActive
	case "reject":
		a.Status = models.PayoutAccountRejected
	default:
		return fmt.Errorf("%w: unknown decision", ErrWithdrawalState)
	}
	now := nowFn()
	a.ReviewedBy, a.ReviewedAt = actor.UserID, &now
	if err := s.accounts.Update(ctx, a); err != nil {
		return fmt.Errorf("service: failed to update payout account: %w", err)
	}
	s.audit.Record(ctx, actor, "payout_account."+req.Decision, "payout_account", id, map[string]interface{}{"note": req.Note})
	return nil
}

// ---- admin: switches, freezes, reports ----

// SetPayoutsEnabled flips the global payout kill-switch.
func (s *WalletService) SetPayoutsEnabled(ctx context.Context, actor Actor, enabled bool, reason string) error {
	v := "true"
	if !enabled {
		v = "false"
	}
	if err := s.settings.Set(ctx, settingPayoutsEnabled, v, actor.UserID); err != nil {
		return fmt.Errorf("service: failed to save payout switch: %w", err)
	}
	s.audit.Record(ctx, actor, "payouts.switch", "platform", "payouts", map[string]interface{}{"enabled": enabled, "reason": reason})
	return nil
}

// PayoutsEnabled reports the effective global state.
func (s *WalletService) PayoutsEnabled(ctx context.Context) bool { return s.payoutsEnabled(ctx) }

// SetWalletFrozen freezes/unfreezes one user's withdrawals.
func (s *WalletService) SetWalletFrozen(ctx context.Context, actor Actor, userID string, frozen bool, reason string) error {
	if _, err := s.users.FindByID(ctx, userID); err != nil {
		return ErrUserNotFound
	}
	if err := s.ledger.SetWalletFrozen(ctx, userID, frozen, reason); err != nil {
		return fmt.Errorf("service: failed to update wallet: %w", err)
	}
	s.audit.Record(ctx, actor, "wallet.freeze", "user", userID, map[string]interface{}{"frozen": frozen, "reason": reason})
	return nil
}

// AdminWallet returns one user's wallet and recent history.
func (s *WalletService) AdminWallet(ctx context.Context, actor Actor, userID string) (*dto.AdminWalletResponse, error) {
	u, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return nil, ErrUserNotFound
	}
	w, err := s.ledger.EnsureWallet(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("service: failed to load wallet: %w", err)
	}
	base, err := s.walletResponse(ctx, userID, w)
	if err != nil {
		return nil, err
	}
	s.audit.Record(ctx, actor, "wallet.view", "user", userID, nil)
	return &dto.AdminWalletResponse{WalletResponse: *base, UserID: userID, UserName: u.Name}, nil
}

// AdminStatement returns a user's ledger lines.
func (s *WalletService) AdminStatement(ctx context.Context, userID string, page, pageSize int) ([]dto.StatementEntryResponse, int64, error) {
	return s.Statement(ctx, userID, page, pageSize)
}

// Integrity runs the ledger self-check.
func (s *WalletService) Integrity(ctx context.Context) (*repository.IntegrityReport, error) {
	return s.ledger.Integrity(ctx)
}

// QuoteForTest exposes pure fee maths to tests/tools.
func QuoteTopupGross(net int64, bps int) int64 { return money.GrossUp(net, bps) }
