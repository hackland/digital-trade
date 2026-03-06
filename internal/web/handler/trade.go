package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jayce/btc-trader/internal/storage"
)

// GetTrades returns paginated trade history.
func (h *Handler) GetTrades(c *gin.Context) {
	ctx := c.Request.Context()
	filter := storage.TradeFilter{
		Symbol:       c.Query("symbol"),
		StrategyName: c.Query("strategy"),
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

	trades, err := h.deps.Store.GetTrades(ctx, filter)
	if err != nil {
		errResp(c, http.StatusInternalServerError, "failed to query trades")
		return
	}
	paginated(c, trades, len(trades), filter.Limit, filter.Offset)
}
