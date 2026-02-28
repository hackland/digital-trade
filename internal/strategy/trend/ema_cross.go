package trend

import (
	"context"
	"fmt"
	"time"

	"github.com/jayce/btc-trader/internal/exchange"
	"github.com/jayce/btc-trader/internal/strategy"
)

// EMACrossStrategy generates signals based on EMA crossovers.
// A golden cross (fast EMA crosses above slow EMA) triggers a BUY.
// A death cross (fast EMA crosses below slow EMA) triggers a SELL.
type EMACrossStrategy struct {
	fastPeriod  int
	slowPeriod  int
	rsiFilter   bool
	rsiPeriod   int
	rsiOverbought float64
	rsiOversold   float64

	prevFastEMA float64
	prevSlowEMA float64
	initialized bool
}

func NewEMACrossStrategy() *EMACrossStrategy {
	return &EMACrossStrategy{
		fastPeriod:    12,
		slowPeriod:    26,
		rsiFilter:     true,
		rsiPeriod:     14,
		rsiOverbought: 70,
		rsiOversold:   30,
	}
}

func (s *EMACrossStrategy) Name() string {
	return "ema_crossover"
}

func (s *EMACrossStrategy) Init(cfg map[string]interface{}) error {
	if v, ok := cfg["fast_period"]; ok {
		s.fastPeriod = toInt(v)
	}
	if v, ok := cfg["slow_period"]; ok {
		s.slowPeriod = toInt(v)
	}
	if v, ok := cfg["rsi_filter"]; ok {
		s.rsiFilter = toBool(v)
	}
	if v, ok := cfg["rsi_period"]; ok {
		s.rsiPeriod = toInt(v)
	}
	if v, ok := cfg["rsi_overbought"]; ok {
		s.rsiOverbought = toFloat(v)
	}
	if v, ok := cfg["rsi_oversold"]; ok {
		s.rsiOversold = toFloat(v)
	}
	return nil
}

func (s *EMACrossStrategy) RequiredIndicators() []strategy.IndicatorRequirement {
	reqs := []strategy.IndicatorRequirement{
		{Name: "EMA", Params: map[string]int{"period": s.fastPeriod}},
		{Name: "EMA", Params: map[string]int{"period": s.slowPeriod}},
	}
	if s.rsiFilter {
		reqs = append(reqs, strategy.IndicatorRequirement{
			Name: "RSI", Params: map[string]int{"period": s.rsiPeriod},
		})
	}
	return reqs
}

func (s *EMACrossStrategy) RequiredHistory() int {
	return s.slowPeriod + 10
}

func (s *EMACrossStrategy) Evaluate(ctx context.Context, snap *strategy.MarketSnapshot) (*strategy.Signal, error) {
	fastEMA := snap.Indicators.EMA[s.fastPeriod]
	slowEMA := snap.Indicators.EMA[s.slowPeriod]

	sig := &strategy.Signal{
		Action:    strategy.Hold,
		Symbol:    snap.Symbol,
		Strategy:  s.Name(),
		Timestamp: snap.Timestamp,
		Indicators: map[string]float64{
			"fast_ema": fastEMA,
			"slow_ema": slowEMA,
		},
	}

	if !s.initialized {
		s.prevFastEMA = fastEMA
		s.prevSlowEMA = slowEMA
		s.initialized = true
		return sig, nil
	}

	// RSI filter
	if s.rsiFilter {
		rsi := snap.Indicators.RSI[s.rsiPeriod]
		sig.Indicators["rsi"] = rsi

		// Golden cross: fast crosses above slow
		if s.prevFastEMA <= s.prevSlowEMA && fastEMA > slowEMA {
			if rsi < s.rsiOverbought { // Don't buy when overbought
				sig.Action = strategy.Buy
				sig.Strength = clamp((fastEMA-slowEMA)/slowEMA*100, 0, 1)
				sig.Reason = fmt.Sprintf(
					"EMA golden cross: EMA(%d)=%.2f > EMA(%d)=%.2f, RSI=%.1f",
					s.fastPeriod, fastEMA, s.slowPeriod, slowEMA, rsi,
				)
			}
		}

		// Death cross: fast crosses below slow
		if s.prevFastEMA >= s.prevSlowEMA && fastEMA < slowEMA {
			if rsi > s.rsiOversold { // Don't sell when oversold
				sig.Action = strategy.Sell
				sig.Strength = clamp((slowEMA-fastEMA)/slowEMA*100, 0, 1)
				sig.Reason = fmt.Sprintf(
					"EMA death cross: EMA(%d)=%.2f < EMA(%d)=%.2f, RSI=%.1f",
					s.fastPeriod, fastEMA, s.slowPeriod, slowEMA, rsi,
				)
			}
		}
	} else {
		// Without RSI filter
		if s.prevFastEMA <= s.prevSlowEMA && fastEMA > slowEMA {
			sig.Action = strategy.Buy
			sig.Strength = clamp((fastEMA-slowEMA)/slowEMA*100, 0, 1)
			sig.Reason = fmt.Sprintf(
				"EMA golden cross: EMA(%d)=%.2f > EMA(%d)=%.2f",
				s.fastPeriod, fastEMA, s.slowPeriod, slowEMA,
			)
		}
		if s.prevFastEMA >= s.prevSlowEMA && fastEMA < slowEMA {
			sig.Action = strategy.Sell
			sig.Strength = clamp((slowEMA-fastEMA)/slowEMA*100, 0, 1)
			sig.Reason = fmt.Sprintf(
				"EMA death cross: EMA(%d)=%.2f < EMA(%d)=%.2f",
				s.fastPeriod, fastEMA, s.slowPeriod, slowEMA,
			)
		}
	}

	s.prevFastEMA = fastEMA
	s.prevSlowEMA = slowEMA

	return sig, nil
}

func (s *EMACrossStrategy) OnTradeExecuted(trade *exchange.Trade) {
	// Can be extended for trailing stop tracking
}

// Helper functions for config parsing
func toInt(v interface{}) int {
	switch val := v.(type) {
	case int:
		return val
	case float64:
		return int(val)
	case int64:
		return int(val)
	default:
		return 0
	}
}

func toFloat(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case int64:
		return float64(val)
	default:
		return 0
	}
}

func toBool(v interface{}) bool {
	if val, ok := v.(bool); ok {
		return val
	}
	return false
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// Ensure compile-time interface compliance.
var _ strategy.Strategy = (*EMACrossStrategy)(nil)

// Suppress unused import
var _ = time.Now
