package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// GetTicker returns the current ticker for a symbol.
func (h *Handler) GetTicker(c *gin.Context) {
	ctx := c.Request.Context()
	symbol := c.Param("symbol")
	if symbol == "" {
		errResp(c, http.StatusBadRequest, "symbol is required")
		return
	}

	ticker, err := h.deps.Exchange.GetTicker(ctx, symbol)
	if err != nil {
		errResp(c, http.StatusInternalServerError, "failed to get ticker")
		return
	}
	ok(c, ticker)
}
