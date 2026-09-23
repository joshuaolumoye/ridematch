# RideMatch API

Backend for RideMatch — a multi-modal (car, okada, keke, bus) ride/logistics
matching platform for Nigeria. Phone-or-email + OTP authentication (no
passwords), no in-app payment for trip fares (drivers pay a flat daily
platform-access fee via Flutterwave instead).

This milestone covers **Phase 1: authentication** — phone/email OTP
registration+login, JWT access tokens, rotating refresh tokens, and the
driver/user data model foundation. Matching, live location, negotiation,
and payment webhooks are follow-on milestones.

## Stack

- **Go 1.24** + **Gin** — HTTP framework
- **GORM** + **MySQL** — persistence, repository pattern
- **Redis** — reserved for live driver geolocation (GEO commands) and rate
  limiting in the next milestone; connected at startup so misconfiguration
  is caught early
- **JWT** (access tokens) + opaque, hashed, rotating refresh tokens
- **Swagger / OpenAPI 3.0** — interactive docs at `/docs`

## Project structure

```
cmd/
  api/              main entrypoint — wires config, DB, repos, services,
                    handlers, router, and starts the HTTP server
  migrate/          standalone migration runner (go run ./cmd/migrate)
internal/
  config/           env var loading + validation
  database/         MySQL (GORM) and Redis connection setup
  models/           GORM entities (User, DriverProfile, OTPRequest,
                    RefreshToken, Trip, TripOffer, PaymentTransaction)
  repository/       repository-pattern interfaces + GORM/Redis
                    implementations — the only layer that imports
                    GORM or the Redis client directly
  service/          business logic (AuthService, DriverService,
                    AdminService, TripService, PaymentService); no gin
                    or GORM imports. payment_service_test.go covers the
                    webhook logic with a fake gateway (go test ./...)
  handler/          gin HTTP handlers; binds requests, calls one service
                    method, maps errors to HTTP responses
  middleware/       JWT auth, role checks, CORS, Redis-backed rate limiting
  dto/              request/response payload shapes (kept separate from
                    models so the API contract can stay stable even as the
                    DB schema evolves)
  router/           route registration
  ws/               WebSocket connection hub + event types for real-time
                    trip lifecycle push
  flutterwave/      minimal Flutterwave v3 API client (checkout link
                    creation + server-side transaction verification)
  utils/            JWT issuance/parsing, OTP generation/hashing, phone
                    normalization, phone-vs-email identifier detection,
                    token hashing, the shared JSON response envelope helper
  sms/              SMS sender interface — console logger for local dev,
                    Termii implementation for production
  email/            Email sender interface — console logger for local dev,
                    stdlib-only SMTP implementation (Hostinger, Gmail,
                    Zoho, SES SMTP, or any other standard mailbox) for
                    production
scripts/
  trip_lifecycle_test.py      end-to-end smoke test (REST + WebSocket +
                               concurrency race test) against a running server
  payment_flow_test.py        end-to-end payment smoke test against a
                               running server + the stub below
  stub_flutterwave_server.py  minimal stand-in for Flutterwave's API, for
                               testing the payment flow without a real account
```

## Architecture notes

- **Repository pattern**: handlers never touch GORM. `handler` calls
  `service`, `service` calls `repository` interfaces. To swap MySQL for
  something else later, only `internal/repository` changes.
- **No passwords.** Auth is a phone number *or* an email address + OTP —
  `POST /auth/otp/request` and `/auth/otp/verify` take a single
  `identifier` field, detect which kind it is (contains "@" → email,
  otherwise validated as a Nigerian phone number), and dispatch the code
  over the matching channel (SMS or email). `/auth/otp/verify` handles
  both first-time registration and every subsequent login through the
  same endpoint — if the identifier has no account yet, one is created
  automatically. An account is keyed by whichever identifier it signed up
  with; `models.User` has both `Phone` and `Email` columns but normally
  only one is populated per account.
- **Refresh token rotation.** Refresh tokens are random opaque strings
  (not JWTs), stored server-side as a SHA-256 hash so a database leak
  doesn't hand out usable tokens. Every `POST /auth/token/refresh` call
  revokes the token it was given and issues a new one — reusing an old
  refresh token fails, which limits the damage if one is ever stolen.
- **OTP codes are bcrypt-hashed** before storage, same treatment as a
  password, even though they're short-lived.
- **`utils.APIResponse`** is the single envelope every endpoint returns —
  `{ success, message, data, error }` — so the mobile app can handle every
  response the same way. Call `utils.Success` / `utils.Fail` from a
  handler instead of `c.JSON` directly.

## Prerequisites

- Go 1.24+
- MySQL 8+ (or MariaDB 10.6+)
- Redis 6+

## Setup

```bash
# 1. Install dependencies
go mod download

# 2. Configure environment
cp .env.example .env
# edit .env: set DB_USER/DB_PASSWORD/DB_NAME, and generate JWT secrets:
openssl rand -base64 48   # run twice, once for each JWT_*_SECRET

# 3. Create the database (adjust to your MySQL setup)
mysql -u root -p -e "CREATE DATABASE ridematch CHARACTER SET utf8mb4;"

# 4. Run migrations — creates/updates every table, safe to re-run anytime
make migrate

# 5. Start the API
make run
```

The server starts on `http://localhost:8080` by default. Interactive API
docs are at **http://localhost:8080/docs**.

### Local OTP testing

With `SMS_PROVIDER=console` and `EMAIL_PROVIDER=console` (both the
default), OTP codes are printed to the server log instead of sent as real
SMS/email:

```
[SMS -> +2348012345678] Your RideMatch verification code is 238413. ...
[EMAIL -> josh@example.com] Your RideMatch verification code is 238413. ...
```

Copy that code into `POST /auth/otp/verify` to complete the flow without
an SMS/email account. Switch to `SMS_PROVIDER=termii` (+ `TERMII_API_KEY`)
and `EMAIL_PROVIDER=smtp` (+ the `SMTP_*` settings — see `.env.example`
for the exact values a Hostinger mailbox needs) to send real messages in
production.

## API overview

Full interactive documentation — request/response schemas, try-it-out —
is served at `/docs`. Summary:

| Method | Path                      | Auth | Description |
|--------|---------------------------|------|--------------|
| GET    | `/health`                 | —    | Liveness check |
| POST   | `/api/v1/auth/otp/request`| —    | Send a 6-digit OTP by SMS or email (auto-detected from `identifier`) |
| POST   | `/api/v1/auth/otp/verify` | —    | Verify OTP; creates account on first use; returns tokens |
| POST   | `/api/v1/auth/token/refresh` | — | Rotate a refresh token for a new pair |
| POST   | `/api/v1/auth/logout`     | —    | Revoke a refresh token |
| GET    | `/api/v1/users/me`        | Bearer | Get the authenticated user's profile |
| PATCH  | `/api/v1/users/me`        | Bearer | Update the caller's own name and/or photo — both optional, send only what changed |
| DELETE | `/api/v1/users/me`        | Bearer | Permanently end every session and soft-delete the account (PII scrubbed immediately; trip/rating history kept for the other party's records) |
| POST   | `/api/v1/uploads`         | Bearer | Upload a file (JPEG/PNG/WebP/PDF, max 8MB) — `multipart/form-data`, field name `file`; returns `{ "url": "..." }`. Used for a driver's vehicle photo and ID document before `POST /driver/register`. |
| POST   | `/api/v1/driver/register` | Bearer | Register the current account as a driver (vehicle + ID doc URLs) |
| GET    | `/api/v1/driver/profile`  | Bearer | Get the authenticated driver's profile |
| POST   | `/api/v1/driver/online`   | Bearer | Go online — requires approved verification + active subscription |
| POST   | `/api/v1/driver/offline`  | Bearer | Go offline |
| POST   | `/api/v1/driver/location` | Bearer | Live location ping (call every 5-10s while online) |
| GET    | `/api/v1/drivers/nearby`  | Bearer | Passenger-facing: nearby online drivers by vehicle type |
| PATCH  | `/api/v1/admin/drivers/:id/verify` | Bearer (admin) | Approve/reject a driver's documents |
| PATCH  | `/api/v1/admin/drivers/:id/subscription` | Bearer (admin) | Manually grant N days of platform access |
| POST   | `/api/v1/trips`           | Bearer | Passenger requests a trip; nearby drivers notified instantly over WebSocket |
| GET    | `/api/v1/trips/active`    | Bearer | Get the caller's current in-progress trip, if any |
| GET    | `/api/v1/trips/history`   | Bearer | Paginated (`?page=&page_size=`, max 50/page) list of the caller's own trips, newest first, every status |
| GET    | `/api/v1/trips/nearby`    | Bearer | Driver-facing: open trip requests near a point (REST fallback for the WS push) |
| GET    | `/api/v1/trips/:id`       | Bearer | Get a trip (passenger or matched driver only) |
| POST   | `/api/v1/trips/:id/offers` | Bearer | Driver proposes a price on an open trip |
| GET    | `/api/v1/trips/:id/offers` | Bearer | Passenger lists pending offers on their trip |
| POST   | `/api/v1/trips/:id/offers/:offerId/accept` | Bearer | Passenger accepts an offer — matches the trip and returns the one-time pickup PIN |
| POST   | `/api/v1/trips/:id/offers/:offerId/reject` | Bearer | Passenger rejects an offer |
| POST   | `/api/v1/trips/:id/confirm-pickup` | Bearer | Driver enters the passenger's PIN to confirm pickup actually happened |
| POST   | `/api/v1/trips/:id/complete` | Bearer | Driver marks the trip complete |
| POST   | `/api/v1/trips/:id/cancel` | Bearer | Passenger or matched driver cancels (only before pickup is confirmed) |
| POST   | `/api/v1/trips/:id/rate`  | Bearer | Either side of a completed trip rates the other (1-5, once each) — folds straight into that party's running average |
| GET    | `/ws?token=<access_token>` | Query token | WebSocket upgrade — real-time push for every trip event above |
| POST   | `/api/v1/driver/subscription/checkout` | Bearer | Create a Flutterwave checkout link to pay for N days of platform access |
| GET    | `/api/v1/driver/trips/active` | Bearer | The caller's current matched/picked-up trip as a driver, if any (404 if none) |
| GET    | `/api/v1/driver/trips/history` | Bearer | Paginated (`?page=&page_size=`, max 50/page) list of the caller's own trips as a driver, newest first, every status |
| GET    | `/api/v1/driver/earnings` | Bearer | Real, SQL-aggregated earnings summary (all-time + today) for the calling driver, plus their 10 most recent trips |
| POST   | `/webhooks/flutterwave`   | `verif-hash` header | Flutterwave calls this when a payment completes — not for direct use |

All authenticated endpoints expect `Authorization: Bearer <access_token>`.

### Driver online/offline + nearby matching

This is the core matching mechanic: a driver's live position lives in
**Redis** (GEO commands), not MySQL — durable driver records (vehicle,
verification, subscription) stay in MySQL, but "who's online right now
and where" is inherently ephemeral and Redis GEO queries (`GEOSEARCH`)
are purpose-built for radius lookups.

- Going online requires `verification_status = approved` **and** an
  active subscription (`subscription_active_until` in the future) — both
  gates are enforced in `DriverService.GoOnline`, not just at the UI
  layer.
- A driver who stops sending `/driver/location` pings (app killed, lost
  connection) without calling `/driver/offline` is **not** stuck online
  forever: their last-seen timestamp goes stale after
  `LOCATION_STALE_AFTER` (default 45s) and they're filtered out of — and
  lazily removed from — nearby-driver results on the next search.
- Coordinates returned by `/drivers/nearby` are rounded
  (`NEARBY_COORDINATE_PRECISION`, default 3 decimal places ≈ 111m), not
  exact — a passenger browsing the map doesn't need a driver's precise
  live position; that level of detail is reserved for after a match is
  confirmed (next milestone).
- Vehicle types (`car`/`okada`/`keke`/`bus`) are matched in separate Redis
  keys, so a passenger looking for an okada never pays the cost of
  scanning car positions.

### Trip lifecycle: request → negotiate → match → pickup PIN → complete

This is the centerpiece feature: a passenger requests a trip, nearby
drivers see it and propose a price, the passenger accepts one, and — the
part that matters most for trust on a cash-settled platform — the driver
must enter a PIN the passenger shows them **in person** before the trip
can ever be marked picked up. There's no way to skip straight from
"matched" to "completed".

**States:** `requested` → `matched` → `picked_up` → `completed`, with
`cancelled` and `expired` as terminal side-exits from `requested`/`matched`.

- **Real-time, not polling.** The moment a trip is created, every nearby
  online driver of the matching vehicle type gets it pushed over the `/ws`
  WebSocket connection (median well under 50ms in local testing — see
  below). `GET /trips/nearby` is the fallback for a driver who wasn't
  connected at that instant, reading the same Redis geo index
  (`geo:trips:{vehicle_type}`, mirroring the existing driver-location
  index). REST is always the source of truth; the socket is a low-latency
  notification layer only — reconnect and re-fetch if a client ever
  suspects it missed something.
- **Concurrency-safe by construction.** Every state transition
  (`TripRepository.CompareAndSwapStatus`, `TripOfferRepository.CompareAndSwapStatus`)
  is a single `UPDATE ... WHERE id = ? AND status = ?`, relying on MySQL's
  own row locking. Two concurrent "accept offer" taps, a double-tapped
  "confirm pickup", or a passenger cancelling at the exact moment a driver
  is being matched — exactly one request wins, the other gets a clean 400/409,
  and the trip never ends up in a corrupted or ambiguous state. This is
  covered by an automated concurrency test (5 simultaneous confirm-pickup
  requests with the correct PIN → exactly 1 succeeds) in the test script
  under `scripts/` (see below).
- **The pickup PIN.** Generated the moment an offer is accepted, hashed
  with bcrypt (the same primitive used for OTP codes) before being stored,
  and returned **in plaintext exactly once**, in the accept-offer response
  — never persisted in plaintext, never returned by any other endpoint.
- **One active trip per person.** A passenger can't open a second trip
  while one is in progress; a driver can't be matched onto a second trip
  while already on one — both enforced at the service layer against live
  DB state, not just the client UI.
- **Self-healing expiry.** An open trip nobody's accepted after
  `TRIP_EXPIRY_AFTER` (default 10m) is lazily marked `expired` the next
  time anything touches it (a read, an offer attempt) — no background
  sweeper process required.
- **Rate limited.** `POST /trips` and `POST /trips/:id/offers` are capped
  per user per minute (`RATE_LIMIT_TRIP_CREATE_PER_MINUTE`,
  `RATE_LIMIT_OFFER_CREATE_PER_MINUTE`) via the Redis-backed fixed-window
  limiter, which fails open if Redis is unreachable so an infra blip never
  takes down trip creation entirely.
- **WebSocket auth.** Standard WebSocket clients can't set an
  `Authorization` header during the handshake, so the access token is
  passed as `?token=` on the connection URL instead:
  `wss://your-host/ws?token=<access_token>`. Message envelope:
  `{"type": "trip.matched", "data": {...}}`. Event types: `trip.new_request`,
  `trip.offer_received`, `trip.offer_rejected`, `trip.matched`,
  `trip.closed`, `trip.picked_up`, `trip.completed`, `trip.cancelled`.

**Try it:** `scripts/trip_lifecycle_test.py` runs the entire flow above
end-to-end against a running server — login, driver approval, trip
request, WebSocket push assertions, the 5-concurrent-requests race test,
wrong-PIN rejection, and cancellation. Requires
`pip install requests websockets` and `SMS_PROVIDER=console` (the local
dev default):

```bash
API_LOG_PATH=/tmp/api.log DB_PASSWORD=your_db_password \
  python3 scripts/trip_lifecycle_test.py
```

### Driver subscription payment (Flutterwave)

The daily platform-access fee is collected through Flutterwave's hosted
checkout — the app never touches card details directly.

**Flow:**
1. Driver calls `POST /driver/subscription/checkout` (optionally `{"days": N}`,
   default 1, max 30) → gets back a `payment_link` and Flutterwave's own
   checkout page opens in a browser/WebView.
2. Driver pays. Flutterwave calls `POST /webhooks/flutterwave` with the result.
3. **Only once that webhook is verified** does the driver's
   `subscription_active_until` actually move — nothing is granted at
   checkout-link creation time.

**Why the webhook is safe to expose publicly with no auth of its own** —
two independent checks, both required:
- The `verif-hash` request header must match `FLW_WEBHOOK_SECRET_HASH`
  (the same string you configure in the Flutterwave dashboard under
  Settings → Webhooks). This is Flutterwave's own webhook-auth mechanism —
  a shared secret echoed back verbatim, not an HMAC signature.
- Even with a valid header, the webhook body is never trusted at face
  value: `PaymentService.HandleWebhook` independently re-fetches the
  transaction from Flutterwave's `/transactions/:id/verify` endpoint using
  the secret key, and only credits the driver if that server-to-server
  call's status, amount, and currency all match what was actually
  requested at checkout. A mismatch marks the transaction `failed` and
  grants nothing.
- Every payment attempt is recorded in `payment_transactions` **before**
  the driver ever reaches Flutterwave's page, keyed by a reference this
  app generates (`tx_ref`) — so the webhook looks up a row it already
  knows about rather than trusting whatever reference the request claims.
- Flutterwave retries webhook delivery; a transaction already in a
  terminal state (`successful`/`failed`) is a no-op on a repeat delivery,
  so a driver is never credited twice for one payment.
- `PaymentService.HandleWebhook` calls the exact same
  `AdminService.ActivateSubscription(driverID, days)` method a manual
  admin override uses — one code path, two callers, so a payment and an
  admin grant behave identically (stacking on top of remaining time
  rather than overwriting it).

**Try it without a real Flutterwave account:**

```bash
python3 scripts/stub_flutterwave_server.py     # terminal 1, leave running
# in .env: FLW_BASE_URL=http://127.0.0.1:9099
#          FLW_WEBHOOK_SECRET_HASH=test-webhook-secret-123
make run                                        # terminal 2
API_LOG_PATH=/tmp/api.log python3 scripts/payment_flow_test.py   # terminal 3
```

This drives a full checkout → webhook → subscription-activated flow
through the real HTTP stack against a stub standing in for Flutterwave's
documented API responses. The core crediting/verification logic also has
unit tests with a fake gateway — no network involved — covering amount
mismatches and webhook-retry idempotency: `go test ./internal/service/...`.

On a normal machine with internet access, set `FLW_SECRET_KEY` /
`FLW_WEBHOOK_SECRET_HASH` to your real Flutterwave dashboard values, leave
`FLW_BASE_URL` empty, and configure
`https://your-domain.com/webhooks/flutterwave` as the webhook URL in the
Flutterwave dashboard.

### Becoming an admin (manual, by design)

There is no self-serve admin signup — admin accounts are created by
directly updating a row:

```sql
UPDATE users SET role = 'admin' WHERE phone = '+234XXXXXXXXXX';
-- or, for an account that signed up with email instead of phone:
UPDATE users SET role = 'admin' WHERE email = 'you@example.com';
```

The user then needs to **log in again** (`/auth/otp/verify`) to get a
fresh access token — the role is embedded in the JWT at issuance time, so
an already-issued token won't reflect the change until it's reissued.

`AdminService.ActivateSubscription` is written generically (driver ID +
number of days) specifically so the upcoming Flutterwave webhook handler
can call the exact same method a manual admin override uses today.

## Object storage (IDrive e2)

Uploaded files (a driver's vehicle photo, ID document — `POST /uploads`)
go through `internal/storage`, an interface with two implementations:

- **`local`** (default) — writes to `./uploads` on this server's own
  disk, served back via the API's static route. Fine for development;
  wrong for production (uploads vanish on redeploy, and don't survive
  running more than one API instance).
- **`s3`** — any S3-compatible object store, via `internal/storage/s3.go`
  (`github.com/aws/aws-sdk-go-v2/service/s3`). This app is configured
  for **IDrive e2**: S3-compatible, no egress fees, and meaningfully
  cheaper than AWS S3/Cloudflare R2/Backblaze B2 at small scale — a
  reasonable default while there's no revenue yet to justify a pricier
  option. Nothing in the client is IDrive-specific, though: point
  `S3_ENDPOINT` at R2, B2, MinIO, or real AWS S3 instead and it works
  unchanged.

**To switch on IDrive e2:**

1. In the [e2 dashboard](https://www.idrive.com/e2/), create a bucket.
   Set it to allow public read (or put a CDN/custom domain in front of
   it) — the URLs `POST /uploads` returns have to be reachable by end
   users' phones, the same contract the `local` driver already has via
   `APP_PUBLIC_URL`.
2. Create an access key pair — e2 dashboard → **Access Keys**.
3. Copy the bucket's endpoint from its detail page. It's per-account,
   shaped like `https://<id>.<region>.idrivee2-<n>.com`, not a fixed
   host — copy it from the dashboard rather than guessing it.
4. In `.env`, fill in `S3_ENDPOINT`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`,
   `S3_BUCKET` (see `.env.example` for the full list, including
   `S3_PUBLIC_BASE_URL` and `S3_FORCE_PATH_STYLE`, which default
   sensibly for e2 and rarely need changing), then set
   `STORAGE_DRIVER=s3` and restart the API.

`storage.New` fails loudly at startup if `STORAGE_DRIVER=s3` is set with
any of `S3_ENDPOINT`/`S3_ACCESS_KEY`/`S3_SECRET_KEY`/`S3_BUCKET` missing,
rather than silently falling back to local disk — a driver's real ID
document ending up unencrypted on local disk because a credential was
typo'd would be a much worse surprise than a startup error. `.env` here
ships with `STORAGE_DRIVER=local` until real e2 credentials are filled
in, so the server always starts cleanly out of the box.

## Commands

```bash
make run             # start the API server
make migrate         # create/update database tables
make build            # compile ./bin/api
make build-migrate    # compile ./bin/migrate
make fmt              # gofmt the codebase
make vet              # go vet
make test             # run the Go unit test suite
make tidy             # sync go.mod/go.sum
```

## A note on `go.mod` replace directives

`go.mod` contains `replace` directives redirecting `golang.org/x/*`,
`google.golang.org/protobuf`, `gopkg.in/yaml.*`, `gopkg.in/check.v1`, and
`gorm.io/*` to their canonical mirrors on `github.com`. These were added
because the sandboxed environment this project was originally built in
could only reach `github.com`, not `proxy.golang.org` or the vanity
`golang.org`/`gopkg.in` domains. **On a normal machine with unrestricted
internet access you can safely delete these `replace` lines** — the
plain module paths will resolve normally via the standard Go module
proxy. They're left in place because they also happen to pin known-good,
tested versions, so removing them is optional, not required.

## What's next

The backend is feature-complete for launch: Phase 1 (auth), Phase 2
(driver online/offline + Redis GEO nearby matching), Phase 3 (trip
request → negotiation → match → pickup PIN → complete, over REST +
WebSocket), Phase 4 (Flutterwave driver subscription payment), file
uploads, driver earnings/history, profile management, account deletion,
and the trip-rating system are all done and tested end-to-end.

1. ~~Driver online/offline toggle + Redis GEO live-location writes, and a
   nearby-drivers/passengers query endpoint~~ ✅ done
2. ~~Trip request → negotiation (offer) → match confirmed → pickup PIN →
   trip complete state machine, with real-time WebSocket push~~ ✅ done
3. ~~Flutterwave subscription checkout + webhook for driver daily
   access~~ ✅ done
4. ~~React Native app wiring against this API~~ ✅ done through the app's
   Category 4 (Driver flow) — which needed a few small additions here
   beyond the original four phases, added along the way rather than
   worked around client-side:
   - `POST /uploads` — a real local-disk (or any S3-compatible object
     store, if configured — this app runs it against IDrive e2) file
     store for a driver's vehicle photo and ID document, since the
     original `RegisterDriverRequest` only accepted URLs and there was
     no endpoint yet to produce one from a phone-picked image
     (`internal/storage/`, `STORAGE_DRIVER=local|s3` in `.env` — see
     "Object storage" below for the IDrive e2 setup).
   - `GET /driver/trips/active`, `GET /driver/trips/history`, and
     `GET /driver/earnings` — driver-side equivalents of the
     passenger-facing `/trips/active` and `/trips/history` endpoints, plus
     a real SQL-aggregated earnings summary (today + all-time), so the
     app never has to fake or locally sum what the driver has earned.
5. ~~Real-time + polish~~ ✅ done — the app's Category 5, which needed
   three more backend additions, all real and tested, not stubs:
   - `PATCH /users/me` / `DELETE /users/me` — a caller can now actually
     update their own name/photo, or permanently delete their account
     (every session revoked, PII scrubbed so the phone number frees up
     for reuse, trip/rating history kept intact for the other party).
   - `POST /trips/:id/rate` — the missing other half of the rating
     system: `User.RatingAverage`/`DriverProfile.RatingAverage` existed
     from day one but nothing ever wrote to them, so every account
     silently showed a permanent 5.00. Either side of a completed trip
     can now rate the other once; it folds straight into a running
     incremental average (no re-scanning every past rating), and
     `TripResponse` now reports `passenger_rated`/`driver_rated` so a
     client knows whether to show the prompt.
   - New `trip_ratings` table (`internal/models/trip_rating.go`) — run
     `go run ./cmd/migrate` after pulling this to create it.
