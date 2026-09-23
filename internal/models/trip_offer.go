package models

// TripOfferStatus tracks one driver's proposed price for one trip.
type TripOfferStatus string

const (
	OfferPending    TripOfferStatus = "pending"
	OfferAccepted   TripOfferStatus = "accepted"
	OfferRejected   TripOfferStatus = "rejected"
	OfferSuperseded TripOfferStatus = "superseded" // trip matched to a different driver
	OfferWithdrawn  TripOfferStatus = "withdrawn"  // trip cancelled/expired while pending
)

// TripOffer is a driver's proposed price for a passenger's open trip
// request. A driver may have at most one pending offer per trip — placing
// a new one supersedes their own previous pending offer.
type TripOffer struct {
	Base

	TripID    string          `gorm:"type:char(36);index:idx_trip_status,priority:1;not null" json:"trip_id"`
	DriverID  string          `gorm:"type:char(36);index;not null" json:"driver_id"`
	PriceKobo int64           `gorm:"not null" json:"price_kobo"`
	Status    TripOfferStatus `gorm:"type:varchar(20);index:idx_trip_status,priority:2;not null;default:'pending'" json:"status"`

	Driver *DriverProfile `gorm:"foreignKey:DriverID" json:"-"`
}

func (TripOffer) TableName() string {
	return "trip_offers"
}
