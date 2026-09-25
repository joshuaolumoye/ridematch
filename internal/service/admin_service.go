package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"ridematch-backend/internal/dto"
	"ridematch-backend/internal/email"
	"ridematch-backend/internal/models"
	"ridematch-backend/internal/repository"
	"ridematch-backend/internal/sms"
	"ridematch-backend/internal/storage"
)

// defaultDashboardSeriesDays is how many trailing days the dashboard's
// trip-volume chart covers.
const defaultDashboardSeriesDays = 14

// AdminService implements every staff-only operation behind the admin
// dashboard: driver document verification and subscription activation
// (pre-existing), plus the riders/users/bookings lists, detail pages,
// suspend/reactivate, driver notifications, live driver locations, and
// the dashboard overview. ActivateSubscription is deliberately generic
// (driver ID + duration) so the future Flutterwave webhook handler can
// call the exact same method an admin's manual override uses today —
// one code path, two callers.
type AdminService struct {
	drivers       repository.DriverRepository
	users         repository.UserRepository
	trips         repository.TripRepository
	locations     repository.LocationRepository
	prices        repository.SubscriptionPriceRepository
	fileStore     storage.Store
	smsSender     sms.Sender
	email         email.Sender
	notifications *NotificationService

	locationStaleAfter time.Duration
}

// NewAdminService constructs an AdminService.
func NewAdminService(
	drivers repository.DriverRepository,
	users repository.UserRepository,
	trips repository.TripRepository,
	locations repository.LocationRepository,
	prices repository.SubscriptionPriceRepository,
	fileStore storage.Store,
	smsSender sms.Sender,
	emailSender email.Sender,
	notifications *NotificationService,
	locationStaleAfter time.Duration,
) *AdminService {
	return &AdminService{
		drivers:            drivers,
		users:              users,
		trips:              trips,
		locations:          locations,
		prices:             prices,
		fileStore:          fileStore,
		smsSender:          smsSender,
		email:              emailSender,
		notifications:      notifications,
		locationStaleAfter: locationStaleAfter,
	}
}

// VerifyDriver approves or rejects a driver's submitted documents.
func (s *AdminService) VerifyDriver(ctx context.Context, driverID string, req dto.AdminVerifyDriverRequest) (*dto.DriverProfileResponse, error) {
	profile, err := s.drivers.FindByID(ctx, driverID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrDriverProfileNotFound
		}
		return nil, fmt.Errorf("service: failed to load driver profile: %w", err)
	}

	if req.Approve {
		profile.VerificationStatus = models.VerificationApproved
	} else {
		profile.VerificationStatus = models.VerificationRejected
	}
	profile.VerificationNote = req.Note

	if err := s.drivers.Update(ctx, profile); err != nil {
		return nil, fmt.Errorf("service: failed to update verification status: %w", err)
	}

	if s.notifications != nil {
		title := "Your documents were approved"
		body := "You're verified — go online whenever you're ready to start receiving trips."
		if !req.Approve {
			title = "Your documents need attention"
			body = "Your submitted documents were not approved."
			if req.Note != "" {
				body = req.Note
			}
		}
		_, _ = s.notifications.SendToUser(ctx, profile.UserID, models.NotificationDriverDocs, title, body, map[string]string{"driver_id": profile.ID})
	}

	resp := buildDriverProfileResponse(profile, signURL(ctx, s.fileStore, profile.VehiclePhotoURL))
	return &resp, nil
}

// ActivateSubscription extends (or starts) a driver's platform-access
// subscription by `days` from now, or from their current expiry if it
// hasn't lapsed yet — so renewing before expiry stacks rather than
// wasting the remaining paid time.
func (s *AdminService) ActivateSubscription(ctx context.Context, driverID string, days int) (*dto.DriverProfileResponse, error) {
	profile, err := s.drivers.FindByID(ctx, driverID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrDriverProfileNotFound
		}
		return nil, fmt.Errorf("service: failed to load driver profile: %w", err)
	}

	base := time.Now()
	if profile.HasActiveSubscription() {
		base = *profile.SubscriptionActiveUntil
	}
	newExpiry := base.Add(time.Duration(days) * 24 * time.Hour)
	profile.SubscriptionActiveUntil = &newExpiry

	if err := s.drivers.Update(ctx, profile); err != nil {
		return nil, fmt.Errorf("service: failed to activate subscription: %w", err)
	}

	resp := buildDriverProfileResponse(profile, signURL(ctx, s.fileStore, profile.VehiclePhotoURL))
	return &resp, nil
}

// Overview aggregates the numbers the dashboard's stat cards and charts
// need — "everything that's going on" in one call.
func (s *AdminService) Overview(ctx context.Context) (*dto.AdminOverviewResponse, error) {
	totalUsers, err := s.users.Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("service: failed to count users: %w", err)
	}
	usersByStatus, err := s.users.CountByStatus(ctx)
	if err != nil {
		return nil, fmt.Errorf("service: failed to count users by status: %w", err)
	}
	driversByVehicle, err := s.drivers.CountByVehicleType(ctx)
	if err != nil {
		return nil, fmt.Errorf("service: failed to count drivers by vehicle type: %w", err)
	}
	driversByVerification, err := s.drivers.CountByVerificationStatus(ctx)
	if err != nil {
		return nil, fmt.Errorf("service: failed to count drivers by verification status: %w", err)
	}
	onlineDrivers, err := s.drivers.CountOnline(ctx)
	if err != nil {
		return nil, fmt.Errorf("service: failed to count online drivers: %w", err)
	}
	tripsByStatus, err := s.trips.CountByStatus(ctx)
	if err != nil {
		return nil, fmt.Errorf("service: failed to count trips by status: %w", err)
	}
	grossValue, err := s.trips.SumGrossBookingValue(ctx)
	if err != nil {
		return nil, fmt.Errorf("service: failed to sum gross booking value: %w", err)
	}

	now := time.Now()
	startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	tripsToday, err := s.trips.CountCreatedSince(ctx, startOfToday)
	if err != nil {
		return nil, fmt.Errorf("service: failed to count today's trips: %w", err)
	}
	completedToday, err := s.trips.CountCompletedSince(ctx, startOfToday)
	if err != nil {
		return nil, fmt.Errorf("service: failed to count today's completed trips: %w", err)
	}

	since := startOfToday.AddDate(0, 0, -(defaultDashboardSeriesDays - 1))
	rawSeries, err := s.trips.DailySeries(ctx, since)
	if err != nil {
		return nil, fmt.Errorf("service: failed to build trip series: %w", err)
	}
	series := fillDailySeries(rawSeries, since, defaultDashboardSeriesDays)

	var totalDrivers int64
	for _, count := range driversByVehicle {
		totalDrivers += count
	}
	var totalTrips int64
	for _, count := range tripsByStatus {
		totalTrips += count
	}

	return &dto.AdminOverviewResponse{
		TotalUsers:            totalUsers,
		TotalDrivers:          totalDrivers,
		DriversByVehicleType:  driversByVehicle,
		OnlineDrivers:         onlineDrivers,
		PendingVerifications:  driversByVerification[string(models.VerificationPending)],
		SuspendedAccounts:     usersByStatus[string(models.StatusSuspended)],
		TotalTrips:            totalTrips,
		TripsByStatus:         tripsByStatus,
		TripsToday:            tripsToday,
		CompletedTripsToday:   completedToday,
		GrossBookingValueKobo: grossValue,
		Series:                series,
	}, nil
}

// fillDailySeries turns sparse per-day rows (only days with activity) into
// a dense, chart-ready series covering every day in the window, in order.
func fillDailySeries(rows []repository.TripDayStat, since time.Time, days int) []dto.AdminTripSeriesPoint {
	byDate := make(map[string]repository.TripDayStat, len(rows))
	for _, row := range rows {
		byDate[row.Date] = row
	}

	series := make([]dto.AdminTripSeriesPoint, 0, days)
	for i := 0; i < days; i++ {
		date := since.AddDate(0, 0, i).Format("2006-01-02")
		if row, ok := byDate[date]; ok {
			series = append(series, dto.AdminTripSeriesPoint{
				Date:           date,
				TripCount:      row.TripCount,
				CompletedCount: row.CompletedCount,
				GrossValueKobo: row.GrossValueKobo,
			})
			continue
		}
		series = append(series, dto.AdminTripSeriesPoint{Date: date})
	}
	return series
}

// ListDrivers returns a filtered, paginated riders list.
func (s *AdminService) ListDrivers(ctx context.Context, filter repository.DriverFilter, page, pageSize int) (*dto.AdminDriverListResponse, error) {
	page, pageSize = normalizePage(page, pageSize)

	profiles, total, err := s.drivers.FindAll(ctx, filter, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, fmt.Errorf("service: failed to list drivers: %w", err)
	}

	items := make([]dto.AdminDriverListItem, 0, len(profiles))
	for i := range profiles {
		items = append(items, s.toDriverListItem(ctx, &profiles[i]))
	}

	return &dto.AdminDriverListResponse{Items: items, Pagination: dto.NewPagination(page, pageSize, total)}, nil
}

// GetDriver returns a rider's full detail page: profile, signed document
// URLs, and trip/earnings summary.
func (s *AdminService) GetDriver(ctx context.Context, driverID string) (*dto.AdminDriverDetailResponse, error) {
	profile, err := s.drivers.FindByID(ctx, driverID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrDriverProfileNotFound
		}
		return nil, fmt.Errorf("service: failed to load driver profile: %w", err)
	}

	totalKobo, totalTrips, _, _, err := s.trips.SumEarningsByDriver(ctx, profile.ID)
	if err != nil {
		return nil, fmt.Errorf("service: failed to sum driver earnings: %w", err)
	}

	item := s.toDriverListItem(ctx, profile)
	return &dto.AdminDriverDetailResponse{
		AdminDriverListItem: item,
		VehiclePhotoURL:     signURL(ctx, s.fileStore, profile.VehiclePhotoURL),
		IDDocumentURL:       signURL(ctx, s.fileStore, profile.IDDocumentURL),
		VerificationNote:    profile.VerificationNote,
		TotalTrips:          totalTrips,
		CompletedTrips:      totalTrips, // SumEarningsByDriver only counts completed trips
		TotalEarningsKobo:   totalKobo,
	}, nil
}

func (s *AdminService) toDriverListItem(ctx context.Context, p *models.DriverProfile) dto.AdminDriverListItem {
	item := dto.AdminDriverListItem{
		ID:                    p.ID,
		UserID:                p.UserID,
		VehicleType:           string(p.VehicleType),
		PlateNumber:           p.PlateNumber,
		VerificationStatus:    string(p.VerificationStatus),
		IsOnline:              p.IsOnline,
		HasActiveSubscription: p.HasActiveSubscription(),
		RatingAverage:         p.RatingAverage,
		CreatedAt:             p.CreatedAt.UTC().Format(time.RFC3339),
	}
	if p.SubscriptionActiveUntil != nil {
		item.SubscriptionActiveUntil = p.SubscriptionActiveUntil.UTC().Format(time.RFC3339)
	}
	if p.User != nil {
		item.Name = p.User.Name
		item.Phone = p.User.Phone
		item.Email = p.User.Email
		item.PhotoURL = signURL(ctx, s.fileStore, p.User.PhotoURL)
		item.AccountStatus = string(p.User.Status)
	}
	return item
}

// NotifyDriver sends a driver a message (SMS and/or email, whichever the
// account has on file) — typically used to ask them to resubmit or update
// a document.
func (s *AdminService) NotifyDriver(ctx context.Context, driverID string, req dto.AdminNotifyDriverRequest) (*dto.AdminNotifyDriverResponse, error) {
	profile, err := s.drivers.FindByID(ctx, driverID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrDriverProfileNotFound
		}
		return nil, fmt.Errorf("service: failed to load driver profile: %w", err)
	}
	if profile.User == nil {
		return nil, fmt.Errorf("service: driver profile %s has no linked user", driverID)
	}

	resp := &dto.AdminNotifyDriverResponse{}
	subject := req.Subject
	if subject == "" {
		subject = "RideMatch: action needed on your driver documents"
	}

	if profile.User.Phone != "" {
		if err := s.smsSender.Send(ctx, profile.User.Phone, req.Message); err != nil {
			return nil, fmt.Errorf("service: failed to send driver notification sms: %w", err)
		}
		resp.SentViaSMS = true
	}
	if profile.User.Email != "" {
		if err := s.email.Send(ctx, profile.User.Email, subject, req.Message); err != nil {
			return nil, fmt.Errorf("service: failed to send driver notification email: %w", err)
		}
		resp.SentViaEmail = true
	}

	// Also lands in the driver's in-app notifications list — SMS/email
	// reach them outside the app, but this is what shows up in the app's
	// own notification screen and phone notification tray.
	if s.notifications != nil {
		_, _ = s.notifications.SendToUser(ctx, profile.UserID, models.NotificationDriverDocs, subject, req.Message, map[string]string{"driver_id": profile.ID})
	}

	return resp, nil
}

// ListUsers returns a filtered, paginated users list.
func (s *AdminService) ListUsers(ctx context.Context, filter repository.UserFilter, page, pageSize int) (*dto.AdminUserListResponse, error) {
	page, pageSize = normalizePage(page, pageSize)

	users, total, err := s.users.FindAll(ctx, filter, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, fmt.Errorf("service: failed to list users: %w", err)
	}

	items := make([]dto.AdminUserListItem, 0, len(users))
	for i := range users {
		items = append(items, s.toUserListItem(ctx, &users[i]))
	}

	return &dto.AdminUserListResponse{Items: items, Pagination: dto.NewPagination(page, pageSize, total)}, nil
}

// GetUser returns a user's detail page: their account, a summary of their
// bookings as a passenger, and their driver profile if they have one.
func (s *AdminService) GetUser(ctx context.Context, userID string) (*dto.AdminUserDetailResponse, error) {
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("service: failed to load user: %w", err)
	}

	// A generous page size here is fine: this only counts/summarizes the
	// user's trips for their detail page, it doesn't render them all —
	// see AdminHandler.UserTrips for the paginated bookings tab itself.
	passengerTrips, total, err := s.trips.FindAllByUser(ctx, userID, "", 1000, 0)
	if err != nil {
		return nil, fmt.Errorf("service: failed to load user trip history: %w", err)
	}
	var completed int64
	for _, t := range passengerTrips {
		if t.Status == models.TripCompleted {
			completed++
		}
	}

	resp := &dto.AdminUserDetailResponse{
		AdminUserListItem:         s.toUserListItem(ctx, user),
		TotalTripsAsPassenger:     total,
		CompletedTripsAsPassenger: completed,
	}

	if user.DriverProfile != nil {
		profile := user.DriverProfile
		profile.User = user
		item := s.toDriverListItem(ctx, profile)
		resp.DriverProfile = &item
	}

	return resp, nil
}

func (s *AdminService) toUserListItem(ctx context.Context, u *models.User) dto.AdminUserListItem {
	return dto.AdminUserListItem{
		ID:            u.ID,
		Name:          u.Name,
		Phone:         u.Phone,
		Email:         u.Email,
		PhotoURL:      signURL(ctx, s.fileStore, u.PhotoURL),
		Role:          string(u.Role),
		Status:        string(u.Status),
		IsDriver:      u.DriverProfile != nil,
		RatingAverage: u.RatingAverage,
		CreatedAt:     u.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// UserTrips returns a paginated page of a user's bookings — as a
// passenger, and as a driver too if they have a driver profile — newest
// first, for the user detail page's bookings tab.
func (s *AdminService) UserTrips(ctx context.Context, userID string, page, pageSize int) (*dto.AdminTripListResponse, error) {
	page, pageSize = normalizePage(page, pageSize)

	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("service: failed to load user: %w", err)
	}

	driverProfileID := ""
	if user.DriverProfile != nil {
		driverProfileID = user.DriverProfile.ID
	}

	trips, total, err := s.trips.FindAllByUser(ctx, userID, driverProfileID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, fmt.Errorf("service: failed to load user trips: %w", err)
	}

	items := make([]dto.AdminTripListItem, 0, len(trips))
	for _, t := range trips {
		items = append(items, toAdminTripListItem(&t))
	}

	return &dto.AdminTripListResponse{Items: items, Pagination: dto.NewPagination(page, pageSize, total)}, nil
}

// UpdateAccountStatus suspends, reactivates, or bans a user account — the
// underlying User record, so it applies whether the person is a plain
// passenger or also a driver (suspending blocks both capabilities).
func (s *AdminService) UpdateAccountStatus(ctx context.Context, userID string, req dto.AdminUpdateAccountStatusRequest) (*dto.AdminUserListItem, error) {
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("service: failed to load user: %w", err)
	}

	user.Status = models.UserStatus(req.Status)
	if err := s.users.Update(ctx, user); err != nil {
		return nil, fmt.Errorf("service: failed to update account status: %w", err)
	}

	if s.notifications != nil {
		title, body := accountStatusNotification(user.Status, req.Reason)
		_, _ = s.notifications.SendToUser(ctx, user.ID, models.NotificationAccountStatus, title, body, nil)
	}

	item := s.toUserListItem(ctx, user)
	return &item, nil
}

func accountStatusNotification(status models.UserStatus, reason string) (title, body string) {
	switch status {
	case models.StatusActive:
		return "Your account is active again", "Welcome back — you have full access to RideMatch again."
	case models.StatusBanned:
		body = "Your account has been permanently banned."
	default: // suspended
		body = "Your account has been temporarily suspended."
	}
	if reason != "" {
		body += " Reason: " + reason
	}
	return "Account status updated", body
}

// ListTrips returns a filtered, paginated bookings list.
func (s *AdminService) ListTrips(ctx context.Context, filter repository.TripFilter, page, pageSize int) (*dto.AdminTripListResponse, error) {
	page, pageSize = normalizePage(page, pageSize)

	trips, total, err := s.trips.FindAllAdmin(ctx, filter, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, fmt.Errorf("service: failed to list trips: %w", err)
	}

	items := make([]dto.AdminTripListItem, 0, len(trips))
	for _, t := range trips {
		items = append(items, toAdminTripListItem(&t))
	}

	return &dto.AdminTripListResponse{Items: items, Pagination: dto.NewPagination(page, pageSize, total)}, nil
}

// GetTrip returns one booking's detail row.
func (s *AdminService) GetTrip(ctx context.Context, tripID string) (*dto.AdminTripListItem, error) {
	trip, err := s.trips.FindByID(ctx, tripID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrTripNotFound
		}
		return nil, fmt.Errorf("service: failed to load trip: %w", err)
	}
	item := toAdminTripListItem(trip)
	return &item, nil
}

func toAdminTripListItem(t *models.Trip) dto.AdminTripListItem {
	item := dto.AdminTripListItem{
		ID:                 t.ID,
		Status:             string(t.Status),
		VehicleType:        string(t.VehicleType),
		PassengerID:        t.PassengerID,
		PickupAddress:      t.PickupAddress,
		DestinationAddress: t.DestinationAddress,
		OfferedPriceKobo:   t.OfferedPriceKobo,
		AgreedPriceKobo:    t.AgreedPriceKobo,
		CreatedAt:          t.CreatedAt.UTC().Format(time.RFC3339),
	}
	if t.Passenger != nil {
		item.PassengerName = t.Passenger.Name
	}
	if t.DriverID != nil {
		item.DriverID = *t.DriverID
	}
	if t.Driver != nil && t.Driver.User != nil {
		item.DriverName = t.Driver.User.Name
	}
	if t.CompletedAt != nil {
		item.CompletedAt = t.CompletedAt.UTC().Format(time.RFC3339)
	}
	return item
}

// DriverLocations returns every currently online driver of the given
// vehicle type with their live position, for the admin "riders by
// location" map. An empty vehicleType returns every vehicle type.
func (s *AdminService) DriverLocations(ctx context.Context, vehicleType string) ([]dto.AdminDriverLocationItem, error) {
	vehicleTypes := []models.VehicleType{models.VehicleCar, models.VehicleOkada, models.VehicleKeke, models.VehicleBus}
	if vehicleType != "" {
		vehicleTypes = []models.VehicleType{models.VehicleType(vehicleType)}
	}

	items := make([]dto.AdminDriverLocationItem, 0)
	for _, vt := range vehicleTypes {
		online, err := s.locations.ListOnline(ctx, vt, s.locationStaleAfter)
		if err != nil {
			return nil, fmt.Errorf("service: failed to list online %s drivers: %w", vt, err)
		}
		for _, o := range online {
			name := ""
			if profile, err := s.drivers.FindByID(ctx, o.DriverID); err == nil && profile.User != nil {
				name = profile.User.Name
			}
			items = append(items, dto.AdminDriverLocationItem{
				DriverID:    o.DriverID,
				Name:        name,
				VehicleType: string(vt),
				Latitude:    o.Latitude,
				Longitude:   o.Longitude,
			})
		}
	}
	return items, nil
}

// ListSubscriptionPrices returns the daily platform-access price for
// every vehicle type — okada, keke, car, and bus each have their own,
// admin-configurable rate.
func (s *AdminService) ListSubscriptionPrices(ctx context.Context) ([]dto.AdminSubscriptionPriceItem, error) {
	prices, err := s.prices.FindAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("service: failed to list subscription prices: %w", err)
	}

	items := make([]dto.AdminSubscriptionPriceItem, 0, len(prices))
	for _, p := range prices {
		items = append(items, toSubscriptionPriceItem(&p))
	}
	return items, nil
}

// UpdateSubscriptionPrice sets a vehicle type's daily platform-access
// price. Takes effect on the driver's *next* subscription checkout —
// never retroactively changes a subscription already paid for.
func (s *AdminService) UpdateSubscriptionPrice(ctx context.Context, vehicleType string, priceKoboPerDay int64) (*dto.AdminSubscriptionPriceItem, error) {
	if !isValidVehicleType(vehicleType) {
		return nil, ErrInvalidVehicleType
	}

	price := &models.SubscriptionPrice{
		VehicleType:     models.VehicleType(vehicleType),
		PriceKoboPerDay: priceKoboPerDay,
		UpdatedAt:       time.Now(),
	}
	if err := s.prices.Upsert(ctx, price); err != nil {
		return nil, fmt.Errorf("service: failed to update subscription price: %w", err)
	}

	item := toSubscriptionPriceItem(price)
	return &item, nil
}

func isValidVehicleType(vehicleType string) bool {
	switch models.VehicleType(vehicleType) {
	case models.VehicleCar, models.VehicleOkada, models.VehicleKeke, models.VehicleBus:
		return true
	default:
		return false
	}
}

func toSubscriptionPriceItem(p *models.SubscriptionPrice) dto.AdminSubscriptionPriceItem {
	return dto.AdminSubscriptionPriceItem{
		VehicleType:     string(p.VehicleType),
		PriceKoboPerDay: p.PriceKoboPerDay,
		UpdatedAt:       p.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// normalizePage applies sane defaults/bounds to page/pageSize query
// params shared by every admin list endpoint.
func normalizePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}
