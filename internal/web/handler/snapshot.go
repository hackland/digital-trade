package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// GetSnapshots returns account snapshots within a time range.
func (h *Handler) GetSnapshots(c *gin.Context) {
	ctx := c.Request.Context()

	start := time.Now().Add(-24 * time.Hour)
	end := time.Now()
	interval := c.DefaultQuery("interval", "5m")

	if s := c.Query("start"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			start = t
		}
	}
	if s := c.Query("end"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			end = t
		}
	}

	snapshots, err := h.deps.Store.GetSnapshots(ctx, start, end, interval)
	if err != nil {
		errResp(c, http.StatusInternalServerError, "failed to query snapshots")
		return
	}
	ok(c, snapshots)
}
