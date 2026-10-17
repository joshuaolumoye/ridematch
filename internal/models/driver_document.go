package models

// DocumentStatus tracks manual review of one submitted document,
// independent of the driver profile's overall VerificationStatus — an
// admin can see exactly which of several documents still needs a look.
type DocumentStatus string

const (
	DocumentPending  DocumentStatus = "pending"
	DocumentApproved DocumentStatus = "approved"
	DocumentRejected DocumentStatus = "rejected"
)

// DriverDocument is one file a driver submitted against one
// admin-defined DocumentType, during registration. A driver has at most
// one DriverDocument per DocumentType (uniqueIndex below) — resubmitting
// the same document type overwrites the previous file rather than
// piling up duplicates.
type DriverDocument struct {
	Base

	DriverID       string `gorm:"type:char(36);not null;uniqueIndex:idx_driver_doctype" json:"driver_id"`
	DocumentTypeID string `gorm:"type:char(36);not null;uniqueIndex:idx_driver_doctype" json:"document_type_id"`
	// FileURL points at private object storage, never a public URL — only
	// ever resolved to a short-lived signed URL for an admin reviewing it.
	FileURL string         `gorm:"type:varchar(500)" json:"-"`
	Status  DocumentStatus `gorm:"type:varchar(20);not null;default:'pending'" json:"status"`

	DocumentType *DocumentType `gorm:"foreignKey:DocumentTypeID" json:"-"`
}

func (DriverDocument) TableName() string {
	return "driver_documents"
}
