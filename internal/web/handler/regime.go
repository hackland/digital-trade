package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jayce/btc-trader/internal/market"
)

// RegimeResult describes the current market regime for a symbol.
type RegimeResult struct {
	Symbol string `json:"symbol"`

	// Daily layer
	Price       float64 `json:"price"`
	DailyEMA50  float64 `json:"daily_ema50"`
	DailyEMA200 float64 `json:"daily_ema200"`
	DailyBull   bool    `json:"daily_bull"` // price > ema50 > ema200

	// Weekly macro layer
	WeeklyEMA200     float64 `json:"weekly_ema200"`
	WeeklyEMA200Prev float64 `json:"weekly_ema200_prev"`
	WeeklyBull       bool    `json:"weekly_bull"` // ema200 slope pointing up

	// Combined verdict
	// "strong_bull" | "bull_pullback" | "strong_bear" | "bear_bounce" | "transition"
	Regime      string `json:"regime"`
	RegimeLabel string `json:"regime_label"`
}

// GetMarketRegime returns the bull/bear market regime for a symbol.
func (h *Handler) GetMarketRegime(c *gin.Context) {
	ctx := c.Request.Context()
	symbol := c.DefaultQuery("symbol", "BTCUSDT")

	ic := market.NewIndicatorComputer()

	// ── Daily layer: need 210 bars for EMA200 ─────────────────────────────────
	dailyStart := time.Now().Add(-210 * 24 * time.Hour)
	dailyKlines, err := h.deps.Store.GetKlines(ctx, symbol, "1d", dailyStart, time.Now(), 210)
	if err != nil || len(dailyKlines) < 50 {
		errResp(c, http.StatusInternalServerError, "insufficient daily kline data")
		return
	}

	dailyCloses := make([]float64, len(dailyKlines))
	for i, k := range dailyKlines {
		dailyCloses[i] = k.Close
	}

	price := dailyCloses[len(dailyCloses)-1]
	ema50 := ic.ComputeEMA(dailyCloses, 50)
	ema200 := ic.ComputeEMA(dailyCloses, 200)
	// Primary signal: price above/below daily EMA200
	dailyAboveEMA200 := price > ema200

	// ── Weekly macro layer: need 210 bars for EMA200 ──────────────────────────
	weeklyStart := time.Now().Add(-210 * 7 * 24 * time.Hour)
	weeklyKlines, _ := h.deps.Store.GetKlines(ctx, symbol, "1w", weeklyStart, time.Now(), 210)

	var weeklyEMA200, weeklyEMA200Prev float64
	// Weekly EMA200 needs 200+ bars; fall back to daily slope when unavailable
	weeklyBull := dailyAboveEMA200 // default: follow daily when no weekly data
	if len(weeklyKlines) >= 10 {
		weeklyCloses := make([]float64, len(weeklyKlines))
		for i, k := range weeklyKlines {
			weeklyCloses[i] = k.Close
		}
		weeklyEMA200 = ic.ComputeEMA(weeklyCloses, min(200, len(weeklyCloses)))
		if len(weeklyCloses) > 1 {
			weeklyEMA200Prev = ic.ComputeEMA(weeklyCloses[:len(weeklyCloses)-1], min(200, len(weeklyCloses)-1))
		} else {
			weeklyEMA200Prev = weeklyEMA200
		}
		weeklyBull = weeklyEMA200 >= weeklyEMA200Prev
	}

	// ── Combined verdict ──────────────────────────────────────────────────────
	regime, label := combineRegime(dailyAboveEMA200, weeklyBull)

	ok(c, RegimeResult{
		Symbol:           symbol,
		Price:            price,
		DailyEMA50:       ema50,
		DailyEMA200:      ema200,
		DailyBull:        dailyAboveEMA200,
		WeeklyEMA200:     weeklyEMA200,
		WeeklyEMA200Prev: weeklyEMA200Prev,
		WeeklyBull:       weeklyBull,
		Regime:           regime,
		RegimeLabel:      label,
	})
}

// combineRegime uses daily EMA200 position as primary signal,
// weekly EMA200 slope as macro context.
//
//	dailyAbove=true,  weeklyBull=true  → 强牛市   (结构牛 + 价格健康)
//	dailyAbove=true,  weeklyBull=false → 熊市反弹 (宏观偏空但短期突破)
//	dailyAbove=false, weeklyBull=true  → 中期熊市 (长线结构尚存，但已跌破日线EMA200)
//	dailyAbove=false, weeklyBull=false → 强熊市   (宏观 + 中期双空)
func combineRegime(dailyAboveEMA200, weeklyBull bool) (string, string) {
	switch {
	case dailyAboveEMA200 && weeklyBull:
		return "strong_bull", "强牛市"
	case dailyAboveEMA200 && !weeklyBull:
		return "bear_bounce", "熊市反弹"
	case !dailyAboveEMA200 && weeklyBull:
		return "mid_bear", "中期熊市"
	default:
		return "strong_bear", "强熊市"
	}
}
