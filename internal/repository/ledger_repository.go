package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"ridematch-backend/internal/models"
)

// Sentinel errors for money operations.
var (
	// ErrAlreadyProcessed means the operation's idempotency key was used
	// before; the caller should treat it as a successful no-op.
	ErrAlreadyProcessed    = errors.New("repository: operation already processed")
	ErrInsufficientFunds   = errors.New("repository: insufficient wallet balance")
	ErrDailyLimit          = errors.New("repository: daily withdrawal limit exceeded")
	ErrWalletFrozen        = errors.New("repository: wallet withdrawals are frozen")
	ErrInvalidTransition   = errors.New("repository: invalid state transition")
	ErrCooldownCap         = errors.New("repository: amount exceeds the limit during the security cooldown")
	ErrDuplicateWithdrawal = errors.New("repository: duplicate withdrawal")
	ErrHoldConflict        = errors.New("repository: trip already has a payment held for a different driver or amount")
)

// WithdrawalGuard carries the limits enforced atomically, under the wallet
// row lock, when a withdrawal is created.
type WithdrawalGuard struct {
	DailyLimitKobo int64
	CooldownCap    int64 // 0 = no cooldown active
	Now            time.Time
	// DuplicateWindow rejects an identical (amount, account) withdrawal
	// created within this window — a double-tapped button.
	DuplicateWindow time.Duration
}

// StatementRow is one wallet ledger line joined with its transaction.
type StatementRow struct {
	ID            uint64    `json:"id"`
	TransactionID string    `json:"transaction_id"`
	Kind          string    `json:"kind"`
	Description   string    `json:"description"`
	RefType       string    `json:"ref_type"`
	RefID         string    `json:"ref_id"`
	AmountKobo    int64     `json:"amount_kobo"`
	BalanceAfter  *int64    `json:"balance_after_kobo"`
	CreatedAt     time.Time `json:"created_at"`
}

// IntegrityReport is the output of the ledger self-check.
type IntegrityReport struct {
	EntriesSumKobo      int64    `json:"entries_sum_kobo"`           // must be 0
	WalletBalancesKobo  int64    `json:"wallet_balances_kobo"`       // sum(wallets.balance)
	WalletEntriesKobo   int64    `json:"wallet_entries_kobo"`        // sum(entries on wallet accounts); must equal above
	EscrowPoolKobo      int64    `json:"escrow_pool_kobo"`           // sum(entries on escrow)
	EscrowHeldKobo      int64    `json:"escrow_held_kobo"`           // sum(holds in held/disputed); must equal pool
	PayoutPendingKobo   int64    `json:"payout_pending_kobo"`        // sum(entries on payout_pending)
	WithdrawalsInFlight int64    `json:"withdrawals_in_flight_kobo"` // must equal payout pending
	ExternalInKobo      int64    `json:"external_in_kobo"`
	ExternalOutKobo     int64    `json:"external_out_kobo"`
	FeesKobo            int64    `json:"fees_kobo"`
	MismatchedWallets   []string `json:"mismatched_wallets"`
	OK                  bool     `json:"ok"`
	Problems            []string `json:"problems"`
}

// LedgerRepository owns every operation that moves money. Each method is a
// single database transaction that locks the rows it touches, writes
// balanced ledger entries, and is idempotent.
type LedgerRepository interface {
	EnsureWallet(ctx context.Context, userID string) (*models.Wallet, error)
	FindWalletByUserID(ctx context.Context, userID string) (*models.Wallet, error)
	UpdateWalletSecurity(ctx context.Context, w *models.Wallet) error
	// RecordPINFailure atomically counts a wrong PIN and locks the PIN once
	// max is reached. Returns the attempts used and the lock expiry, if any.
	RecordPINFailure(ctx context.Context, walletID string, max int, lockFor time.Duration) (attempts int, lockedUntil *time.Time, err error)
	SetWalletFrozen(ctx context.Context, userID string, frozen bool, reason string) error
	Statement(ctx context.Context, walletID string, limit, offset int) ([]StatementRow, int64, error)

	CreateTopup(ctx context.Context, t *models.WalletTopup) error
	FindTopupByID(ctx context.Context, id string) (*models.WalletTopup, error)
	FindTopupByTxRef(ctx context.Context, txRef string) (*models.WalletTopup, error)
	ListTopupsByUser(ctx context.Context, userID string, limit, offset int) ([]models.WalletTopup, int64, error)
	UpdateTopup(ctx context.Context, t *models.WalletTopup) error
	ExpireStaleTopups(ctx context.Context, before time.Time) (int64, error)
	// CreditTopup credits the wallet for a verified payment. applied=false
	// means it had already been credited.
	CreditTopup(ctx context.Context, topupID string, paidKobo, creditKobo int64, flwTxID string) (topup *models.WalletTopup, applied bool, err error)

	FindEscrowByTrip(ctx context.Context, tripID string) (*models.EscrowHold, error)
	ListEscrow(ctx context.Context, status string, limit, offset int) ([]models.EscrowHold, int64, error)
	// HoldEscrow returns created=false when an equivalent live hold already
	// existed, and ErrHoldConflict if one exists for a different payee/amount.
	HoldEscrow(ctx context.Context, h *models.EscrowHold) (hold *models.EscrowHold, created bool, err error)
	MarkEscrowCompleted(ctx context.Context, tripID string, releaseAfter time.Time) error
	DisputeEscrow(ctx context.Context, tripID, reason string) error
	ReleaseEscrow(ctx context.Context, tripID, note string) (applied bool, err error)
	RefundEscrow(ctx context.Context, tripID, resolvedBy, note string) (applied bool, err error)
	DueEscrows(ctx context.Context, now time.Time, limit int) ([]models.EscrowHold, error)

	CreateWithdrawal(ctx context.Context, w *models.Withdrawal, guard WithdrawalGuard) error
	FindWithdrawalByID(ctx context.Context, id string) (*models.Withdrawal, error)
	FindWithdrawalByReference(ctx context.Context, ref string) (*models.Withdrawal, error)
	FindWithdrawalByFlwID(ctx context.Context, flwID string) (*models.Withdrawal, error)
	ListWithdrawalsByUser(ctx context.Context, userID string, limit, offset int) ([]models.Withdrawal, int64, error)
	ListWithdrawals(ctx context.Context, status string, limit, offset int) ([]models.Withdrawal, int64, error)
	WithdrawalsToDispatch(ctx context.Context, olderThan time.Time, limit int) ([]models.Withdrawal, error)
	WithdrawalsInFlightBefore(ctx context.Context, before time.Time, limit int) ([]models.Withdrawal, error)
	// TransitionWithdrawal atomically moves a withdrawal between non-final
	// states, applying extra column updates. Returns false if the current
	// status was not in from.
	TransitionWithdrawal(ctx context.Context, id string, from []models.WithdrawalStatus, to models.WithdrawalStatus, updates map[string]interface{}) (bool, error)
	FinalizeWithdrawalSuccess(ctx context.Context, id, flwTransferID string) (applied bool, err error)
	// FinalizeWithdrawalFailure returns the funds to the wallet. final must
	// be failed or rejected.
	FinalizeWithdrawalFailure(ctx context.Context, id string, final models.WithdrawalStatus, reason, reviewer string) (applied bool, err error)

	// WalletAggregates returns fares held as payer, fares incoming as payee,
	// and withdrawals not yet confirmed — for the wallet overview.
	WalletAggregates(ctx context.Context, userID, walletID string) (heldFor, incoming, pendingWithdrawal int64, err error)

	Integrity(ctx context.Context) (*IntegrityReport, error)
}

type ledgerRepository struct {
	db *gorm.DB
}

// NewLedgerRepository constructs a GORM-backed LedgerRepository.
func NewLedgerRepository(db *gorm.DB) LedgerRepository {
	return &ledgerRepository{db: db}
}

// ---- internals ----

type posting struct {
	account  string
	walletID string // for wallet postings
	amount   int64
}

func walletPosting(walletID string, amount int64) posting {
	return posting{account: models.AccountWallet, walletID: walletID, amount: amount}
}

func poolPosting(account string, amount int64) posting {
	return posting{account: account, amount: amount}
}

func lockWallet(tx *gorm.DB, id string) (*models.Wallet, error) {
	var w models.Wallet
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&w, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &w, err
}

func ensureWalletTx(tx *gorm.DB, userID string) (*models.Wallet, error) {
	var w models.Wallet
	err := tx.First(&w, "user_id = ?", userID).Error
	if err == nil {
		return &w, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	w = models.Wallet{UserID: userID, Currency: "NGN"}
	if err := tx.Create(&w).Error; err != nil {
		if isDuplicateKeyErr(err) {
			if err := tx.First(&w, "user_id = ?", userID).Error; err != nil {
				return nil, err
			}
			return &w, nil
		}
		return nil, err
	}
	return &w, nil
}

// post writes one balanced ledger transaction. Wallet rows referenced by
// postings must already be locked and are passed in `locked`; their
// balances are updated here. Returns ErrAlreadyProcessed when the
// idempotency key exists.
func post(tx *gorm.DB, kind, idemKey, refType, refID, desc string, ps []posting, locked map[string]*models.Wallet) error {
	var sum int64
	for _, p := range ps {
		sum += p.amount
	}
	if sum != 0 {
		return fmt.Errorf("ledger: unbalanced transaction %s (sum=%d)", kind, sum)
	}

	lt := models.LedgerTransaction{
		ID: uuid.NewString(), Kind: kind, IdempotencyKey: idemKey,
		RefType: refType, RefID: refID, Description: desc, CreatedAt: time.Now(),
	}
	if err := tx.Create(&lt).Error; err != nil {
		if isDuplicateKeyErr(err) {
			return ErrAlreadyProcessed
		}
		return err
	}

	now := time.Now()
	for _, p := range ps {
		e := models.LedgerEntry{TransactionID: lt.ID, Account: p.account, AmountKobo: p.amount, CreatedAt: now}
		if p.account == models.AccountWallet {
			w, ok := locked[p.walletID]
			if !ok {
				return fmt.Errorf("ledger: wallet %s not locked", p.walletID)
			}
			w.BalanceKobo += p.amount
			if w.BalanceKobo < 0 {
				return ErrInsufficientFunds
			}
			wid := w.ID
			bal := w.BalanceKobo
			e.WalletID = &wid
			e.BalanceAfter = &bal
			if err := tx.Model(&models.Wallet{}).Where("id = ?", w.ID).Update("balance_kobo", w.BalanceKobo).Error; err != nil {
				return err
			}
		}
		if err := tx.Create(&e).Error; err != nil {
			return err
		}
	}
	return nil
}

func lockOne(tx *gorm.DB, w *models.Wallet) map[string]*models.Wallet {
	return map[string]*models.Wallet{w.ID: w}
}

// ---- wallets ----

func (r *ledgerRepository) EnsureWallet(ctx context.Context, userID string) (*models.Wallet, error) {
	return ensureWalletTx(r.db.WithContext(ctx), userID)
}

func (r *ledgerRepository) FindWalletByUserID(ctx context.Context, userID string) (*models.Wallet, error) {
	var w models.Wallet
	err := r.db.WithContext(ctx).First(&w, "user_id = ?", userID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

// UpdateWalletSecurity persists only the PIN/cooldown columns, never the
// balance, so a stale in-memory copy can't overwrite money.
func (r *ledgerRepository) UpdateWalletSecurity(ctx context.Context, w *models.Wallet) error {
	return r.db.WithContext(ctx).Model(&models.Wallet{}).Where("id = ?", w.ID).
		Select("PINHash", "PINSetAt", "PINFailedAttempts", "PINLockedUntil", "CooldownUntil").
		Updates(w).Error
}

func (r *ledgerRepository) RecordPINFailure(ctx context.Context, walletID string, max int, lockFor time.Duration) (int, *time.Time, error) {
	var attempts int
	var lockedUntil *time.Time
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		w, err := lockWallet(tx, walletID)
		if err != nil {
			return err
		}
		w.PINFailedAttempts++
		attempts = w.PINFailedAttempts
		updates := map[string]interface{}{"pin_failed_attempts": w.PINFailedAttempts}
		if max > 0 && w.PINFailedAttempts >= max {
			t := time.Now().Add(lockFor)
			lockedUntil = &t
			updates["pin_locked_until"] = t
			updates["pin_failed_attempts"] = 0
		}
		return tx.Model(&models.Wallet{}).Where("id = ?", walletID).Updates(updates).Error
	})
	return attempts, lockedUntil, err
}

func (r *ledgerRepository) SetWalletFrozen(ctx context.Context, userID string, frozen bool, reason string) error {
	if _, err := r.EnsureWallet(ctx, userID); err != nil {
		return err
	}
	return r.db.WithContext(ctx).Model(&models.Wallet{}).Where("user_id = ?", userID).
		Updates(map[string]interface{}{"withdrawals_frozen": frozen, "frozen_reason": reason}).Error
}

func (r *ledgerRepository) Statement(ctx context.Context, walletID string, limit, offset int) ([]StatementRow, int64, error) {
	var total int64
	if err := r.db.WithContext(ctx).Model(&models.LedgerEntry{}).
		Where("wallet_id = ?", walletID).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []StatementRow
	err := r.db.WithContext(ctx).Table("ledger_entries e").
		Select("e.id, e.transaction_id, t.kind, t.description, t.ref_type, t.ref_id, e.amount_kobo, e.balance_after, e.created_at").
		Joins("JOIN ledger_transactions t ON t.id = e.transaction_id").
		Where("e.wallet_id = ?", walletID).
		Order("e.id DESC").Limit(limit).Offset(offset).Scan(&rows).Error
	return rows, total, err
}

// ---- top-ups ----

func (r *ledgerRepository) CreateTopup(ctx context.Context, t *models.WalletTopup) error {
	return r.db.WithContext(ctx).Create(t).Error
}

func (r *ledgerRepository) FindTopupByID(ctx context.Context, id string) (*models.WalletTopup, error) {
	var t models.WalletTopup
	err := r.db.WithContext(ctx).First(&t, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &t, err
}

func (r *ledgerRepository) FindTopupByTxRef(ctx context.Context, txRef string) (*models.WalletTopup, error) {
	var t models.WalletTopup
	err := r.db.WithContext(ctx).First(&t, "tx_ref = ?", txRef).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &t, err
}

func (r *ledgerRepository) ListTopupsByUser(ctx context.Context, userID string, limit, offset int) ([]models.WalletTopup, int64, error) {
	var total int64
	q := r.db.WithContext(ctx).Model(&models.WalletTopup{}).Where("user_id = ?", userID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []models.WalletTopup
	err := q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&rows).Error
	return rows, total, err
}

func (r *ledgerRepository) UpdateTopup(ctx context.Context, t *models.WalletTopup) error {
	return r.db.WithContext(ctx).Save(t).Error
}

func (r *ledgerRepository) ExpireStaleTopups(ctx context.Context, before time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Model(&models.WalletTopup{}).
		Where("status = ? AND expires_at IS NOT NULL AND expires_at < ?", models.TopupPending, before).
		Update("status", models.TopupExpired)
	return res.RowsAffected, res.Error
}

func (r *ledgerRepository) CreditTopup(ctx context.Context, topupID string, paidKobo, creditKobo int64, flwTxID string) (*models.WalletTopup, bool, error) {
	var out models.WalletTopup
	applied := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&out, "id = ?", topupID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if out.Status == models.TopupSuccessful {
			return nil
		}
		w, err := ensureWalletTx(tx, out.UserID)
		if err != nil {
			return err
		}
		w, err = lockWallet(tx, w.ID)
		if err != nil {
			return err
		}
		fee := paidKobo - creditKobo
		err = post(tx, "wallet_topup", "topup:"+out.ID, "topup", out.ID, "Wallet top-up",
			[]posting{
				poolPosting(models.AccountExternalIn, -paidKobo),
				walletPosting(w.ID, creditKobo),
				poolPosting(models.AccountFees, fee),
			}, lockOne(tx, w))
		if errors.Is(err, ErrAlreadyProcessed) {
			return nil
		}
		if err != nil {
			return err
		}
		now := time.Now()
		out.Status = models.TopupSuccessful
		out.PaidKobo = paidKobo
		out.CreditedKobo = creditKobo
		out.FlwTransactionID = flwTxID
		out.CompletedAt = &now
		out.FailureReason = ""
		if err := tx.Save(&out).Error; err != nil {
			return err
		}
		applied = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return &out, applied, nil
}

// ---- escrow ----

func (r *ledgerRepository) FindEscrowByTrip(ctx context.Context, tripID string) (*models.EscrowHold, error) {
	var h models.EscrowHold
	err := r.db.WithContext(ctx).First(&h, "trip_id = ?", tripID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &h, err
}

func (r *ledgerRepository) ListEscrow(ctx context.Context, status string, limit, offset int) ([]models.EscrowHold, int64, error) {
	q := r.db.WithContext(ctx).Model(&models.EscrowHold{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []models.EscrowHold
	err := q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&rows).Error
	return rows, total, err
}

// HoldEscrow moves the fare from the payer's wallet into escrow. Safe to
// call again for the same trip: a live hold is returned unchanged, a
// refunded one is re-opened.
func (r *ledgerRepository) HoldEscrow(ctx context.Context, h *models.EscrowHold) (*models.EscrowHold, bool, error) {
	var out models.EscrowHold
	created := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		wallet, err := ensureWalletTx(tx, h.PayerUserID)
		if err != nil {
			return err
		}
		w, err := lockWallet(tx, wallet.ID)
		if err != nil {
			return err
		}

		existing := models.EscrowHold{}
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&existing, "trip_id = ?", h.TripID).Error
		switch {
		case err == nil:
			if existing.Status != models.EscrowRefunded {
				if existing.PayeeDriverID == h.PayeeDriverID && existing.AmountKobo == h.AmountKobo && existing.PayerUserID == h.PayerUserID {
					out = existing
					return nil
				}
				return ErrHoldConflict
			}
			existing.Version++
			existing.Status = models.EscrowHeld
			existing.PayerUserID = h.PayerUserID
			existing.PayeeUserID = h.PayeeUserID
			existing.PayeeDriverID = h.PayeeDriverID
			existing.AmountKobo = h.AmountKobo
			existing.ReleaseAfter, existing.ReleasedAt, existing.RefundedAt = nil, nil, nil
			existing.DisputeReason, existing.DisputedAt, existing.Resolution, existing.ResolvedBy = "", nil, "", ""
			out = existing
		case errors.Is(err, gorm.ErrRecordNotFound):
			out = *h
			out.Status = models.EscrowHeld
			out.Version = 1
			if err := tx.Create(&out).Error; err != nil {
				return err
			}
		default:
			return err
		}

		err = post(tx, "escrow_hold", fmt.Sprintf("escrow_hold:%s:%d", out.TripID, out.Version), "trip", out.TripID,
			"Fare held in escrow",
			[]posting{walletPosting(w.ID, -out.AmountKobo), poolPosting(models.AccountEscrow, out.AmountKobo)},
			lockOne(tx, w))
		if err != nil {
			return err
		}
		created = true
		return tx.Save(&out).Error
	})
	if err != nil {
		return nil, false, err
	}
	return &out, created, nil
}

func (r *ledgerRepository) MarkEscrowCompleted(ctx context.Context, tripID string, releaseAfter time.Time) error {
	return r.db.WithContext(ctx).Model(&models.EscrowHold{}).
		Where("trip_id = ? AND status = ?", tripID, models.EscrowHeld).
		Update("release_after", releaseAfter).Error
}

func (r *ledgerRepository) DisputeEscrow(ctx context.Context, tripID, reason string) error {
	now := time.Now()
	res := r.db.WithContext(ctx).Model(&models.EscrowHold{}).
		Where("trip_id = ? AND status = ?", tripID, models.EscrowHeld).
		Updates(map[string]interface{}{"status": models.EscrowDisputed, "dispute_reason": reason, "disputed_at": now, "release_after": nil})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrInvalidTransition
	}
	return nil
}

func (r *ledgerRepository) ReleaseEscrow(ctx context.Context, tripID, note string) (bool, error) {
	applied := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var h models.EscrowHold
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&h, "trip_id = ?", tripID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if h.Status != models.EscrowHeld && h.Status != models.EscrowDisputed {
			return nil // already released / refunded
		}
		wallet, err := ensureWalletTx(tx, h.PayeeUserID)
		if err != nil {
			return err
		}
		w, err := lockWallet(tx, wallet.ID)
		if err != nil {
			return err
		}
		err = post(tx, "escrow_release", fmt.Sprintf("escrow_release:%s:%d", h.TripID, h.Version), "trip", h.TripID,
			"Trip payment received",
			[]posting{poolPosting(models.AccountEscrow, -h.AmountKobo), walletPosting(w.ID, h.AmountKobo)},
			lockOne(tx, w))
		if errors.Is(err, ErrAlreadyProcessed) {
			return nil
		}
		if err != nil {
			return err
		}
		now := time.Now()
		h.Status = models.EscrowReleased
		h.ReleasedAt = &now
		if note != "" {
			h.Resolution = note
		}
		applied = true
		return tx.Save(&h).Error
	})
	return applied, err
}

func (r *ledgerRepository) RefundEscrow(ctx context.Context, tripID, resolvedBy, note string) (bool, error) {
	applied := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var h models.EscrowHold
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&h, "trip_id = ?", tripID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if h.Status != models.EscrowHeld && h.Status != models.EscrowDisputed {
			return nil
		}
		wallet, err := ensureWalletTx(tx, h.PayerUserID)
		if err != nil {
			return err
		}
		w, err := lockWallet(tx, wallet.ID)
		if err != nil {
			return err
		}
		err = post(tx, "escrow_refund", fmt.Sprintf("escrow_refund:%s:%d", h.TripID, h.Version), "trip", h.TripID,
			"Trip payment refunded",
			[]posting{poolPosting(models.AccountEscrow, -h.AmountKobo), walletPosting(w.ID, h.AmountKobo)},
			lockOne(tx, w))
		if errors.Is(err, ErrAlreadyProcessed) {
			return nil
		}
		if err != nil {
			return err
		}
		now := time.Now()
		h.Status = models.EscrowRefunded
		h.RefundedAt = &now
		h.ResolvedBy = resolvedBy
		if note != "" {
			h.Resolution = note
		}
		applied = true
		return tx.Save(&h).Error
	})
	return applied, err
}

func (r *ledgerRepository) DueEscrows(ctx context.Context, now time.Time, limit int) ([]models.EscrowHold, error) {
	var rows []models.EscrowHold
	err := r.db.WithContext(ctx).
		Where("status = ? AND release_after IS NOT NULL AND release_after <= ?", models.EscrowHeld, now).
		Order("release_after ASC").Limit(limit).Find(&rows).Error
	return rows, err
}

// ---- withdrawals ----

func (r *ledgerRepository) CreateWithdrawal(ctx context.Context, wd *models.Withdrawal, guard WithdrawalGuard) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		w, err := lockWallet(tx, wd.WalletID)
		if err != nil {
			return err
		}
		if w.WithdrawalsFrozen && wd.Kind != models.WithdrawalKindAppeal {
			return ErrWalletFrozen
		}
		if guard.CooldownCap > 0 && wd.AmountKobo > guard.CooldownCap {
			return ErrCooldownCap
		}
		if guard.DuplicateWindow > 0 {
			var dup int64
			if err := tx.Model(&models.Withdrawal{}).
				Where("wallet_id = ? AND amount_kobo = ? AND account_number = ? AND bank_code = ? AND created_at >= ? AND status NOT IN ?",
					w.ID, wd.AmountKobo, wd.AccountNumber, wd.BankCode, guard.Now.Add(-guard.DuplicateWindow),
					[]models.WithdrawalStatus{models.WithdrawalFailed, models.WithdrawalRejected}).
				Count(&dup).Error; err != nil {
				return err
			}
			if dup > 0 {
				return ErrDuplicateWithdrawal
			}
		}
		if guard.DailyLimitKobo > 0 && wd.Kind == models.WithdrawalKindDriver {
			var used int64
			since := guard.Now.Add(-24 * time.Hour)
			if err := tx.Model(&models.Withdrawal{}).
				Where("wallet_id = ? AND kind = ? AND created_at >= ? AND status NOT IN ?", w.ID, models.WithdrawalKindDriver, since,
					[]models.WithdrawalStatus{models.WithdrawalFailed, models.WithdrawalRejected}).
				Select("COALESCE(SUM(amount_kobo),0)").Scan(&used).Error; err != nil {
				return err
			}
			if used+wd.AmountKobo > guard.DailyLimitKobo {
				return ErrDailyLimit
			}
		}
		if err := tx.Create(wd).Error; err != nil {
			return err
		}
		return post(tx, "withdrawal_lock", "withdraw_lock:"+wd.ID, "withdrawal", wd.ID, "Withdrawal to bank",
			[]posting{walletPosting(w.ID, -wd.AmountKobo), poolPosting(models.AccountPayoutPending, wd.AmountKobo)},
			lockOne(tx, w))
	})
}

func (r *ledgerRepository) findWithdrawal(ctx context.Context, where string, arg interface{}) (*models.Withdrawal, error) {
	var w models.Withdrawal
	err := r.db.WithContext(ctx).First(&w, where, arg).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &w, err
}

func (r *ledgerRepository) FindWithdrawalByID(ctx context.Context, id string) (*models.Withdrawal, error) {
	return r.findWithdrawal(ctx, "id = ?", id)
}
func (r *ledgerRepository) FindWithdrawalByReference(ctx context.Context, ref string) (*models.Withdrawal, error) {
	return r.findWithdrawal(ctx, "reference = ?", ref)
}
func (r *ledgerRepository) FindWithdrawalByFlwID(ctx context.Context, flwID string) (*models.Withdrawal, error) {
	return r.findWithdrawal(ctx, "flw_transfer_id = ?", flwID)
}

func (r *ledgerRepository) ListWithdrawalsByUser(ctx context.Context, userID string, limit, offset int) ([]models.Withdrawal, int64, error) {
	q := r.db.WithContext(ctx).Model(&models.Withdrawal{}).Where("user_id = ?", userID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []models.Withdrawal
	err := q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&rows).Error
	return rows, total, err
}

func (r *ledgerRepository) ListWithdrawals(ctx context.Context, status string, limit, offset int) ([]models.Withdrawal, int64, error) {
	q := r.db.WithContext(ctx).Model(&models.Withdrawal{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []models.Withdrawal
	err := q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&rows).Error
	return rows, total, err
}

func (r *ledgerRepository) WithdrawalsToDispatch(ctx context.Context, olderThan time.Time, limit int) ([]models.Withdrawal, error) {
	var rows []models.Withdrawal
	err := r.db.WithContext(ctx).
		Where("status = ? AND updated_at <= ?", models.WithdrawalQueued, olderThan).
		Order("created_at ASC").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r *ledgerRepository) WithdrawalsInFlightBefore(ctx context.Context, before time.Time, limit int) ([]models.Withdrawal, error) {
	var rows []models.Withdrawal
	err := r.db.WithContext(ctx).
		Where("status = ? AND updated_at <= ?", models.WithdrawalProcessing, before).
		Order("updated_at ASC").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r *ledgerRepository) TransitionWithdrawal(ctx context.Context, id string, from []models.WithdrawalStatus, to models.WithdrawalStatus, updates map[string]interface{}) (bool, error) {
	if updates == nil {
		updates = map[string]interface{}{}
	}
	updates["status"] = to
	if to == models.WithdrawalProcessing {
		updates["attempt_count"] = gorm.Expr("attempt_count + 1")
	}
	res := r.db.WithContext(ctx).Model(&models.Withdrawal{}).
		Where("id = ? AND status IN ?", id, from).Updates(updates)
	return res.RowsAffected > 0, res.Error
}

func (r *ledgerRepository) FinalizeWithdrawalSuccess(ctx context.Context, id, flwTransferID string) (bool, error) {
	applied := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var wd models.Withdrawal
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&wd, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if wd.Status == models.WithdrawalSuccessful {
			return nil
		}
		// A "successful" report for a withdrawal we already returned to the
		// wallet would mean paying out twice. Never auto-resolve that.
		if wd.Status == models.WithdrawalFailed || wd.Status == models.WithdrawalRejected ||
			wd.Status == models.WithdrawalPendingReview {
			return ErrInvalidTransition
		}
		err := post(tx, "withdrawal_paid", "withdraw_paid:"+wd.ID, "withdrawal", wd.ID, "Withdrawal paid",
			[]posting{
				poolPosting(models.AccountPayoutPending, -wd.AmountKobo),
				poolPosting(models.AccountExternalOut, wd.NetKobo),
				poolPosting(models.AccountFees, wd.FeeKobo),
			}, nil)
		if errors.Is(err, ErrAlreadyProcessed) {
			return nil
		}
		if err != nil {
			return err
		}
		now := time.Now()
		wd.Status = models.WithdrawalSuccessful
		wd.CompletedAt = &now
		wd.FailureReason = ""
		if flwTransferID != "" {
			wd.FlwTransferID = flwTransferID
		}
		applied = true
		return tx.Save(&wd).Error
	})
	return applied, err
}

func (r *ledgerRepository) FinalizeWithdrawalFailure(ctx context.Context, id string, final models.WithdrawalStatus, reason, reviewer string) (bool, error) {
	if final != models.WithdrawalFailed && final != models.WithdrawalRejected {
		return false, fmt.Errorf("ledger: invalid final status %s", final)
	}
	applied := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var wd models.Withdrawal
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&wd, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if wd.Status == models.WithdrawalFailed || wd.Status == models.WithdrawalRejected {
			return nil
		}
		if wd.Status == models.WithdrawalSuccessful {
			return ErrInvalidTransition
		}
		w, err := lockWallet(tx, wd.WalletID)
		if err != nil {
			return err
		}
		err = post(tx, "withdrawal_return", "withdraw_return:"+wd.ID, "withdrawal", wd.ID, "Withdrawal returned to wallet",
			[]posting{poolPosting(models.AccountPayoutPending, -wd.AmountKobo), walletPosting(w.ID, wd.AmountKobo)},
			lockOne(tx, w))
		if errors.Is(err, ErrAlreadyProcessed) {
			return nil
		}
		if err != nil {
			return err
		}
		now := time.Now()
		wd.Status = final
		wd.FailureReason = reason
		wd.CompletedAt = &now
		if reviewer != "" {
			wd.ReviewedBy = reviewer
			wd.ReviewedAt = &now
		}
		applied = true
		return tx.Save(&wd).Error
	})
	return applied, err
}

// ---- integrity ----

func (r *ledgerRepository) Integrity(ctx context.Context) (*IntegrityReport, error) {
	db := r.db.WithContext(ctx)
	rep := &IntegrityReport{MismatchedWallets: []string{}, Problems: []string{}}

	type acc struct {
		Account string
		Total   int64
	}
	var accs []acc
	if err := db.Model(&models.LedgerEntry{}).Select("account, COALESCE(SUM(amount_kobo),0) AS total").Group("account").Scan(&accs).Error; err != nil {
		return nil, err
	}
	for _, a := range accs {
		rep.EntriesSumKobo += a.Total
		switch a.Account {
		case models.AccountWallet:
			rep.WalletEntriesKobo = a.Total
		case models.AccountEscrow:
			rep.EscrowPoolKobo = a.Total
		case models.AccountPayoutPending:
			rep.PayoutPendingKobo = a.Total
		case models.AccountExternalIn:
			rep.ExternalInKobo = a.Total
		case models.AccountExternalOut:
			rep.ExternalOutKobo = a.Total
		case models.AccountFees:
			rep.FeesKobo = a.Total
		}
	}
	if err := db.Model(&models.Wallet{}).Select("COALESCE(SUM(balance_kobo),0)").Scan(&rep.WalletBalancesKobo).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&models.EscrowHold{}).Where("status IN ?", []models.EscrowStatus{models.EscrowHeld, models.EscrowDisputed}).
		Select("COALESCE(SUM(amount_kobo),0)").Scan(&rep.EscrowHeldKobo).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&models.Withdrawal{}).
		Where("status NOT IN ?", []models.WithdrawalStatus{models.WithdrawalSuccessful, models.WithdrawalFailed, models.WithdrawalRejected}).
		Select("COALESCE(SUM(amount_kobo),0)").Scan(&rep.WithdrawalsInFlight).Error; err != nil {
		return nil, err
	}
	// Per-wallet drift.
	if err := db.Raw(`SELECT w.id FROM wallets w
		LEFT JOIN (SELECT wallet_id, SUM(amount_kobo) s FROM ledger_entries WHERE wallet_id IS NOT NULL GROUP BY wallet_id) e ON e.wallet_id = w.id
		WHERE w.balance_kobo <> COALESCE(e.s,0)`).Scan(&rep.MismatchedWallets).Error; err != nil {
		return nil, err
	}

	if rep.EntriesSumKobo != 0 {
		rep.Problems = append(rep.Problems, "ledger entries do not sum to zero")
	}
	if rep.WalletBalancesKobo != rep.WalletEntriesKobo {
		rep.Problems = append(rep.Problems, "wallet balances differ from wallet ledger entries")
	}
	if rep.EscrowPoolKobo != rep.EscrowHeldKobo {
		rep.Problems = append(rep.Problems, "escrow pool differs from open escrow holds")
	}
	if rep.PayoutPendingKobo != rep.WithdrawalsInFlight {
		rep.Problems = append(rep.Problems, "payout-pending differs from in-flight withdrawals")
	}
	if len(rep.MismatchedWallets) > 0 {
		rep.Problems = append(rep.Problems, fmt.Sprintf("%d wallet(s) drifted from their ledger", len(rep.MismatchedWallets)))
	}
	rep.OK = len(rep.Problems) == 0
	return rep, nil
}

func (r *ledgerRepository) WalletAggregates(ctx context.Context, userID, walletID string) (int64, int64, int64, error) {
	db := r.db.WithContext(ctx)
	open := []models.EscrowStatus{models.EscrowHeld, models.EscrowDisputed}
	var heldFor, incoming, pending int64
	if err := db.Model(&models.EscrowHold{}).Where("payer_user_id = ? AND status IN ?", userID, open).
		Select("COALESCE(SUM(amount_kobo),0)").Scan(&heldFor).Error; err != nil {
		return 0, 0, 0, err
	}
	if err := db.Model(&models.EscrowHold{}).Where("payee_user_id = ? AND status IN ?", userID, open).
		Select("COALESCE(SUM(amount_kobo),0)").Scan(&incoming).Error; err != nil {
		return 0, 0, 0, err
	}
	if err := db.Model(&models.Withdrawal{}).Where("wallet_id = ? AND status NOT IN ?", walletID,
		[]models.WithdrawalStatus{models.WithdrawalSuccessful, models.WithdrawalFailed, models.WithdrawalRejected}).
		Select("COALESCE(SUM(amount_kobo),0)").Scan(&pending).Error; err != nil {
		return 0, 0, 0, err
	}
	return heldFor, incoming, pending, nil
}
