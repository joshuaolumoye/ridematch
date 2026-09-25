package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"ridematch-backend/internal/dto"
	"ridematch-backend/internal/email"
	"ridematch-backend/internal/models"
	"ridematch-backend/internal/repository"
	"ridematch-backend/internal/sms"
	"ridematch-backend/internal/storage"
	"ridematch-backend/internal/utils"
)

// AuthService implements phone/email + OTP registration/login, and JWT
// access / opaque refresh token issuance and rotation.
type AuthService struct {
	users       repository.UserRepository
	otps        repository.OTPRepository
	tokens      repository.RefreshTokenRepository
	smsSender   sms.Sender
	emailSender email.Sender
	jwt         *utils.JWTManager
	fileStore   storage.Store
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
	smsSender sms.Sender,
	emailSender email.Sender,
	jwtManager *utils.JWTManager,
	fileStore storage.Store,
	otpTTL time.Duration,
	otpLength int,
	resendCooldown time.Duration,
	maxAttempts int,
) *AuthService {
	return &AuthService{
		users:       users,
		otps:        otps,
		tokens:      tokens,
		smsSender:   smsSender,
		fileStore:   fileStore,
		emailSender: emailSender,
		jwt:         jwtManager,
		otpTTL:      otpTTL,
		otpLen:      otpLength,
		cooldown:    resendCooldown,
		maxAttempts: maxAttempts,
	}
}

// RequestOTP normalizes the identifier (a Nigerian phone number or an
// email address — detected automatically), enforces the resend cooldown,
// generates and stores a hashed OTP code, and dispatches it over the
// matching channel (SMS or email).
func (s *AuthService) RequestOTP(ctx context.Context, rawIdentifier string) (*dto.RequestOTPResponse, error) {
	identifier, channel, err := utils.NormalizeIdentifier(rawIdentifier)
	if err != nil {
		return nil, err
	}

	if last, err := s.otps.FindLatestAny(ctx, identifier, models.OTPPurposeLogin); err == nil {
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

	otpChannel := models.OTPChannelPhone
	if channel == utils.ChannelEmail {
		otpChannel = models.OTPChannelEmail
	}

	otp := &models.OTPRequest{
		Identifier:  identifier,
		Channel:     otpChannel,
		CodeHash:    hash,
		Purpose:     models.OTPPurposeLogin,
		ExpiresAt:   time.Now().Add(s.otpTTL),
		MaxAttempts: s.maxAttempts,
	}
	if err := s.otps.Create(ctx, otp); err != nil {
		return nil, fmt.Errorf("service: failed to persist otp: %w", err)
	}

	if err := s.dispatchOTP(ctx, identifier, channel, code); err != nil {
		return nil, err
	}

	return &dto.RequestOTPResponse{
		Identifier:       identifier,
		Channel:          string(channel),
		ExpiresInSeconds: int(s.otpTTL.Seconds()),
		ResendInSeconds:  int(s.cooldown.Seconds()),
	}, nil
}

// dispatchOTP sends the code over the right channel.
func (s *AuthService) dispatchOTP(ctx context.Context, identifier string, channel utils.Channel, code string) error {
	if channel == utils.ChannelEmail {
		subject, body := email.BuildOTPEmail(code, int(s.otpTTL.Minutes()))
		if err := s.emailSender.Send(ctx, identifier, subject, body); err != nil {
			return fmt.Errorf("service: failed to send otp email: %w", err)
		}
		return nil
	}

	message := sms.BuildOTPMessage(code, int(s.otpTTL.Minutes()))
	if err := s.smsSender.Send(ctx, identifier, message); err != nil {
		return fmt.Errorf("service: failed to send otp sms: %w", err)
	}
	return nil
}

// VerifyOTP validates a submitted code against the most recent active
// challenge for the identifier. On success it finds-or-creates the User
// account and issues a fresh access/refresh token pair.
func (s *AuthService) VerifyOTP(ctx context.Context, rawIdentifier, code, name string) (*dto.VerifyOTPResponse, error) {
	identifier, channel, err := utils.NormalizeIdentifier(rawIdentifier)
	if err != nil {
		return nil, err
	}

	otp, err := s.otps.FindLatestActive(ctx, identifier, models.OTPPurposeLogin)
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

	user, isNew, err := s.findOrCreateUser(ctx, channel, identifier, name)
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
		User:      s.toUserResponse(ctx, user),
		Tokens:    *tokens,
		IsNewUser: isNew,
	}, nil
}

func (s *AuthService) findOrCreateUser(ctx context.Context, channel utils.Channel, identifier, name string) (*models.User, bool, error) {
	if channel == utils.ChannelEmail {
		return s.findOrCreateUserByEmail(ctx, identifier, name)
	}
	return s.findOrCreateUserByPhone(ctx, identifier, name)
}

func (s *AuthService) findOrCreateUserByPhone(ctx context.Context, phone, name string) (*models.User, bool, error) {
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

func (s *AuthService) findOrCreateUserByEmail(ctx context.Context, emailAddr, name string) (*models.User, bool, error) {
	user, err := s.users.FindByEmail(ctx, emailAddr)
	if err == nil {
		now := time.Now()
		if user.EmailVerifiedAt == nil {
			user.EmailVerifiedAt = &now
			if err := s.users.Update(ctx, user); err != nil {
				return nil, false, fmt.Errorf("service: failed to mark email verified: %w", err)
			}
		}
		return user, false, nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return nil, false, fmt.Errorf("service: failed to look up user: %w", err)
	}

	now := time.Now()
	newUser := &models.User{
		Email:           emailAddr,
		Name:            name,
		Role:            models.RoleUser,
		Status:          models.StatusActive,
		EmailVerifiedAt: &now,
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
	resp := s.toUserResponse(ctx, user)
	return &resp, nil
}

// UpdateProfile applies the caller's requested changes to their own name
// and/or photo. Both fields are optional and independent — a nil field in
// the request leaves that column untouched (so a client can update just
// the photo without resending the name, and vice versa).
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

	resp := s.toUserResponse(ctx, user)
	return &resp, nil
}

// RegisterPushToken stores (or clears, if empty) the caller's Expo push
// token, so admin/system notifications can reach their phone's
// notification tray in addition to the in-app notifications list. Called
// by the app once after login/permission grant, and again whenever the
// token rotates (Expo can reissue tokens).
func (s *AuthService) RegisterPushToken(ctx context.Context, userID, expoPushToken string) error {
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrInvalidRefreshToken
		}
		return fmt.Errorf("service: failed to load user: %w", err)
	}

	user.PushToken = expoPushToken
	if err := s.users.Update(ctx, user); err != nil {
		return fmt.Errorf("service: failed to save push token: %w", err)
	}
	return nil
}

// DeleteAccount permanently ends the caller's session everywhere (all
// refresh tokens revoked) and soft-deletes the account. The identifying
// fields (phone, email, name/photo) are scrubbed first — a soft-deleted
// row otherwise still occupies those lookup indexes (blocking that phone
// number or email from ever registering again) and still exposes PII to
// anything that reads it with Unscoped(). Trip/rating history referencing
// this user is left alone: the other party's records shouldn't disappear
// because this account did, and GORM's default soft-delete scoping
// already excludes the deleted user from every normal query and preload.
func (s *AuthService) DeleteAccount(ctx context.Context, userID string) error {
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("service: failed to load user: %w", err)
	}

	// Both scrubbed values need to be unique across every other
	// soft-deleted account too (see the comment on models.User about why
	// Phone/Email aren't DB-unique) — derived from the user's own UUID,
	// which already is. Phone is varchar(20) — same width as a real
	// E.164 number — so only 18 hex chars (72 bits) fit; that comfortably
	// avoids collisions within that budget. Email has room to spare.
	scrubID := strings.ReplaceAll(user.ID, "-", "")
	if user.Phone != "" {
		user.Phone = "d:" + scrubID[:18]
	}
	if user.Email != "" {
		user.Email = "deleted+" + scrubID + "@ridematch.invalid"
	}
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

func (s *AuthService) toUserResponse(ctx context.Context, user *models.User) dto.UserResponse {
	return dto.UserResponse{
		ID:            user.ID,
		Phone:         user.Phone,
		Email:         user.Email,
		Name:          user.Name,
		PhotoURL:      signURL(ctx, s.fileStore, user.PhotoURL),
		Role:          string(user.Role),
		Status:        string(user.Status),
		RatingAverage: user.RatingAverage,
		IsDriver:      user.DriverProfile != nil,
	}
}
