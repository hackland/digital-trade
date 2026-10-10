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

	// ── Daily layer ────────────────────────────────────────────────────────────
	// Pull the full available daily history (not just period+10ish bars) so
	// EMA200/EMA50 get properly warmed up — an EMA's smoothing constant is
	// tiny at period=200 (~0.01), so seeding from an SMA and then only
	// applying a handful of updates barely moves it off that seed; feeding
	// the whole history lets it actually converge, same as any charting tool.
	dailyStart := time.Now().Add(-20 * 365 * 24 * time.Hour)
	dailyKlines, err := h.deps.Store.GetKlines(ctx, symbol, "1d", dailyStart, time.Now(), 0)
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
	// Bullish stack: price above the fast MA, fast MA above the slow MA — not
	// just price vs EMA200 alone, which whipsaws more right around the line.
	dailyBullStack := ema50 > 0 && ema200 > 0 && price > ema50 && ema50 > ema200

	// ── Weekly macro layer ─────────────────────────────────────────────────────
	weeklyStart := time.Now().Add(-20 * 365 * 24 * time.Hour)
	weeklyKlines, _ := h.deps.Store.GetKlines(ctx, symbol, "1w", weeklyStart, time.Now(), 0)

	var weeklyEMA200, weeklyEMA200Prev float64
	// Weekly EMA200 needs 200+ bars; fall back to daily slope when unavailable
	weeklyBull := dailyBullStack // default: follow daily when no weekly data
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
	regime, label := combineRegime(dailyBullStack, weeklyBull)

	ok(c, RegimeResult{
		Symbol:           symbol,
		Price:            price,
		DailyEMA50:       ema50,
		DailyEMA200:      ema200,
		DailyBull:        dailyBullStack,
		WeeklyEMA200:     weeklyEMA200,
		WeeklyEMA200Prev: weeklyEMA200Prev,
		WeeklyBull:       weeklyBull,
		Regime:           regime,
		RegimeLabel:      label,
	})
}

// combineRegime uses the daily bullish MA stack (price > EMA50 > EMA200) as
// primary signal, weekly EMA200 slope as macro context.
//
//	dailyBull=true,  weeklyBull=true  → 强牛市   (结构牛 + 价格健康)
//	dailyBull=true,  weeklyBull=false → 熊市反弹 (宏观偏空但短期突破)
//	dailyBull=false, weeklyBull=true  → 中期熊市 (长线结构尚存，但日线排列已转弱)
//	dailyBull=false, weeklyBull=false → 强熊市   (宏观 + 中期双空)
func combineRegime(dailyBull, weeklyBull bool) (string, string) {
	switch {
	case dailyBull && weeklyBull:
		return "strong_bull", "强牛市"
	case dailyBull && !weeklyBull:
		return "bear_bounce", "熊市反弹"
	case !dailyBull && weeklyBull:
		return "mid_bear", "中期熊市"
	default:
		return "strong_bear", "强熊市"
	}
}
