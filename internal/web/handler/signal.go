package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jayce/btc-trader/internal/storage"
)

// GetSignals returns paginated signal history.
func (h *Handler) GetSignals(c *gin.Context) {
	ctx := c.Request.Context()
	filter := storage.SignalFilter{
		Symbol:       c.Query("symbol"),
		StrategyName: c.Query("strategy"),
		Action:       c.Query("action"),
		Limit:        parseIntDefault(c.Query("limit"), 20),
		Offset:       parseIntDefault(c.Query("offset"), 0),
	}
	if s := c.Query("start"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			filter.StartTime = &t
		}
	}
	if s := c.Query("end"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			filter.EndTime = &t
		}
	}

	signals, err := h.deps.Store.GetSignals(ctx, filter)
	if err != nil {
		errResp(c, http.StatusInternalServerError, "failed to query signals")
		return
	}
	paginated(c, signals, len(signals), filter.Limit, filter.Offset)
}
