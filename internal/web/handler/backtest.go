package handler

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jayce/btc-trader/internal/backtest"
	"github.com/jayce/btc-trader/internal/exchange"
	"github.com/jayce/btc-trader/internal/strategy"
	"github.com/jayce/btc-trader/internal/strategy/trend"
	"go.uber.org/zap"
)

// BacktestRequest is the JSON body for POST /api/v1/backtest.
type BacktestRequest struct {
	Symbol         string   `json:"symbol" binding:"required"`
	Interval       string   `json:"interval" binding:"required"`
	Strategy       string   `json:"strategy" binding:"required"`
	PriceStrategy  string   `json:"price_strategy"`
	VolumeStrategy string   `json:"volume_strategy"`
	Days           int      `json:"days"`
	Start          string   `json:"start"` // YYYY-MM-DD
	End            string   `json:"end"`   // YYYY-MM-DD
	Cash           float64  `json:"cash"`
	Fee            *float64 `json:"fee"`   // pointer to distinguish 0 from missing
	Alloc          *float64 `json:"alloc"` // pointer to distinguish 0 from missing
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

	// Create strategy
	strat, err := createBacktestStrategy(req.Strategy, req.PriceStrategy, req.VolumeStrategy, h.deps.Config.Strategy.Config)
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

	// Run backtest
	engine := backtest.NewEngine(backtest.EngineConfig{
		Symbol:      req.Symbol,
		Interval:    req.Interval,
		InitialCash: req.Cash,
		FeeRate:     feeRate,
		AllocPct:    allocPct,
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
		{"name": "vwap_reversion", "label": "VWAP Mean Reversion"},
		{"name": "volume_trend", "label": "Volume Trend"},
		{"name": "composite_score", "label": "Composite Score (Multi-Indicator)"},
		{"name": "manual_composite", "label": "Manual Composite (Price + Volume)"},
	}
	ok(c, strategies)
}

func createBacktestStrategy(name, priceStrategy, volumeStrategy string, cfg map[string]interface{}) (strategy.Strategy, error) {
	if name == "manual_composite" {
		if priceStrategy == "" {
			priceStrategy = "ema_crossover"
		}
		if volumeStrategy == "" {
			volumeStrategy = "volume_trend"
		}
		if priceStrategy == "manual_composite" || volumeStrategy == "manual_composite" {
			return nil, fmt.Errorf("manual_composite cannot recursively include manual_composite")
		}

		price, err := createSingleBacktestStrategy(priceStrategy, cfg)
		if err != nil {
			return nil, fmt.Errorf("invalid price_strategy %q", priceStrategy)
		}
		volume, err := createSingleBacktestStrategy(volumeStrategy, cfg)
		if err != nil {
			return nil, fmt.Errorf("invalid volume_strategy %q", volumeStrategy)
		}

		composite := &manualCompositeStrategy{
			price:      price,
			volume:     volume,
			priceName:  priceStrategy,
			volumeName: volumeStrategy,
		}
		return composite, nil
	}

	return createSingleBacktestStrategy(name, cfg)
}

func createSingleBacktestStrategy(name string, cfg map[string]interface{}) (strategy.Strategy, error) {
	reg := strategy.NewRegistry()
	reg.Register("ema_crossover", func() strategy.Strategy { return trend.NewEMACrossStrategy() })
	reg.Register("macd_rsi", func() strategy.Strategy { return trend.NewMACDRSIStrategy() })
	reg.Register("bb_breakout", func() strategy.Strategy { return trend.NewBBBreakoutStrategy() })
	reg.Register("vwap_reversion", func() strategy.Strategy { return trend.NewVWAPReversionStrategy() })
	reg.Register("volume_trend", func() strategy.Strategy { return trend.NewVolumeTrendStrategy() })
	reg.Register("composite_score", func() strategy.Strategy { return trend.NewCompositeScoreStrategy() })
	return reg.Create(name, cfg)
}

// manualCompositeStrategy combines one price strategy and one volume strategy.
// Trading action is produced only when both sub-strategies agree on Buy or Sell.
type manualCompositeStrategy struct {
	price      strategy.Strategy
	volume     strategy.Strategy
	priceName  string
	volumeName string
}

func (s *manualCompositeStrategy) Name() string {
	return "manual_composite"
}

func (s *manualCompositeStrategy) Init(_ map[string]interface{}) error {
	// Sub-strategies are already initialized by factory.
	return nil
}

func (s *manualCompositeStrategy) RequiredIndicators() []strategy.IndicatorRequirement {
	merged := make([]strategy.IndicatorRequirement, 0)
	seen := make(map[string]struct{})

	for _, req := range append(s.price.RequiredIndicators(), s.volume.RequiredIndicators()...) {
		key := req.Name + fmt.Sprintf("%v", req.Params)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, req)
	}
	return merged
}

func (s *manualCompositeStrategy) RequiredHistory() int {
	p := s.price.RequiredHistory()
	v := s.volume.RequiredHistory()
	if v > p {
		return v
	}
	return p
}

func (s *manualCompositeStrategy) Evaluate(ctx context.Context, snap *strategy.MarketSnapshot) (*strategy.Signal, error) {
	priceSig, err := s.price.Evaluate(ctx, snap)
	if err != nil {
		return nil, err
	}
	volumeSig, err := s.volume.Evaluate(ctx, snap)
	if err != nil {
		return nil, err
	}

	action := strategy.Hold
	if priceSig.Action == volumeSig.Action && (priceSig.Action == strategy.Buy || priceSig.Action == strategy.Sell) {
		action = priceSig.Action
	}

	reason := fmt.Sprintf(
		"manual composite [%s:%s | %s:%s]",
		s.priceName, priceSig.Action.String(),
		s.volumeName, volumeSig.Action.String(),
	)

	strength := (priceSig.Strength + volumeSig.Strength) / 2
	if action == strategy.Hold {
		strength = 0
	}

	indicators := make(map[string]float64, len(priceSig.Indicators)+len(volumeSig.Indicators))
	for k, v := range priceSig.Indicators {
		indicators["price."+k] = v
	}
	for k, v := range volumeSig.Indicators {
		indicators["volume."+k] = v
	}

	return &strategy.Signal{
		Action:     action,
		Strength:   strength,
		Symbol:     snap.Symbol,
		Strategy:   s.Name(),
		Reason:     reason,
		Indicators: indicators,
		Timestamp:  snap.Timestamp,
	}, nil
}

func (s *manualCompositeStrategy) OnTradeExecuted(trade *exchange.Trade) {
	s.price.OnTradeExecuted(trade)
	s.volume.OnTradeExecuted(trade)
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
