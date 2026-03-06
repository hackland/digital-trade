package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jayce/btc-trader/internal/backtest"
	"github.com/jayce/btc-trader/internal/strategy"
	"github.com/jayce/btc-trader/internal/strategy/trend"
	"go.uber.org/zap"
)

// BacktestRequest is the JSON body for POST /api/v1/backtest.
type BacktestRequest struct {
	Symbol   string  `json:"symbol" binding:"required"`
	Interval string  `json:"interval" binding:"required"`
	Strategy string  `json:"strategy" binding:"required"`
	Days     int     `json:"days"`
	Start    string  `json:"start"` // YYYY-MM-DD
	End      string  `json:"end"`   // YYYY-MM-DD
	Cash     float64 `json:"cash"`
	Fee      float64 `json:"fee"`
	Alloc    float64 `json:"alloc"`
}

// RunBacktest handles POST /api/v1/backtest.
func (h *Handler) RunBacktest(c *gin.Context) {
	var req BacktestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errResp(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	// Defaults
	if req.Cash <= 0 {
		req.Cash = 10000
	}
	if req.Fee <= 0 {
		req.Fee = 0.001
	}
	if req.Alloc <= 0 {
		req.Alloc = 0.1
	}
	if req.Days <= 0 {
		req.Days = 30
	}

	// Parse time range
	var start, end time.Time
	var err error
	end = time.Now().UTC()

	if req.End != "" {
		end, err = time.Parse("2006-01-02", req.End)
		if err != nil {
			errResp(c, http.StatusBadRequest, "invalid end date: "+err.Error())
			return
		}
		// Set to end of day
		end = end.Add(24*time.Hour - time.Second)
	}

	if req.Start != "" {
		start, err = time.Parse("2006-01-02", req.Start)
		if err != nil {
			errResp(c, http.StatusBadRequest, "invalid start date: "+err.Error())
			return
		}
	} else {
		start = end.Add(-time.Duration(req.Days) * 24 * time.Hour)
	}

	// Create strategy
	strat, err := createBacktestStrategy(req.Strategy, h.deps.Config.Strategy.Config)
	if err != nil {
		errResp(c, http.StatusBadRequest, "unknown strategy: "+req.Strategy)
		return
	}

	// Load klines
	ctx := c.Request.Context()
	klines, err := backtest.LoadKlinesFromStore(ctx, h.deps.Store, req.Symbol, req.Interval, start, end)
	if err != nil {
		h.logger.Error("load klines for backtest", zap.Error(err))
		errResp(c, http.StatusInternalServerError, "failed to load klines: "+err.Error())
		return
	}

	if len(klines) == 0 {
		errResp(c, http.StatusBadRequest, "no kline data found for the specified range")
		return
	}

	// Run backtest
	engine := backtest.NewEngine(backtest.EngineConfig{
		Symbol:      req.Symbol,
		Interval:    req.Interval,
		InitialCash: req.Cash,
		FeeRate:     req.Fee,
		AllocPct:    req.Alloc,
	}, strat, h.logger.Named("backtest"))

	result, err := engine.Run(ctx, klines)
	if err != nil {
		h.logger.Error("backtest failed", zap.Error(err))
		errResp(c, http.StatusInternalServerError, "backtest failed: "+err.Error())
		return
	}

	// Downsample equity curve if too many points (for frontend performance)
	if len(result.EquityCurve) > 500 {
		result.EquityCurve = downsampleEquity(result.EquityCurve, 500)
	}

	ok(c, result)
}

// GetStrategies returns available strategies for the backtest UI.
func (h *Handler) GetStrategies(c *gin.Context) {
	strategies := []map[string]string{
		{"name": "ema_crossover", "label": "EMA Crossover"},
		{"name": "macd_rsi", "label": "MACD + RSI"},
		{"name": "bb_breakout", "label": "Bollinger Bands Breakout"},
	}
	ok(c, strategies)
}

func createBacktestStrategy(name string, cfg map[string]interface{}) (strategy.Strategy, error) {
	reg := strategy.NewRegistry()
	reg.Register("ema_crossover", func() strategy.Strategy { return trend.NewEMACrossStrategy() })
	reg.Register("macd_rsi", func() strategy.Strategy { return trend.NewMACDRSIStrategy() })
	reg.Register("bb_breakout", func() strategy.Strategy { return trend.NewBBBreakoutStrategy() })
	return reg.Create(name, cfg)
}

// downsampleEquity reduces equity curve to ~maxPoints by taking every nth point.
func downsampleEquity(curve []backtest.EquityPoint, maxPoints int) []backtest.EquityPoint {
	if len(curve) <= maxPoints {
		return curve
	}
	step := float64(len(curve)) / float64(maxPoints)
	result := make([]backtest.EquityPoint, 0, maxPoints)
	for i := 0.0; int(i) < len(curve); i += step {
		result = append(result, curve[int(i)])
	}
	// Always include the last point
	if len(result) > 0 && result[len(result)-1].Time != curve[len(curve)-1].Time {
		result = append(result, curve[len(curve)-1])
	}
	return result
}
