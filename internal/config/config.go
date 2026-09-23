// Package config loads and validates application configuration from
// environment variables (and an optional .env file for local development).
package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

// Config holds every externally configurable setting the application needs.
// It is loaded once at startup and passed down explicitly rather than read
// from the environment scattered across the codebase.
type Config struct {
	AppEnv  string
	AppPort string
	// AppPublicURL is the externally reachable base URL for this API —
	// used to build absolute URLs for locally-stored uploads (a driver's
	// vehicle photo / ID document). Not needed with STORAGE_DRIVER=s3,
	// where S3PublicBaseURL is used instead.
	AppPublicURL string

	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string

	RedisAddr     string
	RedisPassword string
	RedisDB       int

	JWTAccessSecret   string
	JWTRefreshSecret  string
	JWTAccessTTL      time.Duration
	JWTRefreshTTL     time.Duration
	OTPTTL            time.Duration
	OTPLength         int
	OTPResendCooldown time.Duration
	OTPMaxAttempts    int
	DriverSubDailyFee int64 // kobo (NGN * 100)
	DriverGracePeriod time.Duration

	LocationStaleAfter        time.Duration
	NearbyDefaultRadiusKM     float64
	NearbyMaxRadiusKM         float64
	NearbyDefaultLimit        int
	NearbyCoordinatePrecision int // decimal places to round returned driver coordinates to

	RateLimitTripCreatePerMinute  int
	RateLimitOfferCreatePerMinute int
	TripExpiryAfter               time.Duration

	SMSProvider    string // "console" | "termii"
	TermiiAPIKey   string
	TermiiSenderID string

	// EmailProvider picks how OTP codes and other transactional email get
	// sent: "console" (default, logs to stdout — no credentials needed)
	// or "smtp", which delivers via the SMTP_* settings below against any
	// standard mailbox (Hostinger, Gmail, Zoho, Amazon SES SMTP, ...).
	EmailProvider   string
	SMTPHost        string
	SMTPPort        string
	SMTPUsername    string
	SMTPPassword    string
	SMTPFromAddress string
	SMTPFromName    string

	FlutterwaveSecretKey   string
	FlutterwavePublicKey   string
	FlutterwaveWebhookKey  string
	FlutterwaveRedirectURL string
	// FlutterwaveBaseURL overrides the Flutterwave API base URL. Leave
	// empty in normal operation (production and Flutterwave's test-mode
	// keys both use the same api.flutterwave.com host); only set this to
	// point at a local stub during tooling/tests.
	FlutterwaveBaseURL string

	// StorageDriver picks the uploaded-file backend: "local" (the default,
	// writes to ./uploads) or "s3" — any S3-compatible object store, which
	// in practice means whichever cheap provider is configured via the
	// S3_* vars below (IDrive e2 is what this app is configured for; the
	// same client works unmodified against R2, Backblaze B2, MinIO, or
	// real AWS S3 — it's all the same API).
	StorageDriver string // "local" | "s3"

	// S3Endpoint is the full HTTPS endpoint for the S3-compatible
	// provider, e.g. IDrive e2's per-account endpoint such as
	// "https://x1a2.svc.us-east-1.idrivee2-8.com" (found on the e2
	// dashboard next to the bucket). AWS S3 itself doesn't need this set.
	S3Endpoint string
	// S3Region is required by the AWS SDK's request signing even when the
	// provider doesn't really have regions (IDrive e2 doesn't) — any
	// stable value works as long as it's consistent; "us-east-1" is a
	// safe default.
	S3Region    string
	S3AccessKey string
	S3SecretKey string
	S3Bucket    string
	// S3PublicBaseURL is what gets prefixed onto an object key to build
	// the URL returned to clients. For a public IDrive e2 bucket, this is
	// typically "<S3Endpoint>/<bucket>"; set it separately (rather than
	// deriving it) so a CDN or custom domain in front of the bucket works
	// too.
	S3PublicBaseURL string
	// S3ForcePathStyle addresses objects as "<endpoint>/<bucket>/<key>"
	// instead of "<bucket>.<endpoint>/<key>". IDrive e2 and most
	// S3-compatible providers expect this on; real AWS S3 does not need
	// it. Defaults to true since every non-AWS provider this app targets
	// needs it.
	S3ForcePathStyle bool
}

// Load reads configuration from environment variables. It first attempts to
// load a `.env` file (ignored silently if absent, e.g. in production where
// real env vars are injected by the platform).
func Load() *Config {
	if err := godotenv.Load(); err != nil {
		log.Println("config: no .env file found, relying on process environment")
	}

	cfg := &Config{
		AppEnv:       getEnv("APP_ENV", "development"),
		AppPort:      getEnv("APP_PORT", "8080"),
		AppPublicURL: getEnv("APP_PUBLIC_URL", "http://localhost:8080"),

		DBHost:     getEnv("DB_HOST", "127.0.0.1"),
		DBPort:     getEnv("DB_PORT", "3306"),
		DBUser:     getEnv("DB_USER", "root"),
		DBPassword: getEnv("DB_PASSWORD", ""),
		DBName:     getEnv("DB_NAME", "ridematch"),

		RedisAddr:     getEnv("REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		RedisDB:       getEnvInt("REDIS_DB", 0),

		JWTAccessSecret:   mustGetEnv("JWT_ACCESS_SECRET"),
		JWTRefreshSecret:  mustGetEnv("JWT_REFRESH_SECRET"),
		JWTAccessTTL:      getEnvDuration("JWT_ACCESS_TTL", 15*time.Minute),
		JWTRefreshTTL:     getEnvDuration("JWT_REFRESH_TTL", 30*24*time.Hour),
		OTPTTL:            getEnvDuration("OTP_TTL", 5*time.Minute),
		OTPLength:         getEnvInt("OTP_LENGTH", 6),
		OTPResendCooldown: getEnvDuration("OTP_RESEND_COOLDOWN", 60*time.Second),
		OTPMaxAttempts:    getEnvInt("OTP_MAX_ATTEMPTS", 5),
		DriverSubDailyFee: int64(getEnvInt("DRIVER_SUB_DAILY_FEE_KOBO", 100000)), // NGN 1,000
		DriverGracePeriod: getEnvDuration("DRIVER_GRACE_PERIOD", 6*time.Hour),

		LocationStaleAfter:        getEnvDuration("LOCATION_STALE_AFTER", 45*time.Second),
		NearbyDefaultRadiusKM:     getEnvFloat("NEARBY_DEFAULT_RADIUS_KM", 5),
		NearbyMaxRadiusKM:         getEnvFloat("NEARBY_MAX_RADIUS_KM", 20),
		NearbyDefaultLimit:        getEnvInt("NEARBY_DEFAULT_LIMIT", 20),
		NearbyCoordinatePrecision: getEnvInt("NEARBY_COORDINATE_PRECISION", 3),

		RateLimitTripCreatePerMinute:  getEnvInt("RATE_LIMIT_TRIP_CREATE_PER_MINUTE", 5),
		RateLimitOfferCreatePerMinute: getEnvInt("RATE_LIMIT_OFFER_CREATE_PER_MINUTE", 20),
		TripExpiryAfter:               getEnvDuration("TRIP_EXPIRY_AFTER", 10*time.Minute),

		SMSProvider:    getEnv("SMS_PROVIDER", "console"),
		TermiiAPIKey:   getEnv("TERMII_API_KEY", ""),
		TermiiSenderID: getEnv("TERMII_SENDER_ID", "RideMatch"),

		EmailProvider:   getEnv("EMAIL_PROVIDER", "console"),
		SMTPHost:        getEnv("SMTP_HOST", ""),
		SMTPPort:        getEnv("SMTP_PORT", "587"),
		SMTPUsername:    getEnv("SMTP_USERNAME", ""),
		SMTPPassword:    getEnv("SMTP_PASSWORD", ""),
		SMTPFromAddress: getEnv("SMTP_FROM_ADDRESS", ""),
		SMTPFromName:    getEnv("SMTP_FROM_NAME", "RideMatch"),

		FlutterwaveSecretKey:   getEnv("FLW_SECRET_KEY", ""),
		FlutterwavePublicKey:   getEnv("FLW_PUBLIC_KEY", ""),
		FlutterwaveWebhookKey:  getEnv("FLW_WEBHOOK_SECRET_HASH", ""),
		FlutterwaveRedirectURL: getEnv("FLW_REDIRECT_URL", "https://ridematch.app/payment-callback"),
		FlutterwaveBaseURL:     getEnv("FLW_BASE_URL", ""),

		StorageDriver:    getEnv("STORAGE_DRIVER", "local"),
		S3Endpoint:       getEnv("S3_ENDPOINT", ""),
		S3Region:         getEnv("S3_REGION", "us-east-1"),
		S3AccessKey:      getEnv("S3_ACCESS_KEY", ""),
		S3SecretKey:      getEnv("S3_SECRET_KEY", ""),
		S3Bucket:         getEnv("S3_BUCKET", ""),
		S3PublicBaseURL:  getEnv("S3_PUBLIC_BASE_URL", ""),
		S3ForcePathStyle: getEnvBool("S3_FORCE_PATH_STYLE", true),
	}

	return cfg
}

// DSN builds the MySQL data source name used by the GORM MySQL driver.
func (c *Config) DSN() string {
	return fmt.Sprintf(
		"%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=UTC&multiStatements=true",
		c.DBUser, c.DBPassword, c.DBHost, c.DBPort, c.DBName,
	)
}

// IsProduction reports whether the app is running with APP_ENV=production.
func (c *Config) IsProduction() bool {
	return c.AppEnv == "production"
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

// mustGetEnv panics on startup (not mid-request) if a required secret is
// missing, so misconfiguration is caught immediately rather than causing
// silent, hard-to-diagnose auth failures later.
func mustGetEnv(key string) string {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		log.Fatalf("config: required environment variable %s is not set", key)
	}
	return v
}

func getEnvInt(key string, fallback int) int {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		log.Printf("config: invalid int for %s=%q, using fallback %d", key, v, fallback)
		return fallback
	}
	return i
}

func getEnvBool(key string, fallback bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		log.Printf("config: invalid bool for %s=%q, using fallback %v", key, v, fallback)
		return fallback
	}
	return b
}

func getEnvFloat(key string, fallback float64) float64 {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		log.Printf("config: invalid float for %s=%q, using fallback %v", key, v, fallback)
		return fallback
	}
	return f
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		log.Printf("config: invalid duration for %s=%q, using fallback %s", key, v, fallback)
		return fallback
	}
	return d
}
