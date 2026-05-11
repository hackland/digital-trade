package handler

import (
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jayce/btc-trader/internal/market"
	"github.com/jayce/btc-trader/internal/strategy/trend"
)

// PositionAnalysis is the full analysis result for a single open position.
type PositionAnalysis struct {
	Symbol string `json:"symbol"`

	// Position basics
	EntryPrice    float64 `json:"entry_price"`
	CurrentPrice  float64 `json:"current_price"`
	Quantity      float64 `json:"quantity"`
	UnrealizedPnL float64 `json:"unrealized_pnl"`
	UnrealizedPct float64 `json:"unrealized_pct"`

	// Key price levels (for chart overlay)
	StopPrice   float64 `json:"stop_price"`
	DailyEMA50  float64 `json:"daily_ema50"`
	DailyEMA200 float64 `json:"daily_ema200"`

	// Distances
	DistToStopPct   float64 `json:"dist_to_stop_pct"`   // % from current price to stop (negative = already below)
	DistToEMA200Pct float64 `json:"dist_to_ema200_pct"` // % from current price to EMA200

	// Strategy state
	CompositeScore float64 `json:"composite_score"`
	SellThreshold  float64 `json:"sell_threshold"`
	ScoreMargin    float64 `json:"score_margin"` // score - sell_threshold (positive = safe)
	BarsSinceEntry int     `json:"bars_since_entry"`
	HoldReason     string  `json:"hold_reason"`

	// Market
	Regime      string `json:"regime"`
	RegimeLabel string `json:"regime_label"`
	HTFBullish  bool   `json:"htf_bullish"`
	HTFBlocked  bool   `json:"htf_blocked"`

	// Multi-dimensional risk breakdown
	Dimensions []RiskDimension `json:"dimensions"`

	// Overall verdict
	RiskLevel      string `json:"risk_level"`     // "low" | "medium" | "high" | "critical"
	Recommendation string `json:"recommendation"` // "hold" | "watch" | "consider_close" | "close_now"
	ReasonSummary  string `json:"reason_summary"`
}

// RiskDimension is one axis of the risk assessment.
type RiskDimension struct {
	Name   string `json:"name"`
	Status string `json:"status"` // "ok" | "warning" | "danger"
	Value  string `json:"value"`
	Detail string `json:"detail"`
}

// GetPositionAnalysis returns a full risk analysis for an open position.
// GET /api/v1/positions/:symbol/analysis
func (h *Handler) GetPositionAnalysis(c *gin.Context) {
	ctx := c.Request.Context()
	symbol := c.Param("symbol")

	// ── 1. Position ──────────────────────────────────────────────────────────
	pos := h.deps.Position.GetPosition(symbol)
	if pos == nil || pos.Quantity <= 0 {
		errResp(c, http.StatusNotFound, "no open position for "+symbol)
		return
	}

	ticker, err := h.deps.Exchange.GetTicker(ctx, symbol)
	if err != nil {
		errResp(c, http.StatusInternalServerError, "failed to get ticker")
		return
	}
	currentPrice := ticker.LastPrice

	// ── 2. Strategy diagnostics ───────────────────────────────────────────────
	var compositeScore, sellThreshold, stopPrice float64
	var barsSinceEntry int
	var holdReason string
	var htfBullish, htfBlocked bool

	if cw, ok := h.deps.Strategy.(*trend.CustomWeightedStrategy); ok {
		if diag := cw.GetDiagnostics(); diag != nil {
			compositeScore = diag.CompositeScore
			sellThreshold = diag.SellThreshold
			stopPrice = diag.StopPrice
			barsSinceEntry = diag.BarsSinceEntry
			holdReason = diag.HoldReason
			htfBullish = diag.HTFBullish
			htfBlocked = diag.HTFBlocked
		}
	}

	// ── 3. Daily EMA50/EMA200 ─────────────────────────────────────────────────
	ic := market.NewIndicatorComputer()
	dailyStart := time.Now().Add(-210 * 24 * time.Hour)
	dailyKlines, _ := h.deps.Store.GetKlines(ctx, symbol, "1d", dailyStart, time.Now(), 210)
	var ema50, ema200 float64
	if len(dailyKlines) >= 50 {
		closes := make([]float64, len(dailyKlines))
		for i, k := range dailyKlines {
			closes[i] = k.Close
		}
		ema50 = ic.ComputeEMA(closes, 50)
		ema200 = ic.ComputeEMA(closes, 200)
	}

	// ── 4. Regime ─────────────────────────────────────────────────────────────
	regime, regimeLabel := computeRegimeFromEMA(currentPrice, ema200)

	// ── 5. Compute dimensions ─────────────────────────────────────────────────
	analysis := buildAnalysis(PositionAnalysis{
		Symbol:         symbol,
		EntryPrice:     pos.AvgEntryPrice,
		CurrentPrice:   currentPrice,
		Quantity:       pos.Quantity,
		StopPrice:      stopPrice,
		DailyEMA50:     ema50,
		DailyEMA200:    ema200,
		CompositeScore: compositeScore,
		SellThreshold:  sellThreshold,
		BarsSinceEntry: barsSinceEntry,
		HoldReason:     holdReason,
		Regime:         regime,
		RegimeLabel:    regimeLabel,
		HTFBullish:     htfBullish,
		HTFBlocked:     htfBlocked,
	})

	ok(c, analysis)
}

// BuildAnalysis is exported so the alert monitor can reuse it without HTTP.
func BuildAnalysis(a PositionAnalysis) PositionAnalysis {
	return buildAnalysis(a)
}

func buildAnalysis(a PositionAnalysis) PositionAnalysis {
	// PnL
	a.UnrealizedPnL = (a.CurrentPrice - a.EntryPrice) * a.Quantity
	a.UnrealizedPct = (a.CurrentPrice/a.EntryPrice - 1) * 100

	// Distances
	if a.StopPrice > 0 {
		a.DistToStopPct = (a.CurrentPrice/a.StopPrice - 1) * 100
	}
	if a.DailyEMA200 > 0 {
		a.DistToEMA200Pct = (a.CurrentPrice/a.DailyEMA200 - 1) * 100
	}
	a.ScoreMargin = a.CompositeScore - a.SellThreshold

	// ── Dimensions ────────────────────────────────────────────────────────────
	dims := []RiskDimension{}

	// 1. ATR 止损距离
	if a.StopPrice > 0 {
		if a.CurrentPrice <= a.StopPrice {
			dims = append(dims, RiskDimension{
				Name:   "ATR 止损",
				Status: "danger",
				Value:  fmt.Sprintf("%.2f (已跌破)", a.StopPrice),
				Detail: fmt.Sprintf("当前价低于止损线 %.2f%%，应立即止损", math.Abs(a.DistToStopPct)),
			})
		} else if a.DistToStopPct < 1.5 {
			dims = append(dims, RiskDimension{
				Name:   "ATR 止损",
				Status: "warning",
				Value:  fmt.Sprintf("%.2f (距离 %.1f%%)", a.StopPrice, a.DistToStopPct),
				Detail: "距止损线不足 1.5%，波动可能触发止损",
			})
		} else {
			dims = append(dims, RiskDimension{
				Name:   "ATR 止损",
				Status: "ok",
				Value:  fmt.Sprintf("%.2f (距离 %.1f%%)", a.StopPrice, a.DistToStopPct),
				Detail: "止损空间充足",
			})
		}
	}

	// 2. 策略评分
	if a.SellThreshold != 0 {
		scorePct := a.ScoreMargin / math.Abs(a.SellThreshold) * 100 // how far from sell threshold in %
		if a.CompositeScore <= a.SellThreshold {
			dims = append(dims, RiskDimension{
				Name:   "策略评分",
				Status: "danger",
				Value:  fmt.Sprintf("%.3f (卖出阈值 %.3f)", a.CompositeScore, a.SellThreshold),
				Detail: "评分已触及卖出阈值，策略信号为平仓",
			})
		} else if scorePct < 30 {
			dims = append(dims, RiskDimension{
				Name:   "策略评分",
				Status: "warning",
				Value:  fmt.Sprintf("%.3f (距阈值 %.0f%%)", a.CompositeScore, scorePct),
				Detail: "评分已接近卖出阈值，注意观察",
			})
		} else {
			dims = append(dims, RiskDimension{
				Name:   "策略评分",
				Status: "ok",
				Value:  fmt.Sprintf("%.3f", a.CompositeScore),
				Detail: "评分健康，距卖出阈值尚远",
			})
		}
	}

	// 3. 价格 vs 日线 EMA200
	if a.DailyEMA200 > 0 {
		if a.CurrentPrice < a.DailyEMA200 {
			detail := fmt.Sprintf("价格低于日线 EMA200 %.1f%%，处于中期空头结构", math.Abs(a.DistToEMA200Pct))
			dims = append(dims, RiskDimension{
				Name:   "日线 EMA200",
				Status: "warning",
				Value:  fmt.Sprintf("%.2f (当前低 %.1f%%)", a.DailyEMA200, math.Abs(a.DistToEMA200Pct)),
				Detail: detail,
			})
		} else {
			dims = append(dims, RiskDimension{
				Name:   "日线 EMA200",
				Status: "ok",
				Value:  fmt.Sprintf("%.2f (当前高 %.1f%%)", a.DailyEMA200, a.DistToEMA200Pct),
				Detail: "价格在日线 EMA200 上方，中期结构偏多",
			})
		}
	}

	// 4. 市场状态
	switch a.Regime {
	case "strong_bull":
		dims = append(dims, RiskDimension{Name: "市场状态", Status: "ok", Value: a.RegimeLabel, Detail: "宏观与中期趋势均偏多"})
	case "bear_bounce":
		dims = append(dims, RiskDimension{Name: "市场状态", Status: "warning", Value: a.RegimeLabel, Detail: "宏观偏空，当前为技术性反弹"})
	case "mid_bear":
		dims = append(dims, RiskDimension{Name: "市场状态", Status: "warning", Value: a.RegimeLabel, Detail: "中期熊市，持多仓风险偏高"})
	case "strong_bear":
		dims = append(dims, RiskDimension{Name: "市场状态", Status: "danger", Value: a.RegimeLabel, Detail: "宏观与中期双空，强烈建议谨慎"})
	default:
		dims = append(dims, RiskDimension{Name: "市场状态", Status: "ok", Value: a.RegimeLabel, Detail: ""})
	}

	// 5. 大周期过滤
	if a.HTFBlocked {
		dims = append(dims, RiskDimension{
			Name:   "大周期过滤",
			Status: "warning",
			Value:  "被拦截",
			Detail: "大周期 EMA 方向偏空，策略不会追加买入",
		})
	} else {
		dims = append(dims, RiskDimension{
			Name:   "大周期过滤",
			Status: "ok",
			Value:  "通过",
			Detail: "大周期方向与持仓一致",
		})
	}

	// 6. 持仓时长
	if a.BarsSinceEntry > 0 {
		if a.BarsSinceEntry > 48 && a.UnrealizedPnL <= 0 {
			dims = append(dims, RiskDimension{
				Name:   "持仓时长",
				Status: "warning",
				Value:  fmt.Sprintf("%d 根K线", a.BarsSinceEntry),
				Detail: "持仓超 48 根 K 线仍未盈利，资金效率低",
			})
		} else {
			dims = append(dims, RiskDimension{
				Name:   "持仓时长",
				Status: "ok",
				Value:  fmt.Sprintf("%d 根K线", a.BarsSinceEntry),
				Detail: "持仓时长正常",
			})
		}
	}

	a.Dimensions = dims

	// ── Overall risk level ────────────────────────────────────────────────────
	dangerCount, warningCount := 0, 0
	for _, d := range dims {
		switch d.Status {
		case "danger":
			dangerCount++
		case "warning":
			warningCount++
		}
	}

	switch {
	case dangerCount >= 2 || (a.StopPrice > 0 && a.CurrentPrice <= a.StopPrice):
		a.RiskLevel = "critical"
		a.Recommendation = "close_now"
		a.ReasonSummary = "多项危险指标触发，建议立即平仓止损"
	case dangerCount == 1 || (dangerCount == 0 && warningCount >= 3):
		a.RiskLevel = "high"
		a.Recommendation = "consider_close"
		a.ReasonSummary = "风险较高，建议考虑平仓或设置更紧的止损"
	case warningCount >= 2:
		a.RiskLevel = "medium"
		a.Recommendation = "watch"
		a.ReasonSummary = "存在多个风险信号，密切关注价格走势"
	default:
		a.RiskLevel = "low"
		a.Recommendation = "hold"
		a.ReasonSummary = "各项指标正常，可继续持有"
	}

	return a
}

// computeRegimeFromEMA returns regime/label based on price vs daily EMA200.
// Simplified version without weekly (used when weekly data unavailable).
func computeRegimeFromEMA(price, ema200 float64) (string, string) {
	if ema200 == 0 {
		return "unknown", "未知"
	}
	if price > ema200 {
		return "strong_bull", "强牛市"
	}
	return "mid_bear", "中期熊市"
}
