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

// OpportunityAnalysis is the analysis result when there is no open position,
// answering "should I buy now?" based on current strategy diagnostics.
type OpportunityAnalysis struct {
	Symbol       string  `json:"symbol"`
	CurrentPrice float64 `json:"current_price"`

	// Key levels
	DailyEMA50  float64 `json:"daily_ema50"`
	DailyEMA200 float64 `json:"daily_ema200"`

	// Distances (% from current)
	DistToEMA50Pct  float64 `json:"dist_to_ema50_pct"`
	DistToEMA200Pct float64 `json:"dist_to_ema200_pct"`

	// Strategy state
	CompositeScore float64 `json:"composite_score"`
	BuyThreshold   float64 `json:"buy_threshold"`
	ScoreGap       float64 `json:"score_gap"` // buy_threshold - score (positive = need higher score to buy)

	// Market
	Regime      string `json:"regime"`
	RegimeLabel string `json:"regime_label"`
	HTFBullish  bool   `json:"htf_bullish"`
	HTFBlocked  bool   `json:"htf_blocked"`

	// Cooldown
	CooldownCount int `json:"cooldown_count"`
	CooldownBars  int `json:"cooldown_bars"`

	Dimensions []RiskDimension `json:"dimensions"`

	// Verdict
	Level          string `json:"level"`          // "good" | "neutral" | "caution" | "avoid"
	Recommendation string `json:"recommendation"` // "strong_buy" | "consider_buy" | "wait" | "avoid"
	ReasonSummary  string `json:"reason_summary"`
}

// GetOpportunityAnalysis answers "should I buy this symbol now?"
// GET /api/v1/market/opportunity?symbol=BTCUSDT
func (h *Handler) GetOpportunityAnalysis(c *gin.Context) {
	ctx := c.Request.Context()
	symbol := c.Query("symbol")
	if symbol == "" {
		errResp(c, http.StatusBadRequest, "symbol required")
		return
	}

	ticker, err := h.deps.Exchange.GetTicker(ctx, symbol)
	if err != nil {
		errResp(c, http.StatusInternalServerError, "failed to get ticker")
		return
	}
	currentPrice := ticker.LastPrice

	a := OpportunityAnalysis{Symbol: symbol, CurrentPrice: currentPrice}

	// Strategy diagnostics
	if cw, ok := h.deps.Strategy.(*trend.CustomWeightedStrategy); ok {
		if d := cw.GetDiagnostics(); d != nil {
			a.CompositeScore = d.CompositeScore
			a.BuyThreshold = d.BuyThreshold
			a.HTFBullish = d.HTFBullish
			a.HTFBlocked = d.HTFBlocked
			a.CooldownCount = d.CooldownCount
			a.CooldownBars = d.CooldownBars
		}
	}
	a.ScoreGap = a.BuyThreshold - a.CompositeScore

	// Daily EMAs
	ic := market.NewIndicatorComputer()
	dailyStart := time.Now().Add(-210 * 24 * time.Hour)
	dailyKlines, _ := h.deps.Store.GetKlines(ctx, symbol, "1d", dailyStart, time.Now(), 210)
	if len(dailyKlines) >= 50 {
		closes := make([]float64, len(dailyKlines))
		for i, k := range dailyKlines {
			closes[i] = k.Close
		}
		a.DailyEMA50 = ic.ComputeEMA(closes, 50)
		a.DailyEMA200 = ic.ComputeEMA(closes, 200)
	}
	if a.DailyEMA50 > 0 {
		a.DistToEMA50Pct = (currentPrice/a.DailyEMA50 - 1) * 100
	}
	if a.DailyEMA200 > 0 {
		a.DistToEMA200Pct = (currentPrice/a.DailyEMA200 - 1) * 100
	}

	a.Regime, a.RegimeLabel = computeRegimeFromEMA(currentPrice, a.DailyEMA200)

	// Build dimensions
	dims := []RiskDimension{}

	// 1. 策略评分 vs 买入阈值
	if a.BuyThreshold != 0 {
		gapPct := a.ScoreGap / math.Abs(a.BuyThreshold) * 100
		switch {
		case a.CompositeScore >= a.BuyThreshold:
			dims = append(dims, RiskDimension{
				Name:   "策略评分",
				Status: "ok",
				Value:  fmt.Sprintf("%.3f (≥ 阈值 %.3f)", a.CompositeScore, a.BuyThreshold),
				Detail: "已满足买入阈值，策略将在下一根 K 线考虑入场",
			})
		case gapPct < 30:
			dims = append(dims, RiskDimension{
				Name:   "策略评分",
				Status: "warning",
				Value:  fmt.Sprintf("%.3f (差 %.0f%% 到阈值)", a.CompositeScore, gapPct),
				Detail: "评分接近买入阈值，可能即将入场",
			})
		default:
			dims = append(dims, RiskDimension{
				Name:   "策略评分",
				Status: "danger",
				Value:  fmt.Sprintf("%.3f (差 %.0f%% 到阈值 %.3f)", a.CompositeScore, gapPct, a.BuyThreshold),
				Detail: "评分远离买入阈值，无入场信号",
			})
		}
	}

	// 2. 大周期过滤
	if a.HTFBlocked {
		dims = append(dims, RiskDimension{
			Name: "大周期过滤", Status: "danger", Value: "拦截",
			Detail: "周线 EMA 趋势偏空，策略不会买入",
		})
	} else {
		dims = append(dims, RiskDimension{
			Name: "大周期过滤", Status: "ok", Value: "通过",
			Detail: "周线趋势允许多头入场",
		})
	}

	// 3. 市场状态
	switch a.Regime {
	case "strong_bull":
		dims = append(dims, RiskDimension{Name: "市场状态", Status: "ok", Value: a.RegimeLabel, Detail: "宏观偏多，是布局多头的时机"})
	case "bear_bounce":
		dims = append(dims, RiskDimension{Name: "市场状态", Status: "warning", Value: a.RegimeLabel, Detail: "宏观偏空，反弹中追多风险高"})
	case "mid_bear":
		dims = append(dims, RiskDimension{Name: "市场状态", Status: "warning", Value: a.RegimeLabel, Detail: "中期熊市，多头胜率偏低"})
	case "strong_bear":
		dims = append(dims, RiskDimension{Name: "市场状态", Status: "danger", Value: a.RegimeLabel, Detail: "强空环境，应避免做多"})
	default:
		dims = append(dims, RiskDimension{Name: "市场状态", Status: "warning", Value: a.RegimeLabel, Detail: "数据不足，无法判定"})
	}

	// 4. 价格 vs EMA200
	if a.DailyEMA200 > 0 {
		if currentPrice > a.DailyEMA200 {
			dims = append(dims, RiskDimension{
				Name: "日线 EMA200", Status: "ok",
				Value:  fmt.Sprintf("%.2f (高 %.1f%%)", a.DailyEMA200, a.DistToEMA200Pct),
				Detail: "价格在中期均线上方，结构偏多",
			})
		} else {
			dims = append(dims, RiskDimension{
				Name: "日线 EMA200", Status: "warning",
				Value:  fmt.Sprintf("%.2f (低 %.1f%%)", a.DailyEMA200, math.Abs(a.DistToEMA200Pct)),
				Detail: "价格在中期均线下方，结构偏空",
			})
		}
	}

	// 5. 冷却期
	if a.CooldownBars > 0 && a.CooldownCount < a.CooldownBars {
		dims = append(dims, RiskDimension{
			Name: "冷却期", Status: "warning",
			Value:  fmt.Sprintf("%d/%d 根K线", a.CooldownCount, a.CooldownBars),
			Detail: "刚平仓后处于冷却期，策略暂不入场",
		})
	}

	a.Dimensions = dims

	// Verdict
	dangerCount, warningCount, okCount := 0, 0, 0
	for _, d := range dims {
		switch d.Status {
		case "danger":
			dangerCount++
		case "warning":
			warningCount++
		case "ok":
			okCount++
		}
	}

	scoreReady := a.BuyThreshold != 0 && a.CompositeScore >= a.BuyThreshold

	switch {
	case dangerCount >= 2:
		a.Level = "avoid"
		a.Recommendation = "avoid"
		a.ReasonSummary = "多项关键指标偏空，建议规避"
	case scoreReady && !a.HTFBlocked && (a.Regime == "strong_bull" || a.Regime == "bear_bounce"):
		a.Level = "good"
		a.Recommendation = "strong_buy"
		a.ReasonSummary = "策略评分达标且大周期允许，是较好的买入时机"
	case scoreReady && !a.HTFBlocked:
		a.Level = "good"
		a.Recommendation = "consider_buy"
		a.ReasonSummary = "策略评分达标但市场结构偏弱，可小仓位试探"
	case dangerCount >= 1 || warningCount >= 3:
		a.Level = "caution"
		a.Recommendation = "wait"
		a.ReasonSummary = "信号不充分，建议等待更好的入场点"
	default:
		a.Level = "neutral"
		a.Recommendation = "wait"
		a.ReasonSummary = "暂无明确入场信号，等待评分进一步靠近阈值"
	}

	ok(c, a)
}
