package dto

// UploadResponse is returned by POST /uploads — hand this URL to any
// endpoint expecting a document/photo URL (e.g. RegisterDriverRequest's
// VehiclePhotoURL/IDDocumentURL).
type UploadResponse struct {
	URL string `json:"url"`
}
