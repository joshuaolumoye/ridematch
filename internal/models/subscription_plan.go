package models

import "time"

// BillingCycle is how often a driver pays for platform access. Replaces
// the old "pick any number of days" model with a fixed, admin-curated
// set of cycles — each vehicle type can have any subset of these turned
// on, with its own price.
type BillingCycle string

const (
	BillingDaily   BillingCycle = "daily"
	BillingWeekly  BillingCycle = "weekly"
	BillingMonthly BillingCycle = "monthly"
	BillingYearly  BillingCycle = "yearly"
)

// AllBillingCycles is every cycle that can exist for a vehicle type — the
// fixed set admins toggle on/off, in display order.
var AllBillingCycles = []BillingCycle{BillingDaily, BillingWeekly, BillingMonthly, BillingYearly}

// Days returns how many days of platform access one unit of this cycle
// grants — used to extend DriverProfile.SubscriptionActiveUntil the same
// way a manual admin override does.
func (c BillingCycle) Days() int {
	switch c {
	case BillingDaily:
		return 1
	case BillingWeekly:
		return 7
	case BillingMonthly:
		return 30
	case BillingYearly:
		return 365
	default:
		return 1
	}
}

// IsValidBillingCycle reports whether a string is one of the four known
// cycles.
func IsValidBillingCycle(cycle string) bool {
	switch BillingCycle(cycle) {
	case BillingDaily, BillingWeekly, BillingMonthly, BillingYearly:
		return true
	default:
		return false
	}
}

// SubscriptionPlan is the admin-configurable platform-access price for
// one (vehicle type, billing cycle) pair — e.g. "okada, weekly" might be
// ₦3,500 and enabled, while "okada, yearly" is disabled until an admin
// turns it on and sets a price. A driver's app only ever offers the
// cycles that are both enabled and have a price set for their own
// vehicle type; if only "yearly" is enabled for car, a car driver sees
// exactly one plan to choose from.
type SubscriptionPlan struct {
	VehicleType  VehicleType  `gorm:"type:varchar(20);primaryKey" json:"vehicle_type"`
	BillingCycle BillingCycle `gorm:"type:varchar(10);primaryKey" json:"billing_cycle"`
	Enabled      bool         `gorm:"not null;default:false" json:"enabled"`
	PriceKobo    int64        `gorm:"not null;default:0" json:"price_kobo"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

func (SubscriptionPlan) TableName() string {
	return "subscription_plans"
}
