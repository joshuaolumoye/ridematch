// Package router wires up the gin engine: global middleware, route
// groups, and versioning.
package router

import (
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"ridematch-backend/internal/handler"
	"ridematch-backend/internal/middleware"
	"ridematch-backend/internal/utils"
)

// Dependencies bundles everything the router needs to register routes.
// Passed as a single struct so adding a new handler later doesn't change
// New's signature.
type Dependencies struct {
	AuthHandler           *handler.AuthHandler
	DriverHandler         *handler.DriverHandler
	AdminHandler          *handler.AdminHandler
	MetaHandler           *handler.MetaHandler
	DocsHandler           *handler.DocsHandler
	TripHandler           *handler.TripHandler
	PublicTrackingHandler *handler.PublicTrackingHandler
	WSHandler             *handler.WSHandler
	PaymentHandler        *handler.PaymentHandler
	UploadHandler         *handler.UploadHandler
	NotificationHandler   *handler.NotificationHandler
	WalletHandler         *handler.WalletHandler
	TrackingHandler       *handler.TrackingHandler
	FinanceAdminHandler   *handler.FinanceAdminHandler
	KinHandler            *handler.KinHandler
	JWTManager            *utils.JWTManager

	// RedisClient backs the rate limiter middleware on the
	// trip-creation and offer-creation routes.
	RedisClient *redis.Client

	RateLimitTripCreatePerMinute  int
	RateLimitOfferCreatePerMinute int

	// TrustedProxies are the IPs/CIDRs of reverse proxies in front of
	// this API that are trusted to set X-Forwarded-For/X-Real-IP
	// honestly — see config.Config.TrustedProxies. Nil/empty means
	// trust none (Gin falls back to the direct connection's IP, which
	// is correct when nothing proxies traffic to this process, and
	// safe-but-wrong — it'll see the proxy's IP for every request — when
	// something does and this wasn't set).
	TrustedProxies []string
}

// New builds and returns a fully configured gin.Engine.
func New(deps Dependencies) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), middleware.CORS())

	// Must be set explicitly: an unset/empty list makes Gin trust NO
	// proxy (c.ClientIP() always uses the direct connection), rather
	// than gin.New()'s own default of trusting every proxy — which lets
	// any client spoof X-Forwarded-For and defeat RateLimitByIP.
	if err := r.SetTrustedProxies(deps.TrustedProxies); err != nil {
		log.Printf("router: invalid TRUSTED_PROXIES entry, trusting no proxies: %v", err)
		_ = r.SetTrustedProxies(nil)
	}

	r.GET("/health", func(c *gin.Context) {
		utils.Success(c, http.StatusOK, "ridematch-api is healthy", gin.H{"status": "ok"})
	})

	// Some platforms' health/uptime checks probe "/" (often with HEAD)
	// rather than "/health" before routing traffic to this instance.
	// Without a handler here that's a 404 on every check — harmless, but
	// noisy in the logs and occasionally mistaken by the platform for an
	// unhealthy instance. GET and HEAD both land here; gin.New() doesn't
	// add Gin's default HEAD-mirrors-GET behavior on its own.
	rootOK := func(c *gin.Context) {
		utils.Success(c, http.StatusOK, "MakeWeRide API", gin.H{"status": "ok"})
	}
	r.GET("/", rootOK)
	r.HEAD("/", rootOK)

	r.GET("/docs", deps.DocsHandler.UI)
	r.GET("/docs/openapi.json", deps.DocsHandler.Spec)

	// The "someone shared their live location with you" page — no auth,
	// by design (see PublicTrackingHandler's doc comment). Its own JSON
	// polling endpoint lives under /api/v1/track/:token below, alongside
	// everything else versioned, rather than here at root.
	r.GET("/t/:token", deps.PublicTrackingHandler.View)

	// Serves whatever POST /api/v1/uploads just wrote (under
	// STORAGE_DRIVER=local — a no-op, unused route under STORAGE_DRIVER=r2,
	// where files are served from R2's own public URL instead).
	r.Static("/uploads", "./uploads")

	// Public webhook — no auth middleware. Protected instead by the
	// verif-hash header check + server-side transaction verification
	// inside PaymentService.HandleWebhook (see that file's doc comment).
	r.POST("/webhooks/flutterwave", deps.PaymentHandler.FlutterwaveWebhook)

	rl := func(action string, perMinute int) gin.HandlerFunc {
		return middleware.RateLimit(deps.RedisClient, action, perMinute, time.Minute)
	}

	v1 := r.Group("/api/v1")
	{
		auth := v1.Group("/auth")
		{
			auth.POST("/otp/request", deps.AuthHandler.RequestOTP)
			auth.POST("/otp/verify", deps.AuthHandler.VerifyOTP)
			auth.POST("/token/refresh", deps.AuthHandler.RefreshToken)
			auth.POST("/logout", deps.AuthHandler.Logout)
		}

		// JSON status feed for the public /t/:token page above — same
		// no-auth reasoning, kept under /api/v1 so it sits alongside every
		// other JSON endpoint rather than needing its own base path.
		v1.GET("/track/:token", deps.PublicTrackingHandler.Status)

		authRequired := middleware.RequireAuth(deps.JWTManager)

		users := v1.Group("/users")
		users.Use(authRequired)
		{
			users.GET("/me", deps.AuthHandler.Me)
			users.PATCH("/me", deps.AuthHandler.UpdateProfile)
			users.PATCH("/me/push-token", deps.AuthHandler.RegisterPushToken)
			users.DELETE("/me", deps.AuthHandler.DeleteAccount)
		}

		// Notifications inbox — every authenticated account (rider, driver,
		// or admin) sees only its own notifications, scoped by JWT user ID
		// inside the handler/service, not by a route param.
		notifications := v1.Group("/notifications")
		notifications.Use(authRequired)
		{
			notifications.GET("", deps.NotificationHandler.List)
			notifications.GET("/unread-count", deps.NotificationHandler.UnreadCount)
			notifications.PATCH("/:id/read", deps.NotificationHandler.MarkRead)
			notifications.POST("/read-all", deps.NotificationHandler.MarkAllRead)
		}

		v1.POST("/uploads", authRequired, deps.UploadHandler.Create)

		// Admin-configured reference data every account needs before (or
		// instead of) registering as a driver: which vehicle types are
		// currently accepting drivers, and which documents a given
		// vehicle type requires. Plain authRequired, not an admin check —
		// this is what drives the registration and booking screens.
		meta := v1.Group("/meta")
		meta.Use(authRequired)
		{
			meta.GET("/vehicle-types", deps.MetaHandler.VehicleTypes)
			meta.GET("/document-types", deps.MetaHandler.DocumentTypes)
			meta.GET("/vehicle-catalog", deps.MetaHandler.VehicleCatalog)
		}

		// Driver-facing endpoints: any authenticated account may register
		// as a driver, so these sit behind auth only, not a role check —
		// registration is what grants the "driver" role in the first
		// place. Business-rule gates (verified? subscribed?) are enforced
		// in the service layer against DB state, not the JWT role claim.
		driver := v1.Group("/driver")
		driver.Use(authRequired)
		{
			driver.POST("/register", deps.DriverHandler.Register)
			driver.GET("/profile", deps.DriverHandler.Me)
			driver.POST("/online", deps.DriverHandler.GoOnline)
			driver.POST("/offline", deps.DriverHandler.GoOffline)
			driver.POST("/location", deps.DriverHandler.PingLocation)
			driver.GET("/subscription/plans", deps.PaymentHandler.ListPlans)
			driver.POST("/subscription/checkout", deps.PaymentHandler.InitiateCheckout)
			driver.GET("/trips/active", deps.TripHandler.DriverActive)
			driver.GET("/trips/history", deps.TripHandler.DriverHistory)
			driver.GET("/earnings", deps.TripHandler.Earnings)

			// GPS evidence: the app uploads buffered location samples for its
			// current shift (the shift_id comes back from POST /driver/online).
			driver.POST("/tracking/points",
				middleware.RateLimit(deps.RedisClient, "tracking.points", 90, time.Minute),
				deps.TrackingHandler.Ingest)

			// Next of kin (drivers only; the service enforces it) and the
			// driver's view of appeals filed in their name.
			driver.GET("/next-of-kin", deps.KinHandler.Get)
			driver.POST("/next-of-kin/code", rl("nok.code", 5), deps.KinHandler.Code)
			driver.PUT("/next-of-kin", rl("nok.save", 10), deps.KinHandler.Save)
			driver.GET("/appeals", deps.KinHandler.MyAppeals)
			driver.POST("/appeals/:id/cancel", rl("appeal.cancel", 10), deps.KinHandler.CancelAppeal)
		}

		// Wallet: every account has one. Funding and balances are for
		// everyone; PIN, bank accounts and withdrawals are drivers only
		// (enforced in WalletService, not just by route).
		wallet := v1.Group("/wallet")
		wallet.Use(authRequired)
		{
			wallet.GET("", deps.WalletHandler.Get)
			wallet.GET("/transactions", deps.WalletHandler.Transactions)
			wallet.GET("/topups/quote", deps.WalletHandler.TopupQuote)
			wallet.POST("/topups", rl("wallet.topup", 10), deps.WalletHandler.CreateTopup)
			wallet.GET("/topups", deps.WalletHandler.ListTopups)
			wallet.GET("/topups/:id", rl("wallet.topup.get", 60), deps.WalletHandler.GetTopup)
			wallet.GET("/banks", deps.WalletHandler.Banks)

			wallet.POST("/pin/code", rl("wallet.pin.code", 5), deps.WalletHandler.PINCode)
			wallet.PUT("/pin", rl("wallet.pin.set", 10), deps.WalletHandler.SetPIN)

			wallet.POST("/payout-accounts/resolve", rl("wallet.resolve", 20), deps.WalletHandler.ResolveAccount)
			wallet.POST("/payout-accounts/code", rl("wallet.acct.code", 5), deps.WalletHandler.PayoutAccountCode)
			wallet.POST("/payout-accounts", rl("wallet.acct.add", 10), deps.WalletHandler.AddPayoutAccount)
			wallet.GET("/payout-accounts", deps.WalletHandler.ListPayoutAccounts)
			wallet.POST("/payout-accounts/:id/remove", rl("wallet.acct.remove", 10), deps.WalletHandler.RemovePayoutAccount)

			wallet.GET("/withdrawals/quote", deps.WalletHandler.WithdrawQuote)
			wallet.POST("/withdrawals", rl("wallet.withdraw", 10), deps.WalletHandler.Withdraw)
			wallet.GET("/withdrawals", deps.WalletHandler.ListWithdrawals)
		}

		// Next-of-kin appeals filed by someone WITHOUT an account. Heavily
		// rate-limited per IP; every answer is deliberately uniform so these
		// routes can't be used to discover who is a driver.
		pub := v1.Group("/public/appeals")
		{
			pub.POST("/code", deps.ipLimit("appeal.code", 5, time.Hour), deps.KinHandler.PublicCode)
			pub.POST("", deps.ipLimit("appeal.submit", 5, time.Hour), deps.KinHandler.PublicSubmit)
			pub.GET("/status", deps.ipLimit("appeal.status", 60, time.Hour), deps.KinHandler.PublicStatus)
			pub.POST("/documents", deps.ipLimit("appeal.docs", 20, time.Hour), deps.KinHandler.PublicUpload)
		}

		// Passenger-facing: browsing nearby drivers requires being logged
		// in (any role) but nothing driver-specific.
		v1.GET("/drivers/nearby", authRequired, deps.DriverHandler.FindNearby)

		admin := v1.Group("/admin")
		admin.Use(authRequired, middleware.RequireRole("admin"))
		{
			admin.GET("/overview", deps.AdminHandler.Overview)

			admin.GET("/drivers", deps.AdminHandler.ListDrivers)
			admin.GET("/drivers/locations", deps.AdminHandler.DriverLocations)
			admin.GET("/drivers/:id", deps.AdminHandler.GetDriver)
			admin.PATCH("/drivers/:id/verify", deps.AdminHandler.VerifyDriver)
			admin.PATCH("/drivers/:id/subscription", deps.AdminHandler.ActivateSubscription)
			admin.POST("/drivers/:id/notify", deps.AdminHandler.NotifyDriver)

			admin.GET("/users", deps.AdminHandler.ListUsers)
			admin.GET("/users/:id", deps.AdminHandler.GetUser)
			admin.GET("/users/:id/trips", deps.AdminHandler.UserTrips)
			admin.PATCH("/users/:id/status", deps.AdminHandler.UpdateAccountStatus)

			admin.GET("/trips", deps.AdminHandler.ListTrips)
			admin.GET("/trips/:id", deps.AdminHandler.GetTrip)
			admin.GET("/trips/:id/route", deps.TrackingHandler.AdminTripRoute)

			// GPS evidence.
			admin.GET("/tracking/shifts", deps.TrackingHandler.AdminShifts)
			admin.GET("/tracking/shifts/:id", deps.TrackingHandler.AdminShift)
			admin.GET("/tracking/shifts/:id/export", deps.TrackingHandler.AdminExportShift)

			// Money.
			admin.GET("/finance/overview", deps.FinanceAdminHandler.Overview)
			admin.GET("/finance/withdrawals", deps.FinanceAdminHandler.Withdrawals)
			admin.POST("/finance/withdrawals/:id/decision", deps.FinanceAdminHandler.DecideWithdrawal)
			admin.GET("/finance/payout-accounts", deps.FinanceAdminHandler.PayoutAccounts)
			admin.POST("/finance/payout-accounts/:id/decision", deps.FinanceAdminHandler.DecidePayoutAccount)
			admin.GET("/finance/escrow", deps.FinanceAdminHandler.Escrow)
			admin.POST("/finance/escrow/:tripId/resolve", deps.FinanceAdminHandler.ResolveEscrow)
			admin.GET("/finance/wallets/:userId", deps.FinanceAdminHandler.Wallet)
			admin.GET("/finance/wallets/:userId/transactions", deps.FinanceAdminHandler.WalletTransactions)
			admin.POST("/finance/wallets/:userId/freeze", deps.FinanceAdminHandler.FreezeWallet)
			admin.POST("/finance/payouts-switch", deps.FinanceAdminHandler.PayoutSwitch)
			admin.GET("/audit-logs", deps.FinanceAdminHandler.AuditLogs)

			// Next-of-kin appeals.
			admin.GET("/appeals", deps.KinHandler.AdminList)
			admin.GET("/appeals/:id", deps.KinHandler.AdminGet)
			admin.POST("/appeals/:id/decision", deps.KinHandler.AdminDecide)

			admin.GET("/settings/subscription-prices", deps.AdminHandler.ListSubscriptionPrices)
			admin.PUT("/settings/subscription-prices/:vehicle_type", deps.AdminHandler.UpdateSubscriptionPrice)

			admin.GET("/settings/vehicle-types", deps.AdminHandler.ListVehicleTypes)
			admin.PUT("/settings/vehicle-types/:vehicle_type", deps.AdminHandler.UpdateVehicleType)

			admin.GET("/settings/subscription-plans", deps.AdminHandler.ListSubscriptionPlans)
			admin.PUT("/settings/subscription-plans/:vehicle_type/:billing_cycle", deps.AdminHandler.UpdateSubscriptionPlan)

			admin.GET("/settings/document-types", deps.AdminHandler.ListDocumentTypes)
			admin.POST("/settings/document-types", deps.AdminHandler.CreateDocumentType)
			admin.PUT("/settings/document-types/:id", deps.AdminHandler.UpdateDocumentType)
			admin.DELETE("/settings/document-types/:id", deps.AdminHandler.DeleteDocumentType)

			admin.GET("/vehicle-catalog/makes", deps.AdminHandler.ListVehicleMakes)
			admin.POST("/vehicle-catalog/makes", deps.AdminHandler.CreateVehicleMake)
			admin.PUT("/vehicle-catalog/makes/:id", deps.AdminHandler.UpdateVehicleMake)
			admin.DELETE("/vehicle-catalog/makes/:id", deps.AdminHandler.DeleteVehicleMake)
			admin.POST("/vehicle-catalog/models", deps.AdminHandler.CreateVehicleModel)
			admin.PUT("/vehicle-catalog/models/:id", deps.AdminHandler.UpdateVehicleModel)
			admin.DELETE("/vehicle-catalog/models/:id", deps.AdminHandler.DeleteVehicleModel)
			admin.POST("/vehicle-catalog/bulk", deps.AdminHandler.BulkImportVehicleCatalog)
		}

		// Trip lifecycle: request → negotiate → match → pickup PIN →
		// complete. Trip-creation and offer-creation are rate-limited per
		// user since they're the two write paths a misbehaving client
		// could hammer.
		trips := v1.Group("/trips")
		trips.Use(authRequired)
		{
			trips.POST("",
				middleware.RateLimit(deps.RedisClient, "trip.create", deps.RateLimitTripCreatePerMinute, time.Minute),
				deps.TripHandler.Create)
			trips.GET("/active", deps.TripHandler.MyActive)
			trips.GET("/history", deps.TripHandler.History)
			trips.GET("/nearby", deps.TripHandler.Nearby)
			trips.GET("/:id", deps.TripHandler.Get)
			trips.POST("/:id/share-location", deps.TripHandler.ShareLocation)
			trips.POST("/:id/offers",
				middleware.RateLimit(deps.RedisClient, "trip.offer", deps.RateLimitOfferCreatePerMinute, time.Minute),
				deps.TripHandler.MakeOffer)
			trips.GET("/:id/offers", deps.TripHandler.ListOffers)
			trips.POST("/:id/offers/:offerId/accept", deps.TripHandler.AcceptOffer)
			trips.POST("/:id/offers/:offerId/reject", deps.TripHandler.RejectOffer)
			trips.POST("/:id/confirm-pickup", deps.TripHandler.ConfirmPickup)
			trips.POST("/:id/complete", deps.TripHandler.Complete)
			trips.POST("/:id/cancel", deps.TripHandler.Cancel)
			trips.POST("/:id/rate", deps.TripHandler.Rate)
			trips.GET("/:id/payment", deps.WalletHandler.TripPayment)
			trips.POST("/:id/confirm-payment", rl("trip.confirm", 20), deps.WalletHandler.ConfirmTripPayment)
			trips.POST("/:id/dispute-payment", rl("trip.dispute", 10), deps.WalletHandler.DisputeTripPayment)
		}
	}

	// Real-time event push. Sits outside /api/v1's authRequired group
	// because authentication happens inside the handler itself (JWT is
	// passed as a query parameter — standard WebSocket clients can't set
	// an Authorization header during the handshake).
	r.GET("/ws", deps.WSHandler.Connect)

	r.NoRoute(func(c *gin.Context) {
		utils.Fail(c, http.StatusNotFound, "route not found")
	})

	return r
}

func (d Dependencies) ipLimit(action string, limit int, window time.Duration) gin.HandlerFunc {
	return middleware.RateLimitByIP(d.RedisClient, action, limit, window)
}
