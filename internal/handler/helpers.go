package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"ridematch-backend/internal/dto"
	"ridematch-backend/internal/middleware"
	"ridematch-backend/internal/service"
	"ridematch-backend/internal/utils"
)

func actorFrom(c *gin.Context) service.Actor {
	role, _ := c.Get(middleware.ContextRole)
	r, _ := role.(string)
	return service.Actor{UserID: middleware.UserIDFromContext(c), Role: r, IP: c.ClientIP()}
}

func pageParams(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.Query("page"))
	size, _ := strconv.Atoi(c.Query("page_size"))
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	return page, size
}

// bind parses a JSON body, answering 400 with msg on failure.
func bind(c *gin.Context, req interface{}, msg string) bool {
	if err := c.ShouldBindJSON(req); err != nil {
		utils.Fail(c, http.StatusBadRequest, msg)
		return false
	}
	return true
}

func writePage[T any](c *gin.Context, message string, items []T, total int64, page, size int, err error) {
	if err != nil {
		handleServiceError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, message, dto.NewPage(items, page, size, total))
}
