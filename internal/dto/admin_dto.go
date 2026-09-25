package dto

// Pagination is the common paging envelope returned by every admin list
// endpoint (drivers, users, trips).
type Pagination struct {
	Page       int   `json:"page"`
	PageSize   int   `json:"page_size"`
	TotalItems int64 `json:"total_items"`
	TotalPages int   `json:"total_pages"`
}

// NewPagination computes TotalPages from TotalItems/PageSize so callers
// never have to duplicate that arithmetic.
func NewPagination(page, pageSize int, totalItems int64) Pagination {
	totalPages := 0
	if pageSize > 0 {
		totalPages = int((totalItems + int64(pageSize) - 1) / int64(pageSize))
	}
	return Pagination{Page: page, PageSize: pageSize, TotalItems: totalItems, TotalPages: totalPages}
}

// AdminTripSeriesPoint is one day of the trip-volume series shown on the
// dashboard's main chart.
type AdminTripSeriesPoint struct {
	Date           string `json:"date" example:"2026-09-20"`
	TripCount      int64  `json:"trip_count"`
	CompletedCount int64  `json:"completed_count"`
	GrossValueKobo int64  `json:"gross_value_kobo"`
}

// AdminOverviewResponse backs the dashboard's stat cards and charts —
// "everything that's going on" in one call.
type AdminOverviewResponse struct {
	TotalUsers            int64                  `json:"total_users"`
	TotalDrivers          int64                  `json:"total_drivers"`
	DriversByVehicleType  map[string]int64       `json:"drivers_by_vehicle_type"`
	OnlineDrivers         int64                  `json:"online_drivers"`
	PendingVerifications  int64                  `json:"pending_verifications"`
	SuspendedAccounts     int64                  `json:"suspended_accounts"`
	TotalTrips            int64                  `json:"total_trips"`
	TripsByStatus         map[string]int64       `json:"trips_by_status"`
	TripsToday            int64                  `json:"trips_today"`
	CompletedTripsToday   int64                  `json:"completed_trips_today"`
	GrossBookingValueKobo int64                  `json:"gross_booking_value_kobo"`
	Series                []AdminTripSeriesPoint `json:"series"`
}

// AdminDriverListItem is one row of the riders list, filterable by
// vehicle type.
type AdminDriverListItem struct {
	ID                      string  `json:"id"`
	UserID                  string  `json:"user_id"`
	Name                    string  `json:"name"`
	Phone                   string  `json:"phone,omitempty"`
	Email                   string  `json:"email,omitempty"`
	PhotoURL                string  `json:"photo_url,omitempty"`
	VehicleType             string  `json:"vehicle_type"`
	PlateNumber             string  `json:"plate_number"`
	VerificationStatus      string  `json:"verification_status"`
	IsOnline                bool    `json:"is_online"`
	HasActiveSubscription   bool    `json:"has_active_subscription"`
	SubscriptionActiveUntil string  `json:"subscription_active_until,omitempty"`
	AccountStatus           string  `json:"account_status"`
	RatingAverage           float64 `json:"rating_average"`
	CreatedAt               string  `json:"created_at"`
}

// AdminDriverListResponse is a page of riders.
type AdminDriverListResponse struct {
	Items      []AdminDriverListItem `json:"items"`
	Pagination Pagination            `json:"pagination"`
}

// AdminDriverDetailResponse is a rider's detail page: profile, submitted
// documents (signed, admin-only), and their trip/earnings summary.
type AdminDriverDetailResponse struct {
	AdminDriverListItem
	VehiclePhotoURL   string `json:"vehicle_photo_url,omitempty"`
	IDDocumentURL     string `json:"id_document_url,omitempty"`
	VerificationNote  string `json:"verification_note,omitempty"`
	TotalTrips        int64  `json:"total_trips"`
	CompletedTrips    int64  `json:"completed_trips"`
	TotalEarningsKobo int64  `json:"total_earnings_kobo"`
}

// AdminUserListItem is one row of the all-users list.
type AdminUserListItem struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Phone         string  `json:"phone,omitempty"`
	Email         string  `json:"email,omitempty"`
	PhotoURL      string  `json:"photo_url,omitempty"`
	Role          string  `json:"role"`
	Status        string  `json:"status"`
	IsDriver      bool    `json:"is_driver"`
	RatingAverage float64 `json:"rating_average"`
	CreatedAt     string  `json:"created_at"`
}

// AdminUserListResponse is a page of users.
type AdminUserListResponse struct {
	Items      []AdminUserListItem `json:"items"`
	Pagination Pagination          `json:"pagination"`
}

// AdminUserDetailResponse is a user's detail page: their account plus a
// summary of their bookings (as a passenger) and their driver profile if
// they have one.
type AdminUserDetailResponse struct {
	AdminUserListItem
	TotalTripsAsPassenger     int64                `json:"total_trips_as_passenger"`
	CompletedTripsAsPassenger int64                `json:"completed_trips_as_passenger"`
	DriverProfile             *AdminDriverListItem `json:"driver_profile,omitempty"`
}

// AdminTripListItem is one row of the bookings list.
type AdminTripListItem struct {
	ID                 string `json:"id"`
	Status             string `json:"status"`
	VehicleType        string `json:"vehicle_type"`
	PassengerID        string `json:"passenger_id"`
	PassengerName      string `json:"passenger_name"`
	DriverID           string `json:"driver_id,omitempty"`
	DriverName         string `json:"driver_name,omitempty"`
	PickupAddress      string `json:"pickup_address,omitempty"`
	DestinationAddress string `json:"destination_address,omitempty"`
	OfferedPriceKobo   int64  `json:"offered_price_kobo"`
	AgreedPriceKobo    *int64 `json:"agreed_price_kobo,omitempty"`
	CreatedAt          string `json:"created_at"`
	CompletedAt        string `json:"completed_at,omitempty"`
}

// AdminTripListResponse is a page of bookings.
type AdminTripListResponse struct {
	Items      []AdminTripListItem `json:"items"`
	Pagination Pagination          `json:"pagination"`
}

// AdminDriverLocationItem is one online driver's live position, for the
// "riders by location" map.
type AdminDriverLocationItem struct {
	DriverID    string  `json:"driver_id"`
	Name        string  `json:"name"`
	VehicleType string  `json:"vehicle_type"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
}

// AdminUpdateAccountStatusRequest is the payload for
// PATCH /admin/users/:id/status — suspend, reactivate, or ban an account.
// Applies to both passenger-only and driver accounts, since it operates
// on the underlying User record.
type AdminUpdateAccountStatusRequest struct {
	Status string `json:"status" binding:"required,oneof=active suspended banned" example:"suspended"`
	Reason string `json:"reason" binding:"omitempty,max=500"`
}

// AdminNotifyDriverRequest is the payload for POST /admin/drivers/:id/notify
// — sends the driver a message (SMS and/or email, whichever the account
// has) about their submitted documents, e.g. asking them to resubmit.
type AdminNotifyDriverRequest struct {
	Subject string `json:"subject" binding:"omitempty,max=150" example:"Update your driver documents"`
	Message string `json:"message" binding:"required,max=1000" example:"Your vehicle photo is unclear, please re-upload a clearer one."`
}

// AdminNotifyDriverResponse confirms which channel(s) the notification
// went out on.
type AdminNotifyDriverResponse struct {
	SentViaSMS   bool `json:"sent_via_sms"`
	SentViaEmail bool `json:"sent_via_email"`
}

// AdminSubscriptionPriceItem is one vehicle type's daily platform-access
// price.
type AdminSubscriptionPriceItem struct {
	VehicleType     string `json:"vehicle_type"`
	PriceKoboPerDay int64  `json:"price_kobo_per_day"`
	UpdatedAt       string `json:"updated_at"`
}

// AdminUpdateSubscriptionPriceRequest is the payload for
// PUT /admin/settings/subscription-prices/:vehicle_type.
type AdminUpdateSubscriptionPriceRequest struct {
	PriceKoboPerDay int64 `json:"price_kobo_per_day" binding:"required,min=0" example:"100000"`
}
