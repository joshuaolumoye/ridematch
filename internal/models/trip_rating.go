package models

// RaterRole distinguishes which side of a completed trip submitted a
// given rating — a trip has at most one rating from its passenger and one
// from its driver, never two from the same side.
type RaterRole string

const (
	RaterPassenger RaterRole = "passenger"
	RaterDriver    RaterRole = "driver"
)

// TripRating is one party's 1-5 rating of the other, submitted after a
// trip completes. RaterUserID is always the account that submitted the
// rating; the target (the other party) is derived from the trip itself
// rather than stored redundantly here.
type TripRating struct {
	Base

	TripID      string    `gorm:"type:char(36);uniqueIndex:idx_trip_rater;not null" json:"trip_id"`
	RaterUserID string    `gorm:"type:char(36);not null;index" json:"rater_user_id"`
	RaterRole   RaterRole `gorm:"type:varchar(20);uniqueIndex:idx_trip_rater;not null" json:"rater_role"`

	Value   int    `gorm:"not null" json:"value"`
	Comment string `gorm:"type:varchar(500)" json:"comment,omitempty"`
}

func (TripRating) TableName() string {
	return "trip_ratings"
}
