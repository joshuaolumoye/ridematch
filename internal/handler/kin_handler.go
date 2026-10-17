package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"ridematch-backend/internal/dto"
	"ridematch-backend/internal/service"
	"ridematch-backend/internal/utils"
)

// KinHandler exposes next of kin, the public appeal flow and admin review.
type KinHandler struct {
	kin *service.KinService
}

// NewKinHandler constructs a KinHandler.
func NewKinHandler(k *service.KinService) *KinHandler { return &KinHandler{kin: k} }

// ---- driver ----

func (h *KinHandler) Get(c *gin.Context) {
	resp, err := h.kin.Get(c.Request.Context(), uid(c))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "next of kin loaded", resp)
}

func (h *KinHandler) Code(c *gin.Context) {
	resp, err := h.kin.RequestCode(c.Request.Context(), uid(c))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "security code sent", resp)
}

func (h *KinHandler) Save(c *gin.Context) {
	var req dto.NextOfKinRequest
	if !bind(c, &req, "name, relationship, a valid phone number and the security code are required") {
		return
	}
	resp, err := h.kin.Save(c.Request.Context(), uid(c), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "next of kin saved", resp)
}

func (h *KinHandler) MyAppeals(c *gin.Context) {
	resp, err := h.kin.DriverAppeals(c.Request.Context(), uid(c))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "appeals loaded", resp)
}

func (h *KinHandler) CancelAppeal(c *gin.Context) {
	var req dto.CancelAppealRequest
	if !bind(c, &req, "your PIN is required") {
		return
	}
	if err := h.kin.DriverCancel(c.Request.Context(), uid(c), c.Param("id"), req.PIN); err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "appeal cancelled", nil)
}

// ---- public (no account) ----

// PublicCode always answers the same way so it can't be used to find out who
// is a driver or who their next of kin is.
func (h *KinHandler) PublicCode(c *gin.Context) {
	var req dto.AppealCodeRequest
	if !bind(c, &req, "the driver's phone or email and your phone number are required") {
		return
	}
	h.kin.RequestAppealCode(c.Request.Context(), req)
	utils.Success(c, http.StatusOK, "if these details match our records, a code has been sent to the next of kin's phone", nil)
}

func (h *KinHandler) PublicSubmit(c *gin.Context) {
	var req dto.SubmitAppealRequest
	if !bind(c, &req, "please complete every field (details need at least 20 characters)") {
		return
	}
	resp, err := h.kin.SubmitAppeal(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusCreated, "appeal received — save your token, it is shown only once", resp)
}

func (h *KinHandler) PublicStatus(c *gin.Context) {
	resp, err := h.kin.StatusByToken(c.Request.Context(), c.GetHeader("X-Appeal-Token"))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "appeal loaded", resp)
}

func (h *KinHandler) PublicUpload(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUploadSizeBytes)
	fh, err := c.FormFile("file")
	if err != nil {
		utils.Fail(c, http.StatusBadRequest, "a \"file\" form field is required (max 8MB)")
		return
	}
	ct := fh.Header.Get("Content-Type")
	if !allowedUploadTypes[ct] {
		utils.Fail(c, http.StatusBadRequest, "unsupported file type — use JPEG, PNG, WebP, or PDF")
		return
	}
	kind := c.PostForm("kind")
	switch kind {
	case "death_certificate", "next_of_kin_id", "driver_id", "letter", "other":
	default:
		utils.Fail(c, http.StatusBadRequest, "kind must be one of death_certificate, next_of_kin_id, driver_id, letter, other")
		return
	}
	f, err := fh.Open()
	if err != nil {
		utils.Fail(c, http.StatusBadRequest, "couldn't read the uploaded file")
		return
	}
	defer f.Close()
	resp, err := h.kin.UploadDocument(c.Request.Context(), c.GetHeader("X-Appeal-Token"), kind, fh.Filename, f, fh.Size, ct)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusCreated, "document uploaded", resp)
}

// ---- admin ----

func (h *KinHandler) AdminList(c *gin.Context) {
	page, size := pageParams(c)
	items, total, err := h.kin.AdminList(c.Request.Context(), c.Query("status"), page, size)
	writePage(c, "appeals loaded", items, total, page, size, err)
}

func (h *KinHandler) AdminGet(c *gin.Context) {
	resp, err := h.kin.AdminGet(c.Request.Context(), actorFrom(c), c.Param("id"))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "appeal loaded", resp)
}

func (h *KinHandler) AdminDecide(c *gin.Context) {
	var req dto.AdminDecisionRequest
	if !bind(c, &req, "a decision and a note (3–500 characters) are required") {
		return
	}
	resp, err := h.kin.AdminDecide(c.Request.Context(), actorFrom(c), c.Param("id"), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "appeal updated", resp)
}
