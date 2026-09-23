package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"ridematch-backend/internal/dto"
	"ridematch-backend/internal/models"
	"ridematch-backend/internal/repository"
	"ridematch-backend/internal/sms"
	"ridematch-backend/internal/utils"
)

// AuthService implements phone + OTP registration/login, and JWT access /
// opaque refresh token issuance and rotation.
type AuthService struct {
	users       repository.UserRepository
	otps        repository.OTPRepository
	tokens      repository.RefreshTokenRepository
	sender      sms.Sender
	jwt         *utils.JWTManager
	otpTTL      time.Duration
	otpLen      int
	cooldown    time.Duration
	maxAttempts int
}

// NewAuthService constructs an AuthService from its dependencies.
func NewAuthService(
	users repository.UserRepository,
	otps repository.OTPRepository,
	tokens repository.RefreshTokenRepository,
	sender sms.Sender,
	jwtManager *utils.JWTManager,
	otpTTL time.Duration,
	otpLength int,
	resendCooldown time.Duration,
	maxAttempts int,
) *AuthService {
	return &AuthService{
		users:       users,
		otps:        otps,
		tokens:      tokens,
		sender:      sender,
		jwt:         jwtManager,
		otpTTL:      otpTTL,
		otpLen:      otpLength,
		cooldown:    resendCooldown,
		maxAttempts: maxAttempts,
	}
}

// RequestOTP normalizes the phone number, enforces the resend cooldown,
// generates and stores a hashed OTP code, and dispatches it by SMS.
func (s *AuthService) RequestOTP(ctx context.Context, rawPhone string) (*dto.RequestOTPResponse, error) {
	phone, err := utils.NormalizePhone(rawPhone)
	if err != nil {
		return nil, err
	}

	if last, err := s.otps.FindLatestAny(ctx, phone, models.OTPPurposeLogin); err == nil {
		if time.Since(last.CreatedAt) < s.cooldown {
			return nil, ErrOTPCooldown
		}
	} else if !errors.Is(err, repository.ErrNotFound) {
		return nil, fmt.Errorf("service: failed to check otp cooldown: %w", err)
	}

	code, err := utils.GenerateOTP(s.otpLen)
	if err != nil {
		return nil, err
	}

	hash, err := utils.HashOTP(code)
	if err != nil {
		return nil, err
	}

	otp := &models.OTPRequest{
		Phone:       phone,
		CodeHash:    hash,
		Purpose:     models.OTPPurposeLogin,
		ExpiresAt:   time.Now().Add(s.otpTTL),
		MaxAttempts: s.maxAttempts,
	}
	if err := s.otps.Create(ctx, otp); err != nil {
		return nil, fmt.Errorf("service: failed to persist otp: %w", err)
	}

	message := sms.BuildOTPMessage(code, int(s.otpTTL.Minutes()))
	if err := s.sender.Send(ctx, phone, message); err != nil {
		return nil, fmt.Errorf("service: failed to send otp sms: %w", err)
	}

	return &dto.RequestOTPResponse{
		Phone:            phone,
		ExpiresInSeconds: int(s.otpTTL.Seconds()),
		ResendInSeconds:  int(s.cooldown.Seconds()),
	}, nil
}

// VerifyOTP validates a submitted code against the most recent active
// challenge for the phone number. On success it finds-or-creates the
// User account and issues a fresh access/refresh token pair.
func (s *AuthService) VerifyOTP(ctx context.Context, rawPhone, code, name string) (*dto.VerifyOTPResponse, error) {
	phone, err := utils.NormalizePhone(rawPhone)
	if err != nil {
		return nil, err
	}

	otp, err := s.otps.FindLatestActive(ctx, phone, models.OTPPurposeLogin)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrOTPNotFound
		}
		return nil, fmt.Errorf("service: failed to load otp: %w", err)
	}

	if otp.AttemptsExceeded() {
		return nil, ErrOTPTooManyAttempts
	}

	if !utils.CompareOTP(otp.CodeHash, code) {
		otp.Attempts++
		_ = s.otps.Update(ctx, otp)
		if otp.AttemptsExceeded() {
			return nil, ErrOTPTooManyAttempts
		}
		return nil, ErrOTPIncorrect
	}

	now := time.Now()
	otp.ConsumedAt = &now
	if err := s.otps.Update(ctx, otp); err != nil {
		return nil, fmt.Errorf("service: failed to consume otp: %w", err)
	}

	user, isNew, err := s.findOrCreateUser(ctx, phone, name)
	if err != nil {
		return nil, err
	}

	if err := s.assertAccountUsable(user); err != nil {
		return nil, err
	}

	tokens, err := s.issueTokens(ctx, user)
	if err != nil {
		return nil, err
	}

	return &dto.VerifyOTPResponse{
		User:      toUserResponse(user),
		Tokens:    *tokens,
		IsNewUser: isNew,
	}, nil
}

func (s *AuthService) findOrCreateUser(ctx context.Context, phone, name string) (*models.User, bool, error) {
	user, err := s.users.FindByPhone(ctx, phone)
	if err == nil {
		now := time.Now()
		if user.PhoneVerifiedAt == nil {
			user.PhoneVerifiedAt = &now
			if err := s.users.Update(ctx, user); err != nil {
				return nil, false, fmt.Errorf("service: failed to mark phone verified: %w", err)
			}
		}
		return user, false, nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return nil, false, fmt.Errorf("service: failed to look up user: %w", err)
	}

	now := time.Now()
	newUser := &models.User{
		Phone:           phone,
		Name:            name,
		Role:            models.RoleUser,
		Status:          models.StatusActive,
		PhoneVerifiedAt: &now,
	}
	if err := s.users.Create(ctx, newUser); err != nil {
		return nil, false, fmt.Errorf("service: failed to create user: %w", err)
	}
	return newUser, true, nil
}

func (s *AuthService) assertAccountUsable(user *models.User) error {
	switch user.Status {
	case models.StatusSuspended:
		return ErrAccountSuspended
	case models.StatusBanned:
		return ErrAccountBanned
	default:
		return nil
	}
}

// issueTokens mints a new access token and a new opaque refresh token
// (persisted hashed) for the given user.
func (s *AuthService) issueTokens(ctx context.Context, user *models.User) (*dto.AuthTokens, error) {
	accessToken, expiresAt, err := s.jwt.GenerateAccessToken(user)
	if err != nil {
		return nil, err
	}

	refreshToken, refreshExpiresAt, err := s.jwt.GenerateRefreshToken()
	if err != nil {
		return nil, err
	}

	record := &models.RefreshToken{
		UserID:    user.ID,
		TokenHash: utils.HashToken(refreshToken),
		ExpiresAt: refreshExpiresAt,
	}
	if err := s.tokens.Create(ctx, record); err != nil {
		return nil, fmt.Errorf("service: failed to persist refresh token: %w", err)
	}

	return &dto.AuthTokens{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    expiresAt.UTC().Format(time.RFC3339),
		TokenType:    "Bearer",
	}, nil
}

// RefreshToken validates a refresh token, revokes it, and issues a new
// token pair (refresh token rotation) — so a leaked, already-used refresh
// token cannot be replayed.
func (s *AuthService) RefreshToken(ctx context.Context, rawRefreshToken string) (*dto.AuthTokens, error) {
	hash := utils.HashToken(rawRefreshToken)

	record, err := s.tokens.FindByHash(ctx, hash)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrInvalidRefreshToken
		}
		return nil, fmt.Errorf("service: failed to load refresh token: %w", err)
	}
	if !record.IsValid() {
		return nil, ErrInvalidRefreshToken
	}

	user, err := s.users.FindByID(ctx, record.UserID)
	if err != nil {
		return nil, fmt.Errorf("service: failed to load user for refresh: %w", err)
	}
	if err := s.assertAccountUsable(user); err != nil {
		return nil, err
	}

	if err := s.tokens.Revoke(ctx, hash); err != nil {
		return nil, fmt.Errorf("service: failed to revoke used refresh token: %w", err)
	}

	return s.issueTokens(ctx, user)
}

// Logout revokes a single refresh token, signing the caller's current
// session out without affecting other devices.
func (s *AuthService) Logout(ctx context.Context, rawRefreshToken string) error {
	hash := utils.HashToken(rawRefreshToken)
	if err := s.tokens.Revoke(ctx, hash); err != nil {
		return fmt.Errorf("service: failed to revoke refresh token: %w", err)
	}
	return nil
}

// Me loads the full user profile for the authenticated caller.
func (s *AuthService) Me(ctx context.Context, userID string) (*dto.UserResponse, error) {
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrInvalidRefreshToken
		}
		return nil, err
	}
	resp := toUserResponse(user)
	return &resp, nil
}

// UpdateProfile applies the caller's requested changes to their own name
// and/or photo. Both fields are optional and independent — a nil field in
// the request leaves that column untouched, so a client can update just
// the photo without resending the name (and vice versa).
func (s *AuthService) UpdateProfile(ctx context.Context, userID string, req dto.UpdateProfileRequest) (*dto.UserResponse, error) {
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrInvalidRefreshToken
		}
		return nil, fmt.Errorf("service: failed to load user: %w", err)
	}

	if req.Name != nil {
		user.Name = *req.Name
	}
	if req.PhotoURL != nil {
		user.PhotoURL = *req.PhotoURL
	}

	if err := s.users.Update(ctx, user); err != nil {
		return nil, fmt.Errorf("service: failed to update profile: %w", err)
	}

	resp := toUserResponse(user)
	return &resp, nil
}

// DeleteAccount permanently ends the caller's session everywhere (all
// refresh tokens revoked) and soft-deletes the account. The phone number
// and name/photo are scrubbed first — a soft-deleted row otherwise still
// occupies the unique phone index (blocking that number from ever
// registering again) and still exposes PII to anything that reads it
// with Unscoped(). Trip/rating history referencing this user is left
// alone: the other party's records shouldn't disappear because this
// account did, and GORM's default soft-delete scoping already excludes
// the deleted user from every normal query and preload.
func (s *AuthService) DeleteAccount(ctx context.Context, userID string) error {
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("service: failed to load user: %w", err)
	}

	// Phone is varchar(20) — same width as a real E.164 number — so the
	// scrubbed value has to fit that, not just be unique. 18 hex chars
	// (72 bits) of the user's own UUID, minus its dashes, comfortably
	// avoids collisions within that budget.
	user.Phone = "d:" + strings.ReplaceAll(user.ID, "-", "")[:18]
	user.Name = "Deleted user"
	user.PhotoURL = ""
	if err := s.users.Update(ctx, user); err != nil {
		return fmt.Errorf("service: failed to scrub user before deletion: %w", err)
	}

	if err := s.users.Delete(ctx, userID); err != nil {
		return fmt.Errorf("service: failed to delete user: %w", err)
	}

	if err := s.tokens.RevokeAllForUser(ctx, userID); err != nil {
		return fmt.Errorf("service: failed to revoke sessions: %w", err)
	}

	return nil
}

func toUserResponse(user *models.User) dto.UserResponse {
	return dto.UserResponse{
		ID:            user.ID,
		Phone:         user.Phone,
		Name:          user.Name,
		PhotoURL:      user.PhotoURL,
		Role:          string(user.Role),
		Status:        string(user.Status),
		RatingAverage: user.RatingAverage,
		IsDriver:      user.DriverProfile != nil,
	}
}
