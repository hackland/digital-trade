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
	Symbol         string                 `json:"symbol" binding:"required"`
	Interval       string                 `json:"interval" binding:"required"`
	Strategy       string                 `json:"strategy" binding:"required"`
	PriceStrategy  string                 `json:"price_strategy"`
	VolumeStrategy string                 `json:"volume_strategy"`
	StrategyConfig map[string]interface{} `json:"strategy_config"` // frontend-provided strategy config
	Days           int                    `json:"days"`
	Start          string                 `json:"start"` // YYYY-MM-DD
	End            string                 `json:"end"`   // YYYY-MM-DD
	Cash           float64                `json:"cash"`
	Fee            *float64               `json:"fee"`   // pointer to distinguish 0 from missing
	Alloc          *float64               `json:"alloc"` // pointer to distinguish 0 from missing
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
	feeRate := 0.001
	if req.Fee != nil {
		feeRate = *req.Fee
	}
	allocPct := 0.1
	if req.Alloc != nil {
		allocPct = *req.Alloc
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

	// Create strategy: use frontend config if provided, else global config
	stratCfg := h.deps.Config.Strategy.Config
	if len(req.StrategyConfig) > 0 {
		stratCfg = req.StrategyConfig
	}
	strat, err := createBacktestStrategy(req.Strategy, stratCfg)
	if err != nil {
		errResp(c, http.StatusBadRequest, err.Error())
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

	// Build engine config
	engineCfg := backtest.EngineConfig{
		Symbol:      req.Symbol,
		Interval:    req.Interval,
		InitialCash: req.Cash,
		FeeRate:     feeRate,
		AllocPct:    allocPct,
	}

	// Load HTF klines if strategy requires multi-TF
	if cwStrat, ok := strat.(*trend.CustomWeightedStrategy); ok {
		htfInterval := cwStrat.HTFInterval()
		if htfInterval != "" {
			htfKlines, htfErr := backtest.LoadKlinesFromStore(ctx, h.deps.Store, req.Symbol, htfInterval, start, end)
			if htfErr != nil {
				h.logger.Warn("load HTF klines", zap.String("interval", htfInterval), zap.Error(htfErr))
			} else if len(htfKlines) > 0 {
				engineCfg.HTFKlines = htfKlines
				engineCfg.HTFInterval = htfInterval
				engineCfg.HTFIndReqs = cwStrat.HTFIndicatorRequirements()
				engineCfg.HTFHistSize = cwStrat.HTFHistoryRequired()
			}
		}
	}

	// Run backtest
	engine := backtest.NewEngine(engineCfg, strat, h.logger.Named("backtest"))

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
		{"name": "custom_weighted", "label": "自定义加权 (Custom Weighted)"},
	}
	ok(c, strategies)
}

func createBacktestStrategy(name string, cfg map[string]interface{}) (strategy.Strategy, error) {
	reg := strategy.NewRegistry()
	reg.Register("custom_weighted", func() strategy.Strategy { return trend.NewCustomWeightedStrategy() })
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
