package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jayce/btc-trader/internal/backtest"
	"github.com/jayce/btc-trader/internal/exchange"
	"github.com/jayce/btc-trader/internal/grid"
	"go.uber.org/zap"
)

// GridBacktestRequest is the JSON body for POST /api/v1/grid-backtest.
type GridBacktestRequest struct {
	Symbol   string `json:"symbol" binding:"required"`
	Interval string `json:"interval"` // kline interval used for simulation; defaults to "1m"

	Days  int    `json:"days"`
	Start string `json:"start"` // YYYY-MM-DD
	End   string `json:"end"`   // YYYY-MM-DD

	// Range: either supply UpperPrice/LowerPrice explicitly, or leave both
	// zero/omitted and set AutoRangeDays to derive them from the historical
	// high/low over that many trailing days.
	UpperPrice    float64 `json:"upper_price"`
	LowerPrice    float64 `json:"lower_price"`
	AutoRangeDays int     `json:"auto_range_days"`

	GridSpacingPct     float64  `json:"grid_spacing_pct"`
	USDTPerGrid        float64  `json:"usdt_per_grid"`
	Cash               float64  `json:"cash"`
	Fee                *float64 `json:"fee"`
	StopLossBelowPct   float64  `json:"stop_loss_below_pct"`
	PauseBuyAboveUpper *bool    `json:"pause_buy_above_upper"`
}

// RunGridBacktest handles POST /api/v1/grid-backtest.
func (h *Handler) RunGridBacktest(c *gin.Context) {
	var req GridBacktestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errResp(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	if req.Interval == "" {
		req.Interval = "1m"
	}
	if req.Cash <= 0 {
		req.Cash = 10000
	}
	feeRate := 0.001
	if req.Fee != nil {
		feeRate = *req.Fee
	}
	if req.Days <= 0 && req.Start == "" {
		req.Days = 14
	}
	if req.GridSpacingPct <= 0 {
		errResp(c, http.StatusBadRequest, "grid_spacing_pct must be > 0")
		return
	}
	if req.USDTPerGrid <= 0 {
		errResp(c, http.StatusBadRequest, "usdt_per_grid must be > 0")
		return
	}
	pauseAboveUpper := true
	if req.PauseBuyAboveUpper != nil {
		pauseAboveUpper = *req.PauseBuyAboveUpper
	}

	// Parse time range (same convention as RunBacktest: whole days, UTC).
	var start, end time.Time
	var err error
	nowUTC := time.Now().UTC()
	endDay := time.Date(nowUTC.Year(), nowUTC.Month(), nowUTC.Day(), 0, 0, 0, 0, time.UTC)
	end = endDay.Add(24*time.Hour - time.Second)

	if req.End != "" {
		var parsedEnd time.Time
		parsedEnd, err = time.Parse("2006-01-02", req.End)
		if err != nil {
			errResp(c, http.StatusBadRequest, "invalid end date: "+err.Error())
			return
		}
		endDay = parsedEnd
		end = parsedEnd.Add(24*time.Hour - time.Second)
	}
	if req.Start != "" {
		start, err = time.Parse("2006-01-02", req.Start)
		if err != nil {
			errResp(c, http.StatusBadRequest, "invalid start date: "+err.Error())
			return
		}
	} else {
		start = endDay.Add(-time.Duration(req.Days-1) * 24 * time.Hour)
	}

	ctx := c.Request.Context()
	klines, err := backtest.LoadKlinesFromStore(ctx, h.deps.Store, req.Symbol, req.Interval, start, end)
	if err != nil {
		h.logger.Warn("load klines from store failed, will fetch from exchange", zap.Error(err))
	}
	expectedCount := backtest.ExpectedKlineCount(req.Interval, start, end)
	if len(klines) < expectedCount*8/10 {
		fetched, fetchErr := backtest.FetchKlinesFromExchange(ctx, h.deps.Exchange, req.Symbol, req.Interval, start, end)
		if fetchErr != nil {
			h.logger.Error("fetch klines from exchange", zap.Error(fetchErr))
			if len(klines) == 0 {
				errResp(c, http.StatusInternalServerError, "failed to load klines: "+fetchErr.Error())
				return
			}
		} else {
			klines = fetched
		}
	}
	if len(klines) == 0 {
		errResp(c, http.StatusBadRequest, "no kline data found for the specified range")
		return
	}

	upper, lower := req.UpperPrice, req.LowerPrice
	if upper <= 0 || lower <= 0 {
		if req.AutoRangeDays <= 0 {
			errResp(c, http.StatusBadRequest, "upper_price/lower_price required, or set auto_range_days to derive them from history")
			return
		}
		rangeKlines := klines
		if req.AutoRangeDays*24*60 < len(klines) {
			// AutoRangeDays is measured in calendar days of the requested
			// interval; approximate the trailing window by bar count.
			rangeKlines = klines[len(klines)-minInt(len(klines), barsForDays(req.Interval, req.AutoRangeDays)):]
		}
		lower, upper = highLow(rangeKlines)
		if lower <= 0 || upper <= lower {
			errResp(c, http.StatusInternalServerError, "failed to derive a valid range from history")
			return
		}
	}

	cfg := grid.NewConfig()
	cfg.Symbol = req.Symbol
	cfg.Interval = req.Interval
	cfg.UpperPrice = upper
	cfg.LowerPrice = lower
	cfg.GridSpacingPct = req.GridSpacingPct
	cfg.USDTPerGrid = req.USDTPerGrid
	cfg.FeeRate = feeRate
	cfg.InitialCash = req.Cash
	cfg.StopLossBelowPct = req.StopLossBelowPct
	cfg.PauseBuyAboveUpper = pauseAboveUpper

	engine := grid.NewEngine(cfg)
	result, err := engine.Run(klines)
	if err != nil {
		errResp(c, http.StatusBadRequest, "grid backtest failed: "+err.Error())
		return
	}

	if len(result.EquityCurve) > 1000 {
		result.EquityCurve = downsampleGridEquity(result.EquityCurve, 1000)
	}

	ok(c, result)
}

// SuggestGridRange handles GET /api/v1/grid-backtest/suggest-range — returns
// a candidate [lower, upper] range derived from recent price history, for
// the UI to pre-fill before the operator adjusts and runs a full backtest.
func (h *Handler) SuggestGridRange(c *gin.Context) {
	symbol := c.Query("symbol")
	if symbol == "" {
		errResp(c, http.StatusBadRequest, "symbol is required")
		return
	}
	days := 30
	if v := c.Query("days"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			days = parsed
		}
	}

	ctx := c.Request.Context()
	end := time.Now().UTC()
	start := end.Add(-time.Duration(days) * 24 * time.Hour)

	klines, err := backtest.LoadKlinesFromStore(ctx, h.deps.Store, symbol, "1h", start, end)
	if err != nil || len(klines) == 0 {
		errResp(c, http.StatusInternalServerError, "failed to load history for range suggestion")
		return
	}

	lower, upper := highLow(klines)
	ok(c, gin.H{
		"symbol":        symbol,
		"days":          days,
		"lower_price":   lower,
		"upper_price":   upper,
		"current_price": klines[len(klines)-1].Close,
	})
}

// highLow returns the lowest Low and highest High across the given klines.
func highLow(klines []exchange.Kline) (lower, upper float64) {
	if len(klines) == 0 {
		return 0, 0
	}
	lower, upper = klines[0].Low, klines[0].High
	for _, k := range klines {
		if k.Low < lower {
			lower = k.Low
		}
		if k.High > upper {
			upper = k.High
		}
	}
	return lower, upper
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// barsForDays approximates how many bars of the given interval span N days.
func barsForDays(interval string, days int) int {
	perDay := 24 * 60 / intervalMinutes(interval)
	return perDay * days
}

func intervalMinutes(interval string) int {
	switch interval {
	case "1m":
		return 1
	case "5m":
		return 5
	case "15m":
		return 15
	case "1h":
		return 60
	case "4h":
		return 240
	case "1d":
		return 1440
	default:
		return 60
	}
}

func downsampleGridEquity(curve []grid.EquityPoint, maxPoints int) []grid.EquityPoint {
	if len(curve) <= maxPoints {
		return curve
	}
	step := float64(len(curve)) / float64(maxPoints)
	result := make([]grid.EquityPoint, 0, maxPoints)
	for i := 0.0; int(i) < len(curve); i += step {
		result = append(result, curve[int(i)])
	}
	if len(result) > 0 && result[len(result)-1].Time != curve[len(curve)-1].Time {
		result = append(result, curve[len(curve)-1])
	}
	return result
}
