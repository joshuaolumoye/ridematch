package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"ridematch-backend/internal/dto"
	"ridematch-backend/internal/email"
	"ridematch-backend/internal/flutterwave"
	"ridematch-backend/internal/models"
	"ridematch-backend/internal/money"
	"ridematch-backend/internal/repository"
	"ridematch-backend/internal/sms"
	"ridematch-backend/internal/storage"
	"ridematch-backend/internal/utils"
)

const (
	appealFreezeReason = "A next-of-kin appeal is under review"
	maxAppealDocuments = 10
)

// KinService manages a driver's next of kin and the appeal process by which
// the next of kin can ask for the driver's wallet balance if the driver dies
// or can no longer act.
//
// An appeal can never pay out on its own: it freezes withdrawals, notifies
// the driver (who can cancel it with their PIN), needs the recommendation of
// one admin and the final approval of a DIFFERENT admin, and then waits out a
// cooling-off period before the full balance is sent to the bank account the
// next of kin named.
type KinService struct {
	kin      repository.KinRepository
	ledger   repository.LedgerRepository
	users    repository.UserRepository
	drivers  repository.DriverRepository
	wallet   *WalletService
	stepUp   *StepUpService
	notify   *NotificationService
	audit    *AuditService
	gateway  flutterwave.WalletGateway
	sms      sms.Sender
	email    email.Sender
	files    storage.Store
	coolOff  time.Duration
	tokenTTL time.Duration
}

// NewKinService constructs a KinService.
func NewKinService(
	kin repository.KinRepository, ledger repository.LedgerRepository, users repository.UserRepository,
	drivers repository.DriverRepository, wallet *WalletService, stepUp *StepUpService, notify *NotificationService,
	audit *AuditService, gateway flutterwave.WalletGateway, smsSender sms.Sender, emailSender email.Sender,
	files storage.Store, coolOff, tokenTTL time.Duration,
) *KinService {
	return &KinService{kin: kin, ledger: ledger, users: users, drivers: drivers, wallet: wallet, stepUp: stepUp,
		notify: notify, audit: audit, gateway: gateway, sms: smsSender, email: emailSender, files: files,
		coolOff: coolOff, tokenTTL: tokenTTL}
}

func toKinResponse(n *models.NextOfKin) *dto.NextOfKinResponse {
	return &dto.NextOfKinResponse{FullName: n.FullName, Relationship: n.Relationship, Phone: n.Phone,
		Email: n.Email, Address: n.Address, UpdatedAt: n.UpdatedAt}
}

func (s *KinService) requireDriver(ctx context.Context, userID string) error {
	if _, err := s.drivers.FindByUserID(ctx, userID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrOnlyDrivers
		}
		return fmt.Errorf("service: failed to load driver profile: %w", err)
	}
	return nil
}

// Get returns the caller's next of kin.
func (s *KinService) Get(ctx context.Context, userID string) (*dto.NextOfKinResponse, error) {
	n, err := s.kin.FindByUser(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrNoNextOfKin
		}
		return nil, fmt.Errorf("service: failed to load next of kin: %w", err)
	}
	return toKinResponse(n), nil
}

// RequestCode sends the security code needed to change next of kin.
func (s *KinService) RequestCode(ctx context.Context, userID string) (*dto.StepUpOTPResponse, error) {
	if err := s.requireDriver(ctx, userID); err != nil {
		return nil, err
	}
	return s.stepUp.Request(ctx, userID, models.OTPPurposeNOK)
}

// Save sets or replaces the driver's next of kin.
func (s *KinService) Save(ctx context.Context, userID string, req dto.NextOfKinRequest) (*dto.NextOfKinResponse, error) {
	if err := s.requireDriver(ctx, userID); err != nil {
		return nil, err
	}
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return nil, ErrUserNotFound
	}
	phone, err := utils.NormalizePhone(req.Phone)
	if err != nil {
		return nil, fmt.Errorf("%w: enter a valid Nigerian phone number", ErrNextOfKinInvalid)
	}
	if phone == user.Phone {
		return nil, fmt.Errorf("%w: your next of kin needs their own phone number", ErrNextOfKinInvalid)
	}
	if open, err := s.kin.ListOpenAppealsForDriver(ctx, userID); err == nil && len(open) > 0 {
		return nil, fmt.Errorf("%w: an appeal is already open", ErrNextOfKinInvalid)
	}
	if err := s.stepUp.Verify(ctx, userID, models.OTPPurposeNOK, req.OTP); err != nil {
		return nil, err
	}
	now := time.Now()
	n := &models.NextOfKin{
		UserID: userID, FullName: strings.TrimSpace(req.FullName), Relationship: strings.TrimSpace(req.Relationship),
		Phone: phone, Email: strings.ToLower(strings.TrimSpace(req.Email)), Address: strings.TrimSpace(req.Address), VerifiedAt: &now,
	}
	if err := s.kin.Upsert(ctx, n); err != nil {
		return nil, fmt.Errorf("service: failed to save next of kin: %w", err)
	}
	s.audit.Record(ctx, Actor{UserID: userID, Role: "driver"}, "next_of_kin.save", "user", userID, nil)
	return toKinResponse(n), nil
}

// ---------------- public appeal flow ----------------

// locate finds the driver and their next of kin for the supplied driver
// identifier and kin phone. Any mismatch returns ErrAppealInvalid with no
// detail, so the endpoint can't be used to discover who is a driver.
func (s *KinService) locate(ctx context.Context, driverIdent, nokPhoneRaw string) (*models.User, *models.NextOfKin, string, error) {
	ident, channel, err := utils.NormalizeIdentifier(driverIdent)
	if err != nil {
		return nil, nil, "", ErrAppealInvalid
	}
	nokPhone, err := utils.NormalizePhone(nokPhoneRaw)
	if err != nil {
		return nil, nil, "", ErrAppealInvalid
	}
	var user *models.User
	if channel == utils.ChannelEmail {
		user, err = s.users.FindByEmail(ctx, ident)
	} else {
		user, err = s.users.FindByPhone(ctx, ident)
	}
	if err != nil {
		return nil, nil, "", ErrAppealInvalid
	}
	if _, err := s.drivers.FindByUserID(ctx, user.ID); err != nil {
		return nil, nil, "", ErrAppealInvalid
	}
	n, err := s.kin.FindByUser(ctx, user.ID)
	if err != nil || n.Phone != nokPhone {
		return nil, nil, "", ErrAppealInvalid
	}
	return user, n, nokPhone, nil
}

// RequestAppealCode texts a code to the next of kin's phone — but only if
// the pair matches what the driver registered. The caller always gets the
// same answer either way.
func (s *KinService) RequestAppealCode(ctx context.Context, req dto.AppealCodeRequest) {
	_, _, phone, err := s.locate(ctx, req.DriverIdentifier, req.NOKPhone)
	if err != nil {
		return
	}
	if err := s.stepUp.RequestToPhone(ctx, phone, models.OTPPurposeAppeal); err != nil && !errors.Is(err, ErrOTPCooldown) {
		log.Printf("kin: failed to send appeal code: %v", err)
	}
}

func newToken() (raw, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	raw = hex.EncodeToString(b)
	return raw, utils.HashToken(raw), nil
}

// SubmitAppeal files an appeal after the kin proves control of their phone.
func (s *KinService) SubmitAppeal(ctx context.Context, req dto.SubmitAppealRequest) (*dto.SubmitAppealResponse, error) {
	user, n, phone, err := s.locate(ctx, req.DriverIdentifier, req.NOKPhone)
	if err != nil {
		return nil, err
	}
	if err := s.stepUp.VerifyIdentifier(ctx, phone, models.OTPPurposeAppeal, req.OTP); err != nil {
		return nil, err
	}
	if open, err := s.kin.ListOpenAppealsForDriver(ctx, user.ID); err != nil {
		return nil, fmt.Errorf("service: failed to check open appeals: %w", err)
	} else if len(open) > 0 {
		return nil, fmt.Errorf("%w: an appeal for this driver is already open", ErrAppealInvalid)
	}
	w, err := s.ledger.EnsureWallet(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("service: failed to load wallet: %w", err)
	}
	if w.BalanceKobo <= 0 {
		return nil, ErrNothingToAppeal
	}

	banks, err := s.wallet.Banks(ctx)
	if err != nil {
		return nil, err
	}
	bankName := ""
	for _, b := range banks {
		if b.Code == req.BankCode {
			bankName = b.Name
		}
	}
	if bankName == "" {
		return nil, ErrInvalidBank
	}
	accName, err := s.wallet.resolve(ctx, req.BankCode, req.AccountNumber)
	if err != nil {
		return nil, err
	}

	raw, hash, err := newToken()
	if err != nil {
		return nil, fmt.Errorf("service: failed to create appeal token: %w", err)
	}
	a := &models.WithdrawalAppeal{
		DriverUserID: user.ID, NextOfKinID: n.ID, NOKName: n.FullName, NOKPhone: n.Phone,
		Reason: models.AppealReason(req.Reason), Details: strings.TrimSpace(req.Details), Status: models.AppealSubmitted,
		DestBankCode: req.BankCode, DestBankName: bankName, DestAccountNumber: req.AccountNumber, DestAccountName: accName,
		TokenHash: hash, TokenExpiresAt: time.Now().Add(s.tokenTTL),
	}
	if err := s.kin.CreateAppeal(ctx, a); err != nil {
		return nil, fmt.Errorf("service: failed to save appeal: %w", err)
	}

	// Freeze first: from this moment nothing leaves the wallet.
	if err := s.ledger.SetWalletFrozen(ctx, user.ID, true, appealFreezeReason); err != nil {
		log.Printf("ALERT kin: could not freeze wallet for appeal %s: %v", a.ID, err)
	}
	s.audit.Record(ctx, Actor{Role: "next_of_kin"}, "appeal.submit", "appeal", a.ID, map[string]interface{}{"driver_user_id": user.ID, "reason": req.Reason})
	s.alertDriver(ctx, user, a)
	return &dto.SubmitAppealResponse{AppealID: a.ID, Token: raw}, nil
}

// alertDriver tells the driver, on every channel we have, that an appeal was
// filed in their name — if they are alive and well they can cancel it.
func (s *KinService) alertDriver(ctx context.Context, user *models.User, a *models.WithdrawalAppeal) {
	msg := fmt.Sprintf("Someone (%s, your listed next of kin) asked MakeWeRide to release your wallet balance. Your withdrawals are paused. If this is a mistake, open the app and cancel it with your PIN.", a.NOKName)
	if s.notify != nil {
		if _, err := s.notify.SendToUser(ctx, user.ID, models.NotificationWallet, "Next-of-kin appeal filed", msg, map[string]string{"appeal_id": a.ID}); err != nil {
			log.Printf("kin: in-app notice failed: %v", err)
		}
	}
	if user.Phone != "" {
		if err := s.sms.Send(ctx, user.Phone, "MakeWeRide: "+msg); err != nil {
			log.Printf("kin: sms notice failed: %v", err)
		}
	}
	if user.Email != "" {
		if err := s.email.Send(ctx, user.Email, "A next-of-kin appeal was filed on your MakeWeRide wallet", msg); err != nil {
			log.Printf("kin: email notice failed: %v", err)
		}
	}
	now := time.Now()
	_, _ = s.kin.TransitionAppeal(ctx, a.ID, []models.AppealStatus{models.AppealSubmitted}, map[string]interface{}{"driver_notified_at": now, "status": models.AppealSubmitted})
}

func (s *KinService) byToken(ctx context.Context, token string) (*models.WithdrawalAppeal, error) {
	if len(token) != 64 {
		return nil, ErrAppealTokenInvalid
	}
	a, err := s.kin.FindAppealByTokenHash(ctx, utils.HashToken(token))
	if err != nil || time.Now().After(a.TokenExpiresAt) {
		return nil, ErrAppealTokenInvalid
	}
	return a, nil
}

var appealStatusNote = map[models.AppealStatus]string{
	models.AppealSubmitted:     "Received. Our team will review your documents.",
	models.AppealInfoRequested: "We need more information from you — see the message below.",
	models.AppealRecommended:   "Your appeal passed first review and is waiting for final approval.",
	models.AppealApproved:      "Approved. For safety, payment is released after a waiting period.",
	models.AppealDisbursed:     "The money has been sent to the bank account you provided.",
	models.AppealRejected:      "Your appeal was not approved. Contact support for details.",
	models.AppealCancelled:     "This appeal was cancelled.",
}

// StatusByToken shows the next of kin their appeal's progress.
func (s *KinService) StatusByToken(ctx context.Context, token string) (*dto.PublicAppealStatusResponse, error) {
	a, err := s.byToken(ctx, token)
	if err != nil {
		return nil, err
	}
	docs, err := s.kin.ListDocuments(ctx, a.ID)
	if err != nil {
		return nil, fmt.Errorf("service: failed to list documents: %w", err)
	}
	out := &dto.PublicAppealStatusResponse{AppealID: a.ID, Status: string(a.Status), StatusNote: appealStatusNote[a.Status],
		InfoRequest: a.InfoRequest, CreatedAt: a.CreatedAt, Documents: []dto.AppealDocumentResponse{}}
	for _, d := range docs {
		out.Documents = append(out.Documents, dto.AppealDocumentResponse{ID: d.ID, Kind: d.Kind, CreatedAt: d.CreatedAt})
	}
	return out, nil
}

// UploadDocument stores a supporting file against an appeal.
func (s *KinService) UploadDocument(ctx context.Context, token, kind, filename string, data io.Reader, size int64, contentType string) (*dto.AppealDocumentResponse, error) {
	a, err := s.byToken(ctx, token)
	if err != nil {
		return nil, err
	}
	switch a.Status {
	case models.AppealSubmitted, models.AppealInfoRequested:
	default:
		return nil, ErrAppealState
	}
	if n, err := s.kin.CountDocuments(ctx, a.ID); err == nil && n >= maxAppealDocuments {
		return nil, fmt.Errorf("%w: at most %d documents", ErrAppealState, maxAppealDocuments)
	}
	url, err := s.files.Save(ctx, filename, data, size, contentType)
	if err != nil {
		return nil, fmt.Errorf("service: failed to store appeal document: %w", err)
	}
	d := &models.AppealDocument{AppealID: a.ID, Kind: kind, URL: url}
	if err := s.kin.AddDocument(ctx, d); err != nil {
		return nil, fmt.Errorf("service: failed to save appeal document: %w", err)
	}
	if a.Status == models.AppealInfoRequested {
		_, _ = s.kin.TransitionAppeal(ctx, a.ID, []models.AppealStatus{models.AppealInfoRequested}, map[string]interface{}{"status": models.AppealSubmitted})
	}
	return &dto.AppealDocumentResponse{ID: d.ID, Kind: d.Kind, CreatedAt: d.CreatedAt}, nil
}

// ---------------- driver side ----------------

// DriverAppeals lists open appeals filed against the caller.
func (s *KinService) DriverAppeals(ctx context.Context, userID string) ([]dto.DriverAppealResponse, error) {
	rows, err := s.kin.ListOpenAppealsForDriver(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("service: failed to list appeals: %w", err)
	}
	out := make([]dto.DriverAppealResponse, len(rows))
	for i, a := range rows {
		out[i] = dto.DriverAppealResponse{ID: a.ID, Status: string(a.Status), Reason: string(a.Reason), NOKName: a.NOKName, CreatedAt: a.CreatedAt}
	}
	return out, nil
}

// DriverCancel lets the driver themself cancel an appeal filed in their name.
func (s *KinService) DriverCancel(ctx context.Context, userID, appealID, pin string) error {
	a, err := s.kin.FindAppealByID(ctx, appealID)
	if err != nil || a.DriverUserID != userID {
		return ErrAppealNotFound
	}
	w, err := s.ledger.EnsureWallet(ctx, userID)
	if err != nil {
		return fmt.Errorf("service: failed to load wallet: %w", err)
	}
	if err := s.wallet.checkPIN(ctx, w, pin); err != nil {
		return err
	}
	ok, err := s.kin.TransitionAppeal(ctx, appealID,
		[]models.AppealStatus{models.AppealSubmitted, models.AppealInfoRequested, models.AppealRecommended, models.AppealApproved},
		map[string]interface{}{"status": models.AppealCancelled, "cancelled_at": time.Now()})
	if err != nil {
		return fmt.Errorf("service: failed to cancel appeal: %w", err)
	}
	if !ok {
		return ErrAppealState
	}
	s.audit.Record(ctx, Actor{UserID: userID, Role: "driver"}, "appeal.cancel_by_driver", "appeal", appealID, nil)
	s.maybeUnfreeze(ctx, userID)
	return nil
}

// maybeUnfreeze lifts the appeal freeze once no appeal remains open — and
// only if the freeze was the appeal's (never an admin's own freeze).
func (s *KinService) maybeUnfreeze(ctx context.Context, userID string) {
	open, err := s.kin.ListOpenAppealsForDriver(ctx, userID)
	if err != nil || len(open) > 0 {
		return
	}
	w, err := s.ledger.FindWalletByUserID(ctx, userID)
	if err != nil || !w.WithdrawalsFrozen || w.FrozenReason != appealFreezeReason {
		return
	}
	if err := s.ledger.SetWalletFrozen(ctx, userID, false, ""); err != nil {
		log.Printf("ALERT kin: could not lift appeal freeze for %s: %v", userID, err)
	}
}

// ---------------- admin ----------------

func (s *KinService) adminAppeal(ctx context.Context, a *models.WithdrawalAppeal) dto.AdminAppealResponse {
	r := dto.AdminAppealResponse{
		ID: a.ID, Status: string(a.Status), Reason: string(a.Reason), Details: a.Details, DriverUserID: a.DriverUserID,
		NOKName: a.NOKName, NOKPhone: a.NOKPhone, DestBankName: a.DestBankName, DestAccountNumber: a.DestAccountNumber,
		DestAccountName: a.DestAccountName, DriverNotifiedAt: a.DriverNotifiedAt, RecommendedBy: a.RecommendedBy,
		ApprovedBy: a.ApprovedBy, DisbursableAfter: a.DisbursableAfter, InfoRequest: a.InfoRequest,
		RejectionReason: a.RejectionReason, AdminNotes: a.AdminNotes, AmountKobo: a.AmountKobo, CreatedAt: a.CreatedAt,
		Documents: []dto.AppealDocumentResponse{},
	}
	if u, err := s.users.FindByID(ctx, a.DriverUserID); err == nil {
		r.DriverName, r.DriverPhone = u.Name, u.Phone
	}
	if n, err := s.kin.FindByUser(ctx, a.DriverUserID); err == nil {
		r.NOKRelationship = n.Relationship
	}
	if w, err := s.ledger.FindWalletByUserID(ctx, a.DriverUserID); err == nil {
		r.WalletBalanceKobo = w.BalanceKobo
	}
	if docs, err := s.kin.ListDocuments(ctx, a.ID); err == nil {
		for _, d := range docs {
			r.Documents = append(r.Documents, dto.AppealDocumentResponse{ID: d.ID, Kind: d.Kind, URL: signURL(ctx, s.files, d.URL), CreatedAt: d.CreatedAt})
		}
	}
	return r
}

// AdminList lists appeals by status.
func (s *KinService) AdminList(ctx context.Context, status string, page, pageSize int) ([]dto.AdminAppealResponse, int64, error) {
	page, pageSize = normalizePage(page, pageSize)
	rows, total, err := s.kin.ListAppeals(ctx, status, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("service: failed to list appeals: %w", err)
	}
	out := make([]dto.AdminAppealResponse, len(rows))
	for i := range rows {
		out[i] = s.adminAppeal(ctx, &rows[i])
	}
	return out, total, nil
}

// AdminGet returns one appeal (viewing is audited).
func (s *KinService) AdminGet(ctx context.Context, actor Actor, id string) (*dto.AdminAppealResponse, error) {
	a, err := s.kin.FindAppealByID(ctx, id)
	if err != nil {
		return nil, ErrAppealNotFound
	}
	s.audit.Record(ctx, actor, "appeal.view", "appeal", id, nil)
	r := s.adminAppeal(ctx, a)
	return &r, nil
}

// AdminDecide advances an appeal. Decisions: request_info, recommend,
// approve, reject. "approve" must come from a different admin than the one
// who recommended.
func (s *KinService) AdminDecide(ctx context.Context, actor Actor, id string, req dto.AdminDecisionRequest) (*dto.AdminAppealResponse, error) {
	a, err := s.kin.FindAppealByID(ctx, id)
	if err != nil {
		return nil, ErrAppealNotFound
	}
	note := strings.TrimSpace(req.Note)
	now := time.Now()
	var ok bool
	switch req.Decision {
	case "request_info":
		ok, err = s.kin.TransitionAppeal(ctx, id, []models.AppealStatus{models.AppealSubmitted, models.AppealRecommended},
			map[string]interface{}{"status": models.AppealInfoRequested, "info_request": note})
	case "recommend":
		if n, _ := s.kin.CountDocuments(ctx, id); n == 0 {
			return nil, fmt.Errorf("%w: no supporting documents have been uploaded yet", ErrAppealState)
		}
		ok, err = s.kin.TransitionAppeal(ctx, id, []models.AppealStatus{models.AppealSubmitted},
			map[string]interface{}{"status": models.AppealRecommended, "recommended_by": actor.UserID, "recommended_at": now, "admin_notes": note})
	case "approve":
		if a.RecommendedBy == actor.UserID {
			return nil, ErrSameApprover
		}
		after := now.Add(s.coolOff)
		ok, err = s.kin.TransitionAppeal(ctx, id, []models.AppealStatus{models.AppealRecommended},
			map[string]interface{}{"status": models.AppealApproved, "approved_by": actor.UserID, "approved_at": now, "disbursable_after": after})
	case "reject":
		ok, err = s.kin.TransitionAppeal(ctx, id,
			[]models.AppealStatus{models.AppealSubmitted, models.AppealInfoRequested, models.AppealRecommended, models.AppealApproved},
			map[string]interface{}{"status": models.AppealRejected, "rejected_by": actor.UserID, "rejection_reason": note})
	default:
		return nil, fmt.Errorf("%w: unknown decision", ErrAppealState)
	}
	if err != nil {
		return nil, fmt.Errorf("service: failed to update appeal: %w", err)
	}
	if !ok {
		return nil, ErrAppealState
	}
	s.audit.Record(ctx, actor, "appeal."+req.Decision, "appeal", id, map[string]interface{}{"note": note})
	if req.Decision == "reject" {
		s.maybeUnfreeze(ctx, a.DriverUserID)
	}
	a, _ = s.kin.FindAppealByID(ctx, id)
	r := s.adminAppeal(ctx, a)
	return &r, nil
}

// DisburseDue pays out approved appeals whose cooling-off has elapsed. The
// payout is an ordinary withdrawal with a reference derived from the appeal,
// so running this twice (or crashing midway) can never pay twice.
func (s *KinService) DisburseDue(ctx context.Context) {
	if !s.wallet.PayoutsEnabled(ctx) {
		return
	}
	rows, err := s.kin.DueAppeals(ctx, time.Now(), 20)
	if err != nil {
		log.Printf("kin: due appeals scan failed: %v", err)
		return
	}
	for i := range rows {
		a := &rows[i]
		if err := s.disburse(ctx, a); err != nil {
			log.Printf("ALERT kin: disbursement of appeal %s failed: %v", a.ID, err)
		}
	}
}

func (s *KinService) disburse(ctx context.Context, a *models.WithdrawalAppeal) error {
	ref := "appeal_" + a.ID
	if existing, err := s.ledger.FindWithdrawalByReference(ctx, ref); err == nil {
		// Created on an earlier run that crashed before marking the appeal.
		_, err := s.kin.TransitionAppeal(ctx, a.ID, []models.AppealStatus{models.AppealApproved},
			map[string]interface{}{"status": models.AppealDisbursed, "amount_kobo": existing.AmountKobo, "withdrawal_id": existing.ID})
		return err
	}
	w, err := s.ledger.FindWalletByUserID(ctx, a.DriverUserID)
	if err != nil {
		return err
	}
	amount := (w.BalanceKobo / 100) * 100 // whole naira
	fee := money.TransferFee(amount)
	if amount <= fee {
		_, err := s.kin.TransitionAppeal(ctx, a.ID, []models.AppealStatus{models.AppealApproved},
			map[string]interface{}{"status": models.AppealRejected, "rejection_reason": "balance too small to pay out"})
		return err
	}
	wd := &models.Withdrawal{
		UserID: a.DriverUserID, WalletID: w.ID, Kind: models.WithdrawalKindAppeal, Status: models.WithdrawalQueued,
		AmountKobo: amount, FeeKobo: fee, NetKobo: amount - fee, BankCode: a.DestBankCode, BankName: a.DestBankName,
		AccountNumber: a.DestAccountNumber, AccountName: a.DestAccountName, Reference: ref, AppealID: &a.ID, RiskFlags: "appeal",
	}
	if err := s.ledger.CreateWithdrawal(ctx, wd, repository.WithdrawalGuard{Now: time.Now()}); err != nil {
		return err
	}
	if _, err := s.kin.TransitionAppeal(ctx, a.ID, []models.AppealStatus{models.AppealApproved},
		map[string]interface{}{"status": models.AppealDisbursed, "amount_kobo": amount, "withdrawal_id": wd.ID}); err != nil {
		return err
	}
	s.audit.Record(ctx, Actor{Role: "system"}, "appeal.disburse", "appeal", a.ID, map[string]interface{}{"amount_kobo": amount})
	go s.wallet.dispatchDetached(wd.ID)
	return nil
}
