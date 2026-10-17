package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"ridematch-backend/internal/dto"
	"ridematch-backend/internal/email"
	"ridematch-backend/internal/models"
	"ridematch-backend/internal/repository"
	"ridematch-backend/internal/sms"
	"ridematch-backend/internal/utils"
)

// StepUpService issues and verifies short-lived one-time codes that
// authorise one sensitive action (setting a PIN, adding a bank account,
// changing next of kin, filing an appeal). A code is scoped to a purpose and
// to the account holder's own verified phone/email, so a stolen session
// token alone cannot perform these actions.
type StepUpService struct {
	users       repository.UserRepository
	otps        repository.OTPRepository
	smsSender   sms.Sender
	emailSender email.Sender
	ttl         time.Duration
	length      int
	cooldown    time.Duration
	maxAttempts int
}

// NewStepUpService constructs a StepUpService.
func NewStepUpService(
	users repository.UserRepository, otps repository.OTPRepository,
	smsSender sms.Sender, emailSender email.Sender,
	ttl time.Duration, length int, cooldown time.Duration, maxAttempts int,
) *StepUpService {
	return &StepUpService{users: users, otps: otps, smsSender: smsSender, emailSender: emailSender,
		ttl: ttl, length: length, cooldown: cooldown, maxAttempts: maxAttempts}
}

var purposeLabel = map[models.OTPPurpose]string{
	models.OTPPurposeSetPIN:     "set your withdrawal PIN",
	models.OTPPurposePayoutAcct: "add a payout bank account",
	models.OTPPurposeNOK:        "update your next of kin",
	models.OTPPurposeAppeal:     "confirm a withdrawal appeal",
}

// accountIdentifier picks the user's own verified contact for the code.
func accountIdentifier(u *models.User) (identifier string, isEmail bool, err error) {
	switch {
	case u.Phone != "" && u.PhoneVerifiedAt != nil:
		return u.Phone, false, nil
	case u.Email != "" && u.EmailVerifiedAt != nil:
		return u.Email, true, nil
	case u.Phone != "":
		return u.Phone, false, nil
	case u.Email != "":
		return u.Email, true, nil
	}
	return "", false, ErrNoContactOnFile
}

func maskIdentifier(id string, isEmail bool) string {
	if isEmail {
		at := strings.Index(id, "@")
		if at <= 1 {
			return id
		}
		return id[:1] + strings.Repeat("*", at-1) + id[at:]
	}
	if len(id) <= 5 {
		return id
	}
	return id[:4] + strings.Repeat("*", len(id)-7) + id[len(id)-3:]
}

// Request sends a code for purpose to the user's own contact.
func (s *StepUpService) Request(ctx context.Context, userID string, purpose models.OTPPurpose) (*dto.StepUpOTPResponse, error) {
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("service: failed to load user: %w", err)
	}
	identifier, isEmail, err := accountIdentifier(user)
	if err != nil {
		return nil, err
	}
	if err := s.send(ctx, identifier, isEmail, purpose); err != nil {
		return nil, err
	}
	return &dto.StepUpOTPResponse{
		SentTo: maskIdentifier(identifier, isEmail), ExpiresInSeconds: int(s.ttl.Seconds()), ResendInSeconds: int(s.cooldown.Seconds()),
	}, nil
}

// RequestToPhone sends a purpose-scoped code to an arbitrary phone number
// (used to prove a next of kin controls the phone number on file).
func (s *StepUpService) RequestToPhone(ctx context.Context, phone string, purpose models.OTPPurpose) error {
	return s.send(ctx, phone, false, purpose)
}

func (s *StepUpService) send(ctx context.Context, identifier string, isEmail bool, purpose models.OTPPurpose) error {
	if last, err := s.otps.FindLatestAny(ctx, identifier, purpose); err == nil {
		if time.Since(last.CreatedAt) < s.cooldown {
			return ErrOTPCooldown
		}
	} else if !errors.Is(err, repository.ErrNotFound) {
		return fmt.Errorf("service: failed to check otp cooldown: %w", err)
	}

	code, err := utils.GenerateOTP(s.length)
	if err != nil {
		return err
	}
	hash, err := utils.HashOTP(code)
	if err != nil {
		return err
	}
	channel := models.OTPChannelPhone
	if isEmail {
		channel = models.OTPChannelEmail
	}
	row := &models.OTPRequest{
		Identifier: identifier, Channel: channel, CodeHash: hash, Purpose: purpose,
		ExpiresAt: time.Now().Add(s.ttl), MaxAttempts: s.maxAttempts,
	}
	if err := s.otps.Create(ctx, row); err != nil {
		return fmt.Errorf("service: failed to persist otp: %w", err)
	}

	label := purposeLabel[purpose]
	minutes := int(s.ttl.Minutes())
	var sendErr error
	if isEmail {
		sendErr = s.emailSender.Send(ctx, identifier, "Your MakeWeRide security code",
			fmt.Sprintf("Your code to %s is %s.\n\nIt expires in %d minutes. Never share this code — MakeWeRide staff will never ask for it.", label, code, minutes))
	} else {
		sendErr = s.smsSender.Send(ctx, identifier,
			fmt.Sprintf("MakeWeRide: your code to %s is %s. Expires in %d min. Never share it.", label, code, minutes))
	}
	if sendErr != nil {
		if delErr := s.otps.Delete(ctx, row.ID); delErr != nil {
			log.Printf("stepup: failed to roll back undelivered otp %s: %v", row.ID, delErr)
		}
		return fmt.Errorf("service: failed to deliver security code: %w", sendErr)
	}
	return nil
}

// Verify checks and consumes a code for the user's own contact.
func (s *StepUpService) Verify(ctx context.Context, userID string, purpose models.OTPPurpose, code string) error {
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrUserNotFound
		}
		return fmt.Errorf("service: failed to load user: %w", err)
	}
	identifier, _, err := accountIdentifier(user)
	if err != nil {
		return err
	}
	return s.VerifyIdentifier(ctx, identifier, purpose, code)
}

// VerifyIdentifier checks and consumes a code sent to a specific identifier.
func (s *StepUpService) VerifyIdentifier(ctx context.Context, identifier string, purpose models.OTPPurpose, code string) error {
	otp, err := s.otps.FindLatestActive(ctx, identifier, purpose)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrOTPNotFound
		}
		return fmt.Errorf("service: failed to load otp: %w", err)
	}
	if otp.AttemptsExceeded() {
		return ErrOTPTooManyAttempts
	}
	if !utils.CompareOTP(otp.CodeHash, code) {
		otp.Attempts++
		_ = s.otps.Update(ctx, otp)
		if otp.AttemptsExceeded() {
			return ErrOTPTooManyAttempts
		}
		return ErrOTPIncorrect
	}
	now := time.Now()
	otp.ConsumedAt = &now
	if err := s.otps.Update(ctx, otp); err != nil {
		return fmt.Errorf("service: failed to consume otp: %w", err)
	}
	return nil
}
