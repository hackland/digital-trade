package handler

import "github.com/gin-gonic/gin"

// GetRiskStatus returns the current risk management state.
func (h *Handler) GetRiskStatus(c *gin.Context) {
	status := h.deps.Risk.GetStatus()
	ok(c, status)
}
