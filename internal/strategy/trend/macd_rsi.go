package trend

import (
	"context"
	"fmt"

	"github.com/jayce/btc-trader/internal/exchange"
	"github.com/jayce/btc-trader/internal/strategy"
)

// MACDRSIStrategy generates signals using MACD histogram zero-line crossovers
// filtered by RSI to avoid extreme overbought/oversold entries.
//
// Buy:  MACD histogram crosses from negative to positive (bullish momentum)
//   - RSI between oversold and overbought thresholds
//
// Sell: MACD histogram crosses from positive to negative (bearish momentum)
//   - RSI between oversold and overbought thresholds
type MACDRSIStrategy struct {
	fastPeriod    int
	slowPeriod    int
	signalPeriod  int
	rsiPeriod     int
	rsiOverbought float64
	rsiOversold   float64

	prevHistogram float64
	initialized   bool
}

// NewMACDRSIStrategy creates a strategy with default parameters.
func NewMACDRSIStrategy() *MACDRSIStrategy {
	return &MACDRSIStrategy{
		fastPeriod:    12,
		slowPeriod:    26,
		signalPeriod:  9,
		rsiPeriod:     14,
		rsiOverbought: 70,
		rsiOversold:   30,
	}
}

func (s *MACDRSIStrategy) Name() string {
	return "macd_rsi"
}

func (s *MACDRSIStrategy) Init(cfg map[string]interface{}) error {
	if v, ok := cfg["fast_period"]; ok {
		s.fastPeriod = toInt(v)
	}
	if v, ok := cfg["slow_period"]; ok {
		s.slowPeriod = toInt(v)
	}
	if v, ok := cfg["signal_period"]; ok {
		s.signalPeriod = toInt(v)
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

func (s *MACDRSIStrategy) RequiredIndicators() []strategy.IndicatorRequirement {
	return []strategy.IndicatorRequirement{
		{
			Name: "MACD",
			Params: map[string]int{
				"fast":   s.fastPeriod,
				"slow":   s.slowPeriod,
				"signal": s.signalPeriod,
			},
		},
		{
			Name:   "RSI",
			Params: map[string]int{"period": s.rsiPeriod},
		},
	}
}

func (s *MACDRSIStrategy) RequiredHistory() int {
	// slow EMA needs slowPeriod, then MACD signal needs signalPeriod more
	return s.slowPeriod + s.signalPeriod + 10
}

func (s *MACDRSIStrategy) Evaluate(ctx context.Context, snap *strategy.MarketSnapshot) (*strategy.Signal, error) {
	macd := snap.Indicators.MACD
	rsi := snap.Indicators.RSI[s.rsiPeriod]
	histogram := macd.Histogram

	sig := &strategy.Signal{
		Action:    strategy.Hold,
		Symbol:    snap.Symbol,
		Strategy:  s.Name(),
		Timestamp: snap.Timestamp,
		Indicators: map[string]float64{
			"macd":      macd.MACD,
			"signal":    macd.Signal,
			"histogram": histogram,
			"rsi":       rsi,
		},
	}

	if !s.initialized {
		s.prevHistogram = histogram
		s.initialized = true
		return sig, nil
	}

	// Bullish: histogram crosses from negative to positive
	if s.prevHistogram <= 0 && histogram > 0 {
		// RSI filter: don't buy when overbought
		if rsi < s.rsiOverbought && rsi > s.rsiOversold {
			sig.Action = strategy.Buy
			sig.Strength = clamp(histogram/macd.Signal*10, 0, 1)
			if sig.Strength == 0 {
				sig.Strength = 0.5
			}
			sig.Reason = fmt.Sprintf(
				"MACD bullish crossover: histogram %.4f (prev %.4f), RSI=%.1f",
				histogram, s.prevHistogram, rsi,
			)
		}
	}

	// Bearish: histogram crosses from positive to negative
	if s.prevHistogram >= 0 && histogram < 0 {
		// RSI filter: don't sell when oversold
		if rsi > s.rsiOversold && rsi < s.rsiOverbought {
			sig.Action = strategy.Sell
			sig.Strength = clamp(-histogram/macd.Signal*10, 0, 1)
			if sig.Strength == 0 {
				sig.Strength = 0.5
			}
			sig.Reason = fmt.Sprintf(
				"MACD bearish crossover: histogram %.4f (prev %.4f), RSI=%.1f",
				histogram, s.prevHistogram, rsi,
			)
		}
	}

	s.prevHistogram = histogram

	return sig, nil
}

func (s *MACDRSIStrategy) OnTradeExecuted(trade *exchange.Trade) {
	// No special state tracking needed
}

// Ensure compile-time interface compliance.
var _ strategy.Strategy = (*MACDRSIStrategy)(nil)
