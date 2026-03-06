package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

type paginatedResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
	Total   int         `json:"total"`
	Limit   int         `json:"limit"`
	Offset  int         `json:"offset"`
}

func ok(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, response{Code: 0, Message: "ok", Data: data})
}

func paginated(c *gin.Context, data interface{}, total, limit, offset int) {
	c.JSON(http.StatusOK, paginatedResponse{
		Code: 0, Message: "ok", Data: data,
		Total: total, Limit: limit, Offset: offset,
	})
}

func errResp(c *gin.Context, status int, msg string) {
	c.JSON(status, response{Code: -1, Message: msg})
}
