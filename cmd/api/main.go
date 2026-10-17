// Command api runs the MakeWeRide HTTP API server.
package main

import (
	"context"
	"log"
	"time"

	"github.com/redis/go-redis/v9"

	"ridematch-backend/internal/config"
	"ridematch-backend/internal/database"
	"ridematch-backend/internal/email"
	"ridematch-backend/internal/flutterwave"
	"ridematch-backend/internal/handler"
	"ridematch-backend/internal/push"
	"ridematch-backend/internal/repository"
	"ridematch-backend/internal/router"
	"ridematch-backend/internal/service"
	"ridematch-backend/internal/sms"
	"ridematch-backend/internal/storage"
	"ridematch-backend/internal/utils"
	"ridematch-backend/internal/ws"
)

func main() {
	cfg := config.Load()

	db, err := database.NewDB(cfg)
	if err != nil {
		log.Fatalf("main: %v", err)
	}

	redisClient, err := database.NewRedis(cfg)
	if err != nil {
		log.Fatalf("main: %v", err)
	}

	// Repositories
	userRepo := repository.NewUserRepository(db)
	otpRepo := repository.NewOTPRepository(db)
	driverRepo := repository.NewDriverRepository(db)
	tokenRepo := repository.NewRefreshTokenRepository(db)
	locationRepo := repository.NewLocationRepository(redisClient)
	tripRepo := repository.NewTripRepository(db)
	tripOfferRepo := repository.NewTripOfferRepository(db)
	tripLocationRepo := repository.NewTripLocationRepository(redisClient)
	paymentRepo := repository.NewPaymentRepository(db)
	tripRatingRepo := repository.NewTripRatingRepository(db)
	subscriptionPriceRepo := repository.NewSubscriptionPriceRepository(db)
	notificationRepo := repository.NewNotificationRepository(db)
	vehicleTypeConfigRepo := repository.NewVehicleTypeConfigRepository(db)
	subscriptionPlanRepo := repository.NewSubscriptionPlanRepository(db)
	documentTypeRepo := repository.NewDocumentTypeRepository(db)
	driverDocumentRepo := repository.NewDriverDocumentRepository(db)
	vehicleCatalogRepo := repository.NewVehicleCatalogRepository(db)
	ledgerRepo := repository.NewLedgerRepository(db)
	payoutAccountRepo := repository.NewPayoutAccountRepository(db)
	kinRepo := repository.NewKinRepository(db)
	auditRepo := repository.NewAuditRepository(db)
	platformSettingRepo := repository.NewPlatformSettingRepository(db)
	trackingRepo := repository.NewTrackingRepository(db)

	// SMS provider — console logging by default, Termii in production
	// once SMS_PROVIDER=termii and TERMII_API_KEY are set.
	var smsSender sms.Sender
	switch cfg.SMSProvider {
	case "termii":
		smsSender = sms.NewTermiiSender(cfg.TermiiAPIKey, cfg.TermiiSenderID)
	default:
		smsSender = sms.NewConsoleSender()
	}

	// Email provider — console logging by default, real SMTP delivery
	// (Hostinger or any other mailbox) once EMAIL_PROVIDER=smtp and the
	// SMTP_* settings are configured.
	var emailSender email.Sender
	switch cfg.EmailProvider {
	case "smtp":
		emailSender = email.NewSMTPSender(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUsername, cfg.SMTPPassword, cfg.SMTPFromAddress, cfg.SMTPFromName)
	default:
		emailSender = email.NewConsoleSender()
	}

	jwtManager := utils.NewJWTManager(cfg.JWTAccessSecret, cfg.JWTAccessTTL, cfg.JWTRefreshTTL)

	fileStore, err := storage.New(cfg)
	if err != nil {
		log.Fatalf("main: %v", err)
	}

	// Push provider — console logging by default, real Expo push delivery
	// once PUSH_PROVIDER=expo (EXPO_PUSH_ACCESS_TOKEN is optional even
	// then, only needed if Expo's enhanced security is enabled).
	var pushSender push.Sender
	switch cfg.PushProvider {
	case "expo":
		pushSender = push.NewExpoSender(cfg.ExpoPushAccessToken)
	default:
		pushSender = push.NewConsoleSender()
	}

	hub := ws.NewHub()

	authService := service.NewAuthService(
		userRepo, otpRepo, tokenRepo, smsSender, emailSender, jwtManager, fileStore,
		cfg.OTPTTL, cfg.OTPLength, cfg.OTPResendCooldown, cfg.OTPMaxAttempts,
	)
	settingsService := service.NewSettingsService(vehicleTypeConfigRepo, subscriptionPlanRepo, documentTypeRepo, vehicleCatalogRepo)
	driverService := service.NewDriverService(
		driverRepo, userRepo, locationRepo, driverDocumentRepo, settingsService, fileStore,
		cfg.LocationStaleAfter, cfg.NearbyDefaultRadiusKM, cfg.NearbyMaxRadiusKM,
		cfg.NearbyDefaultLimit, cfg.NearbyCoordinatePrecision,
	)
	notificationService := service.NewNotificationService(notificationRepo, userRepo, hub, pushSender)
	adminService := service.NewAdminService(
		driverRepo, userRepo, tripRepo, locationRepo, subscriptionPriceRepo, driverDocumentRepo, fileStore, smsSender, emailSender,
		notificationService, cfg.LocationStaleAfter,
	)

	flwClient := flutterwave.NewClient(cfg.FlutterwaveSecretKey)
	if cfg.FlutterwaveBaseURL != "" {
		flwClient = flwClient.WithBaseURL(cfg.FlutterwaveBaseURL)
	}
	var flwGateway flutterwave.Gateway = flwClient
	paymentService := service.NewPaymentService(
		paymentRepo, driverRepo, settingsService, adminService, flwGateway,
		cfg.FlutterwaveWebhookKey, cfg.FlutterwaveRedirectURL,
	)

	tripService := service.NewTripService(
		tripRepo, tripOfferRepo, driverRepo, userRepo, tripRatingRepo, tripLocationRepo, locationRepo, hub, fileStore,
		cfg.TripExpiryAfter, cfg.NearbyDefaultRadiusKM, cfg.NearbyDefaultLimit, cfg.NearbyCoordinatePrecision,
		cfg.LocationStaleAfter, cfg.JWTAccessSecret, cfg.AppPublicURL,
	)

	// ---- tracking, wallet, escrow, payouts, next of kin ----
	auditService := service.NewAuditService(auditRepo)
	trackingService := service.NewTrackingService(
		trackingRepo, driverRepo, tripRepo, auditService, cfg.TrackingHMACSecret, cfg.ShiftStaleAfter, cfg.TrackingRetention,
	)
	driverService.SetTracking(trackingService)

	stepUpService := service.NewStepUpService(userRepo, otpRepo, smsSender, emailSender, cfg.OTPTTL, cfg.OTPLength, cfg.OTPResendCooldown, cfg.OTPMaxAttempts)
	var walletGateway flutterwave.WalletGateway = flwClient
	walletService := service.NewWalletService(
		ledgerRepo, payoutAccountRepo, userRepo, driverRepo, platformSettingRepo, walletGateway, stepUpService,
		notificationService, auditService, service.WalletConfig{
			TopupFeeBps: cfg.WalletTopupFeeBps, TopupMinKobo: cfg.WalletTopupMinKobo, TopupMaxKobo: cfg.WalletTopupMaxKobo,
			TopupTTL: cfg.WalletTopupTTL, PayoutsEnabled: cfg.PayoutsEnabled, PayoutMinKobo: cfg.PayoutMinKobo,
			PayoutDailyLimit: cfg.PayoutDailyLimitKobo, PayoutCooldown: cfg.PayoutCooldown, PayoutCooldownCap: cfg.PayoutCooldownCap,
			PayoutReviewKobo: cfg.PayoutReviewKobo, PINMaxAttempts: cfg.PINMaxAttempts, PINLockDuration: cfg.PINLockDuration,
		},
	)
	escrowService := service.NewEscrowService(ledgerRepo, userRepo, tripRepo, notificationService, auditService, cfg.EscrowRequired, cfg.EscrowAutoRelease)
	tripService.SetEscrow(escrowService)
	kinService := service.NewKinService(
		kinRepo, ledgerRepo, userRepo, driverRepo, walletService, stepUpService, notificationService, auditService,
		walletGateway, smsSender, emailSender, fileStore, cfg.AppealCoolingOff, cfg.AppealTokenTTL,
	)

	authHandler := handler.NewAuthHandler(authService)
	driverHandler := handler.NewDriverHandler(driverService)
	adminHandler := handler.NewAdminHandler(adminService, settingsService)
	metaHandler := handler.NewMetaHandler(settingsService)
	docsHandler := handler.NewDocsHandler()
	tripHandler := handler.NewTripHandler(tripService)
	publicTrackingHandler := handler.NewPublicTrackingHandler(tripService)
	wsHandler := handler.NewWSHandler(hub, jwtManager)
	paymentHandler := handler.NewPaymentHandler(paymentService)
	paymentHandler.SetWallet(walletService)
	walletHandler := handler.NewWalletHandler(walletService, escrowService)
	trackingHandler := handler.NewTrackingHandler(trackingService)
	financeAdminHandler := handler.NewFinanceAdminHandler(walletService, escrowService, auditService)
	kinHandler := handler.NewKinHandler(kinService)
	uploadHandler := handler.NewUploadHandler(fileStore)
	notificationHandler := handler.NewNotificationHandler(notificationService)

	r := router.New(router.Dependencies{
		AuthHandler:           authHandler,
		DriverHandler:         driverHandler,
		AdminHandler:          adminHandler,
		MetaHandler:           metaHandler,
		DocsHandler:           docsHandler,
		TripHandler:           tripHandler,
		PublicTrackingHandler: publicTrackingHandler,
		WSHandler:             wsHandler,
		PaymentHandler:        paymentHandler,
		UploadHandler:         uploadHandler,
		NotificationHandler:   notificationHandler,
		WalletHandler:         walletHandler,
		TrackingHandler:       trackingHandler,
		FinanceAdminHandler:   financeAdminHandler,
		KinHandler:            kinHandler,
		JWTManager:            jwtManager,

		RedisClient:                   redisClient,
		RateLimitTripCreatePerMinute:  cfg.RateLimitTripCreatePerMinute,
		RateLimitOfferCreatePerMinute: cfg.RateLimitOfferCreatePerMinute,
		TrustedProxies:                cfg.TrustedProxies,
	})

	// Background jobs. Every one is idempotent (compare-and-swap /
	// unique-key guarded), so running several API instances is safe; the
	// Redis lock only avoids redundant work.
	workerCtx := context.Background()
	runEvery(workerCtx, redisClient, "withdrawals", 20*time.Second, walletService.PollWithdrawals)
	runEvery(workerCtx, redisClient, "escrow-release", 30*time.Second, escrowService.ReleaseDue)
	runEvery(workerCtx, redisClient, "stale-shifts", time.Minute, trackingService.CloseStaleShifts)
	runEvery(workerCtx, redisClient, "expire-topups", 5*time.Minute, walletService.ExpireTopups)
	runEvery(workerCtx, redisClient, "appeal-disburse", time.Minute, kinService.DisburseDue)
	runEvery(workerCtx, redisClient, "tracking-purge", 6*time.Hour, trackingService.Purge)

	addr := ":" + cfg.AppPort
	log.Printf("main: ridematch-api listening on %s (env=%s)", addr, cfg.AppEnv)
	log.Printf("main: API docs available at http://localhost%s/docs", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("main: server failed: %v", err)
	}
}

// runEvery runs fn on an interval in the background. A short-lived Redis lock
// per tick keeps multiple API instances from doing the same sweep at once;
// if Redis is unavailable the job simply runs (the jobs are idempotent).
func runEvery(ctx context.Context, rdb *redis.Client, name string, every time.Duration, fn func(context.Context)) {
	go func() {
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for range ticker.C {
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("worker %s: panic: %v", name, r)
					}
				}()
				lockTTL := every - time.Second
				if lockTTL < time.Second {
					lockTTL = time.Second
				}
				lctx, cancel := context.WithTimeout(ctx, 2*time.Second)
				ok, err := rdb.SetNX(lctx, "worker-lock:"+name, "1", lockTTL).Result()
				cancel()
				if err == nil && !ok {
					return
				}
				jctx, jcancel := context.WithTimeout(ctx, 5*time.Minute)
				defer jcancel()
				fn(jctx)
			}()
		}
	}()
}
