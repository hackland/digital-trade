package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// GetRiskStatus returns the current risk management state.
func (h *Handler) GetRiskStatus(c *gin.Context) {
	status := h.deps.Risk.GetStatus()
	ok(c, status)
}

// GetRiskLimits returns the current runtime risk limit overrides.
func (h *Handler) GetRiskLimits(c *gin.Context) {
	ok(c, gin.H{
		"max_long_entry_price": h.deps.Risk.GetMaxLongEntryPrice(),
	})
}

// SetRiskLimits updates runtime risk limit overrides without restarting.
func (h *Handler) SetRiskLimits(c *gin.Context) {
	var body struct {
		MaxLongEntryPrice *float64 `json:"max_long_entry_price"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		errResp(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	if body.MaxLongEntryPrice != nil {
		h.deps.Risk.SetMaxLongEntryPrice(*body.MaxLongEntryPrice)
	}
	ok(c, gin.H{
		"max_long_entry_price": h.deps.Risk.GetMaxLongEntryPrice(),
	})
}
