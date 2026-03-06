package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// defaultLookback returns how far back to query based on the kline interval
// and the number of candles requested (limit).
func defaultLookback(interval string, limit int) time.Duration {
	var perCandle time.Duration
	switch interval {
	case "1m":
		perCandle = time.Minute
	case "5m":
		perCandle = 5 * time.Minute
	case "15m":
		perCandle = 15 * time.Minute
	case "1h":
		perCandle = time.Hour
	case "4h":
		perCandle = 4 * time.Hour
	case "1d":
		perCandle = 24 * time.Hour
	default:
		perCandle = 5 * time.Minute
	}
	// Add 10% buffer so we don't miss edge candles
	return time.Duration(float64(perCandle) * float64(limit) * 1.1)
}

// GetKlines returns historical kline data.
func (h *Handler) GetKlines(c *gin.Context) {
	ctx := c.Request.Context()

	symbol := c.DefaultQuery("symbol", "BTCUSDT")
	interval := c.DefaultQuery("interval", "5m")
	limit := parseIntDefault(c.Query("limit"), 500)

	start := time.Now().Add(-defaultLookback(interval, limit))
	end := time.Now()

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

	klines, err := h.deps.Store.GetKlines(ctx, symbol, interval, start, end, limit)
	if err != nil {
		errResp(c, http.StatusInternalServerError, "failed to query klines")
		return
	}
	ok(c, klines)
}
