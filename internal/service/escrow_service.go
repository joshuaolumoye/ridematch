package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"ridematch-backend/internal/dto"
	"ridematch-backend/internal/models"
	"ridematch-backend/internal/repository"
)

// EscrowService moves trip fares through escrow: held from the passenger's
// wallet when a driver is confirmed, released to the driver's wallet when the
// trip is done (confirmed by the passenger or automatically after a window),
// refunded to the passenger on cancellation, or parked for an admin when the
// passenger reports a problem. The platform takes no commission.
type EscrowService struct {
	ledger      repository.LedgerRepository
	users       repository.UserRepository
	trips       repository.TripRepository
	notify      *NotificationService
	audit       *AuditService
	required    bool
	autoRelease time.Duration
}

// NewEscrowService constructs an EscrowService.
func NewEscrowService(
	ledger repository.LedgerRepository, users repository.UserRepository, trips repository.TripRepository,
	notify *NotificationService, audit *AuditService, required bool, autoRelease time.Duration,
) *EscrowService {
	return &EscrowService{ledger: ledger, users: users, trips: trips, notify: notify, audit: audit, required: required, autoRelease: autoRelease}
}

// Required reports whether trips must be pre-paid into escrow.
func (s *EscrowService) Required() bool { return s != nil && s.required }

func (s *EscrowService) push(ctx context.Context, userID, title, body string, data map[string]string) {
	if s.notify == nil {
		return
	}
	if _, err := s.notify.SendToUser(ctx, userID, models.NotificationWallet, title, body, data); err != nil {
		log.Printf("escrow: notification to %s failed: %v", userID, err)
	}
}

// EnsureCanAfford rejects a request the passenger's wallet can't cover.
func (s *EscrowService) EnsureCanAfford(ctx context.Context, passengerID string, amountKobo int64) error {
	if !s.Required() {
		return nil
	}
	w, err := s.ledger.EnsureWallet(ctx, passengerID)
	if err != nil {
		return fmt.Errorf("service: failed to load wallet: %w", err)
	}
	if w.BalanceKobo < amountKobo {
		return fmt.Errorf("%w: you need %s but have %s — top up your wallet to continue",
			ErrInsufficientBalance, Naira(amountKobo), Naira(w.BalanceKobo))
	}
	return nil
}

// Hold moves the fare into escrow for a trip being matched. created reports
// whether this call took the money (so only it may undo it on failure).
func (s *EscrowService) Hold(ctx context.Context, trip *models.Trip, driver *models.DriverProfile, amountKobo int64) (created bool, err error) {
	if !s.Required() {
		return false, nil
	}
	_, created, err = s.ledger.HoldEscrow(ctx, &models.EscrowHold{
		TripID: trip.ID, PayerUserID: trip.PassengerID, PayeeUserID: driver.UserID,
		PayeeDriverID: driver.ID, AmountKobo: amountKobo,
	})
	if err != nil {
		if errors.Is(err, repository.ErrInsufficientFunds) {
			w, _ := s.ledger.EnsureWallet(ctx, trip.PassengerID)
			bal := int64(0)
			if w != nil {
				bal = w.BalanceKobo
			}
			return false, fmt.Errorf("%w: you need %s but have %s — top up your wallet to continue",
				ErrInsufficientBalance, Naira(amountKobo), Naira(bal))
		}
		if errors.Is(err, repository.ErrHoldConflict) {
			return false, ErrTripNotOpen
		}
		return false, fmt.Errorf("service: failed to hold trip payment: %w", err)
	}
	return created, nil
}

// Peek returns the escrow summary shown inside a trip response (nil if none).
func (s *EscrowService) Peek(ctx context.Context, tripID string) *dto.EscrowResponse {
	if !s.Required() {
		return nil
	}
	h, err := s.ledger.FindEscrowByTrip(ctx, tripID)
	if err != nil {
		return nil
	}
	return toEscrowResponse(h)
}

// Refund returns a held fare to the passenger. A trip with no hold is fine.
func (s *EscrowService) Refund(ctx context.Context, tripID, note string) {
	if s == nil {
		return
	}
	applied, err := s.ledger.RefundEscrow(ctx, tripID, "", note)
	if err != nil {
		if !errors.Is(err, repository.ErrNotFound) {
			log.Printf("ALERT escrow: refund for trip %s failed: %v", tripID, err)
		}
		return
	}
	if applied {
		if h, err := s.ledger.FindEscrowByTrip(ctx, tripID); err == nil {
			s.push(ctx, h.PayerUserID, "Payment returned", fmt.Sprintf("%s was returned to your wallet.", Naira(h.AmountKobo)), map[string]string{"trip_id": tripID})
		}
	}
}

// OnTripCompleted starts the confirmation window.
func (s *EscrowService) OnTripCompleted(ctx context.Context, trip *models.Trip) {
	if !s.Required() {
		return
	}
	release := time.Now().Add(s.autoRelease)
	if err := s.ledger.MarkEscrowCompleted(ctx, trip.ID, release); err != nil {
		log.Printf("ALERT escrow: failed to start release window for trip %s: %v", trip.ID, err)
		return
	}
	s.push(ctx, trip.PassengerID, "Trip complete",
		"Confirm the trip to pay your driver, or report a problem within the next "+humanDuration(s.autoRelease)+".",
		map[string]string{"trip_id": trip.ID})
}

func humanDuration(d time.Duration) string {
	if d >= time.Hour {
		return fmt.Sprintf("%d hour(s)", int(d.Hours()))
	}
	return fmt.Sprintf("%d minutes", int(d.Minutes()))
}

func (s *EscrowService) release(ctx context.Context, tripID, note string) error {
	applied, err := s.ledger.ReleaseEscrow(ctx, tripID, note)
	if err != nil {
		return err
	}
	if applied {
		if h, err := s.ledger.FindEscrowByTrip(ctx, tripID); err == nil {
			s.push(ctx, h.PayeeUserID, "Payment received", fmt.Sprintf("%s has been added to your wallet.", Naira(h.AmountKobo)), map[string]string{"trip_id": tripID})
			s.push(ctx, h.PayerUserID, "Payment sent", fmt.Sprintf("%s was paid to your driver. Thanks for riding with us.", Naira(h.AmountKobo)), map[string]string{"trip_id": tripID})
		}
	}
	return nil
}

func toEscrowResponse(h *models.EscrowHold) *dto.EscrowResponse {
	return &dto.EscrowResponse{TripID: h.TripID, Status: string(h.Status), AmountKobo: h.AmountKobo,
		ReleaseAfter: h.ReleaseAfter, Disputed: h.Status == models.EscrowDisputed}
}

// ForTrip returns the escrow state for a trip to either participant.
func (s *EscrowService) ForTrip(ctx context.Context, userID, tripID string) (*dto.EscrowResponse, error) {
	h, err := s.ledger.FindEscrowByTrip(ctx, tripID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrEscrowNotFound
		}
		return nil, err
	}
	if h.PayerUserID != userID && h.PayeeUserID != userID {
		return nil, ErrEscrowNotFound
	}
	return toEscrowResponse(h), nil
}

// ConfirmTrip lets the passenger release payment to the driver right away.
func (s *EscrowService) ConfirmTrip(ctx context.Context, passengerID, tripID string) (*dto.EscrowResponse, error) {
	trip, err := s.trips.FindByID(ctx, tripID)
	if err != nil {
		return nil, ErrEscrowNotFound
	}
	h, err := s.ledger.FindEscrowByTrip(ctx, tripID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrEscrowNotFound
		}
		return nil, err
	}
	if h.PayerUserID != passengerID {
		return nil, ErrNotPayer
	}
	if trip.Status != models.TripCompleted || h.Status != models.EscrowHeld {
		return nil, ErrEscrowState
	}
	if err := s.release(ctx, tripID, "confirmed by passenger"); err != nil {
		return nil, fmt.Errorf("service: failed to release payment: %w", err)
	}
	h, _ = s.ledger.FindEscrowByTrip(ctx, tripID)
	return toEscrowResponse(h), nil
}

// DisputeTrip parks the payment for admin review.
func (s *EscrowService) DisputeTrip(ctx context.Context, passengerID, tripID, reason string) (*dto.EscrowResponse, error) {
	trip, err := s.trips.FindByID(ctx, tripID)
	if err != nil {
		return nil, ErrEscrowNotFound
	}
	h, err := s.ledger.FindEscrowByTrip(ctx, tripID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrEscrowNotFound
		}
		return nil, err
	}
	if h.PayerUserID != passengerID {
		return nil, ErrNotPayer
	}
	if (trip.Status != models.TripCompleted && trip.Status != models.TripPickedUp) || h.Status != models.EscrowHeld {
		return nil, ErrEscrowState
	}
	if err := s.ledger.DisputeEscrow(ctx, tripID, strings.TrimSpace(reason)); err != nil {
		if errors.Is(err, repository.ErrInvalidTransition) {
			return nil, ErrEscrowState
		}
		return nil, fmt.Errorf("service: failed to open dispute: %w", err)
	}
	s.audit.Record(ctx, Actor{UserID: passengerID, Role: "user"}, "escrow.dispute", "trip", tripID, map[string]interface{}{"reason": reason})
	s.push(ctx, h.PayeeUserID, "Trip payment on hold", "The passenger reported a problem with a trip. Our team will review it and update you.", map[string]string{"trip_id": tripID})
	h, _ = s.ledger.FindEscrowByTrip(ctx, tripID)
	return toEscrowResponse(h), nil
}

// ReleaseDue pays out every hold whose confirmation window has elapsed.
func (s *EscrowService) ReleaseDue(ctx context.Context) {
	rows, err := s.ledger.DueEscrows(ctx, time.Now(), 100)
	if err != nil {
		log.Printf("escrow: due scan failed: %v", err)
		return
	}
	for _, h := range rows {
		if err := s.release(ctx, h.TripID, "auto-released after confirmation window"); err != nil {
			log.Printf("ALERT escrow: auto-release of trip %s failed: %v", h.TripID, err)
		}
	}
}

// ---- admin ----

// AdminList lists escrow holds (default: disputed ones need attention).
func (s *EscrowService) AdminList(ctx context.Context, status string, page, pageSize int) ([]dto.AdminEscrowResponse, int64, error) {
	page, pageSize = normalizePage(page, pageSize)
	rows, total, err := s.ledger.ListEscrow(ctx, status, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("service: failed to list escrow: %w", err)
	}
	out := make([]dto.AdminEscrowResponse, len(rows))
	for i, h := range rows {
		r := dto.AdminEscrowResponse{ID: h.ID, TripID: h.TripID, AmountKobo: h.AmountKobo, Status: string(h.Status),
			ReleaseAfter: h.ReleaseAfter, DisputeReason: h.DisputeReason, DisputedAt: h.DisputedAt,
			Resolution: h.Resolution, CreatedAt: h.CreatedAt}
		if u, err := s.users.FindByID(ctx, h.PayerUserID); err == nil {
			r.PassengerName = u.Name
		}
		if u, err := s.users.FindByID(ctx, h.PayeeUserID); err == nil {
			r.DriverName = u.Name
		}
		out[i] = r
	}
	return out, total, nil
}

// AdminResolve settles a held or disputed payment: "release" pays the
// driver, "refund" returns it to the passenger.
func (s *EscrowService) AdminResolve(ctx context.Context, actor Actor, tripID string, req dto.AdminDecisionRequest) error {
	h, err := s.ledger.FindEscrowByTrip(ctx, tripID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrEscrowNotFound
		}
		return err
	}
	if h.Status != models.EscrowHeld && h.Status != models.EscrowDisputed {
		return ErrEscrowState
	}
	note := strings.TrimSpace(req.Note)
	switch req.Decision {
	case "release":
		if err := s.release(ctx, tripID, "admin: "+note); err != nil {
			return fmt.Errorf("service: failed to release payment: %w", err)
		}
	case "refund":
		if _, err := s.ledger.RefundEscrow(ctx, tripID, actor.UserID, "admin: "+note); err != nil {
			return fmt.Errorf("service: failed to refund payment: %w", err)
		}
		s.push(ctx, h.PayerUserID, "Payment returned", fmt.Sprintf("%s was returned to your wallet after review.", Naira(h.AmountKobo)), map[string]string{"trip_id": tripID})
		s.push(ctx, h.PayeeUserID, "Trip payment reversed", "After review, the payment for a trip was returned to the passenger.", map[string]string{"trip_id": tripID})
	default:
		return ErrEscrowState
	}
	s.audit.Record(ctx, actor, "escrow."+req.Decision, "trip", tripID, map[string]interface{}{"note": note, "amount_kobo": h.AmountKobo})
	return nil
}
