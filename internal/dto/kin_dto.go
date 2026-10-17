package dto

import "time"

// NextOfKinRequest saves or replaces the driver's next of kin.
type NextOfKinRequest struct {
	FullName     string `json:"full_name" binding:"required,min=3,max=150"`
	Relationship string `json:"relationship" binding:"required,min=2,max=50"`
	Phone        string `json:"phone" binding:"required"`
	Email        string `json:"email" binding:"omitempty,email"`
	Address      string `json:"address" binding:"omitempty,max=255"`
	OTP          string `json:"otp" binding:"required"`
}

// NextOfKinResponse is the driver's saved next of kin.
type NextOfKinResponse struct {
	FullName     string    `json:"full_name"`
	Relationship string    `json:"relationship"`
	Phone        string    `json:"phone"`
	Email        string    `json:"email,omitempty"`
	Address      string    `json:"address,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// AppealCodeRequest asks for a code to be sent to the next of kin's phone.
type AppealCodeRequest struct {
	DriverIdentifier string `json:"driver_identifier" binding:"required"` // the driver's phone or email
	NOKPhone         string `json:"nok_phone" binding:"required"`
}

// SubmitAppealRequest files a withdrawal appeal.
type SubmitAppealRequest struct {
	DriverIdentifier string `json:"driver_identifier" binding:"required"`
	NOKPhone         string `json:"nok_phone" binding:"required"`
	OTP              string `json:"otp" binding:"required"`
	Reason           string `json:"reason" binding:"required,oneof=deceased incapacitated missing other"`
	Details          string `json:"details" binding:"required,min=20,max=3000"`
	BankCode         string `json:"bank_code" binding:"required"`
	AccountNumber    string `json:"account_number" binding:"required,len=10,numeric"`
}

// SubmitAppealResponse carries the one-time appeal token.
type SubmitAppealResponse struct {
	AppealID string `json:"appeal_id"`
	// Token is shown ONCE: it lets the next of kin upload documents and
	// check progress. It cannot be recovered.
	Token string `json:"token"`
}

// AppealDocumentRequest attaches an uploaded file to an appeal.
type AppealDocumentRequest struct {
	Kind string `json:"kind" binding:"required,oneof=death_certificate next_of_kin_id driver_id letter other"`
	URL  string `json:"url" binding:"required,url"`
}

// AppealDocumentResponse is an attached document.
type AppealDocumentResponse struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"created_at"`
}

// PublicAppealStatusResponse is what a next of kin sees with their token.
type PublicAppealStatusResponse struct {
	AppealID    string                   `json:"appeal_id"`
	Status      string                   `json:"status"`
	StatusNote  string                   `json:"status_note"`
	InfoRequest string                   `json:"info_request,omitempty"`
	Documents   []AppealDocumentResponse `json:"documents"`
	CreatedAt   time.Time                `json:"created_at"`
}

// DriverAppealResponse is shown to the driver when an appeal is filed in
// their name, so they can cancel it if it is not legitimate.
type DriverAppealResponse struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	Reason    string    `json:"reason"`
	NOKName   string    `json:"nok_name"`
	CreatedAt time.Time `json:"created_at"`
}

// CancelAppealRequest confirms the driver is cancelling with their PIN.
type CancelAppealRequest struct {
	PIN string `json:"pin" binding:"required"`
}

// AdminAppealResponse is the full appeal for staff.
type AdminAppealResponse struct {
	ID                string           `json:"id"`
	Status            string           `json:"status"`
	Reason            string           `json:"reason"`
	Details           string           `json:"details"`
	DriverUserID      string           `json:"driver_user_id"`
	DriverName        string           `json:"driver_name"`
	DriverPhone       string           `json:"driver_phone,omitempty"`
	NOKName           string           `json:"nok_name"`
	NOKPhone          string           `json:"nok_phone"`
	NOKRelationship   string           `json:"nok_relationship,omitempty"`
	DestBankName      string           `json:"dest_bank_name"`
	DestAccountNumber string           `json:"dest_account_number"`
	DestAccountName   string           `json:"dest_account_name"`
	WalletBalanceKobo int64            `json:"wallet_balance_kobo"`
	DriverNotifiedAt  *time.Time       `json:"driver_notified_at,omitempty"`
	RecommendedBy     string           `json:"recommended_by,omitempty"`
	ApprovedBy        string           `json:"approved_by,omitempty"`
	DisbursableAfter  *time.Time       `json:"disbursable_after,omitempty"`
	InfoRequest       string           `json:"info_request,omitempty"`
	RejectionReason   string           `json:"rejection_reason,omitempty"`
	AdminNotes        string           `json:"admin_notes,omitempty"`
	AmountKobo        int64            `json:"amount_kobo,omitempty"`
	Documents         []AdminAppealDoc `json:"documents"`
	CreatedAt         time.Time        `json:"created_at"`
}

// AdminAppealDoc is a document with a viewable URL.
type AdminAppealDoc = AppealDocumentResponse
