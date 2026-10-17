package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"ridematch-backend/internal/dto"
	"ridematch-backend/internal/storage"
	"ridematch-backend/internal/utils"
)

// maxUploadSizeBytes caps a single upload at 8MB — comfortably more than
// a phone-camera photo needs once the client compresses it, well short of
// a raw multi-megapixel original or an abusive payload.
const maxUploadSizeBytes = 8 << 20

// allowedUploadTypes are the content types RegisterDriverRequest's photo/
// document URLs actually need: a vehicle photo, or an ID document photo
// or scanned PDF.
var allowedUploadTypes = map[string]bool{
	"image/jpeg":      true,
	"image/png":       true,
	"image/webp":      true,
	"application/pdf": true,
}

// UploadHandler backs the one generic file-upload endpoint the app has —
// driver vehicle photos and ID documents, both just URLs everywhere else
// in the API (see dto.RegisterDriverRequest's doc comment).
type UploadHandler struct {
	store storage.Store
}

// NewUploadHandler constructs an UploadHandler.
func NewUploadHandler(store storage.Store) *UploadHandler {
	return &UploadHandler{store: store}
}

// Create godoc
//
//	@Summary		Upload a file
//	@Description	Uploads an image (JPEG/PNG/WebP) or PDF, up to 8MB, and returns its URL. Used for a driver's vehicle photo and ID document — pass the returned URL to POST /driver/register.
//	@Tags			Uploads
//	@Accept			multipart/form-data
//	@Produce		json
//	@Security		BearerAuth
//	@Param			file	formData	file	true	"The file to upload"
//	@Success		201		{object}	utils.APIResponse{data=dto.UploadResponse}
//	@Failure		400		{object}	utils.APIResponse
//	@Router			/uploads [post]
func (h *UploadHandler) Create(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUploadSizeBytes)

	fileHeader, err := c.FormFile("file")
	if err != nil {
		utils.Fail(c, http.StatusBadRequest, "a \"file\" form field is required (max 8MB)")
		return
	}

	contentType := fileHeader.Header.Get("Content-Type")
	if !allowedUploadTypes[contentType] {
		utils.Fail(c, http.StatusBadRequest, "unsupported file type — use JPEG, PNG, WebP, or PDF")
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		utils.Fail(c, http.StatusBadRequest, "couldn't read the uploaded file")
		return
	}
	defer file.Close()

	url, err := h.store.Save(c.Request.Context(), fileHeader.Filename, file, fileHeader.Size, contentType)
	if err != nil {
		utils.Fail(c, http.StatusInternalServerError, "failed to store the uploaded file")
		return
	}

	utils.Success(c, http.StatusCreated, "file uploaded", dto.UploadResponse{URL: url})
}
