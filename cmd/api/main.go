// Command api runs the RideMatch HTTP API server.
package main

import (
	"log"

	"ridematch-backend/internal/config"
	"ridematch-backend/internal/database"
	"ridematch-backend/internal/email"
	"ridematch-backend/internal/flutterwave"
	"ridematch-backend/internal/handler"
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

	db, err := database.NewMySQL(cfg)
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

	authService := service.NewAuthService(
		userRepo, otpRepo, tokenRepo, smsSender, emailSender, jwtManager,
		cfg.OTPTTL, cfg.OTPLength, cfg.OTPResendCooldown, cfg.OTPMaxAttempts,
	)
	driverService := service.NewDriverService(
		driverRepo, userRepo, locationRepo,
		cfg.LocationStaleAfter, cfg.NearbyDefaultRadiusKM, cfg.NearbyMaxRadiusKM,
		cfg.NearbyDefaultLimit, cfg.NearbyCoordinatePrecision,
	)
	adminService := service.NewAdminService(driverRepo)

	fileStore, err := storage.New(cfg)
	if err != nil {
		log.Fatalf("main: %v", err)
	}

	flwClient := flutterwave.NewClient(cfg.FlutterwaveSecretKey)
	if cfg.FlutterwaveBaseURL != "" {
		flwClient = flwClient.WithBaseURL(cfg.FlutterwaveBaseURL)
	}
	var flwGateway flutterwave.Gateway = flwClient
	paymentService := service.NewPaymentService(
		paymentRepo, driverRepo, adminService, flwGateway,
		cfg.DriverSubDailyFee, cfg.FlutterwaveWebhookKey, cfg.FlutterwaveRedirectURL,
	)

	hub := ws.NewHub()
	tripService := service.NewTripService(
		tripRepo, tripOfferRepo, driverRepo, userRepo, tripRatingRepo, tripLocationRepo, locationRepo, hub,
		cfg.TripExpiryAfter, cfg.NearbyDefaultRadiusKM, cfg.NearbyDefaultLimit, cfg.NearbyCoordinatePrecision,
		cfg.LocationStaleAfter,
	)

	authHandler := handler.NewAuthHandler(authService)
	driverHandler := handler.NewDriverHandler(driverService)
	adminHandler := handler.NewAdminHandler(adminService)
	docsHandler := handler.NewDocsHandler()
	tripHandler := handler.NewTripHandler(tripService)
	wsHandler := handler.NewWSHandler(hub, jwtManager)
	paymentHandler := handler.NewPaymentHandler(paymentService)
	uploadHandler := handler.NewUploadHandler(fileStore)

	r := router.New(router.Dependencies{
		AuthHandler:    authHandler,
		DriverHandler:  driverHandler,
		AdminHandler:   adminHandler,
		DocsHandler:    docsHandler,
		TripHandler:    tripHandler,
		WSHandler:      wsHandler,
		PaymentHandler: paymentHandler,
		UploadHandler:  uploadHandler,
		JWTManager:     jwtManager,

		RedisClient:                   redisClient,
		RateLimitTripCreatePerMinute:  cfg.RateLimitTripCreatePerMinute,
		RateLimitOfferCreatePerMinute: cfg.RateLimitOfferCreatePerMinute,
	})

	addr := ":" + cfg.AppPort
	log.Printf("main: ridematch-api listening on %s (env=%s)", addr, cfg.AppEnv)
	log.Printf("main: API docs available at http://localhost%s/docs", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("main: server failed: %v", err)
	}
}
