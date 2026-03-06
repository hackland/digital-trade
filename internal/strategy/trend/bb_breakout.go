package trend

import (
	"context"
	"fmt"

	"github.com/jayce/btc-trader/internal/exchange"
	"github.com/jayce/btc-trader/internal/strategy"
)

// BBBreakoutStrategy generates signals based on Bollinger Bands breakouts
// confirmed by volume.
//
// Buy:  Close price breaks above the upper band + volume > N-period average volume
// Sell: Close price drops below the lower band
type BBBreakoutStrategy struct {
	bbPeriod     int
	bbMult       float64 // standard deviation multiplier
	volumePeriod int     // period for average volume calculation
	rsiPeriod    int     // optional RSI confirmation

	prevClose   float64
	initialized bool
}

// NewBBBreakoutStrategy creates a strategy with default parameters.
func NewBBBreakoutStrategy() *BBBreakoutStrategy {
	return &BBBreakoutStrategy{
		bbPeriod:     20,
		bbMult:       2.0,
		volumePeriod: 20,
		rsiPeriod:    14,
	}
}

func (s *BBBreakoutStrategy) Name() string {
	return "bb_breakout"
}

func (s *BBBreakoutStrategy) Init(cfg map[string]interface{}) error {
	if v, ok := cfg["bb_period"]; ok {
		s.bbPeriod = toInt(v)
	}
	if v, ok := cfg["bb_mult"]; ok {
		s.bbMult = toFloat(v)
	}
	if v, ok := cfg["volume_period"]; ok {
		s.volumePeriod = toInt(v)
	}
	if v, ok := cfg["rsi_period"]; ok {
		s.rsiPeriod = toInt(v)
	}
	return nil
}

func (s *BBBreakoutStrategy) RequiredIndicators() []strategy.IndicatorRequirement {
	return []strategy.IndicatorRequirement{
		{
			Name: "BB",
			Params: map[string]int{
				"period": s.bbPeriod,
				"mult":   int(s.bbMult), // integer approximation, actual mult used via config
			},
		},
		{
			Name:   "RSI",
			Params: map[string]int{"period": s.rsiPeriod},
		},
	}
}

func (s *BBBreakoutStrategy) RequiredHistory() int {
	maxPeriod := s.bbPeriod
	if s.volumePeriod > maxPeriod {
		maxPeriod = s.volumePeriod
	}
	return maxPeriod + 10
}

func (s *BBBreakoutStrategy) Evaluate(ctx context.Context, snap *strategy.MarketSnapshot) (*strategy.Signal, error) {
	bb := snap.Indicators.BB
	rsi := snap.Indicators.RSI[s.rsiPeriod]

	// Current close price from the latest kline
	var closePrice, volume float64
	if len(snap.Klines) > 0 {
		last := snap.Klines[len(snap.Klines)-1]
		closePrice = last.Close
		volume = last.Volume
	}

	// Compute average volume over volumePeriod
	avgVolume := s.computeAvgVolume(snap.Klines)

	sig := &strategy.Signal{
		Action:    strategy.Hold,
		Symbol:    snap.Symbol,
		Strategy:  s.Name(),
		Timestamp: snap.Timestamp,
		Indicators: map[string]float64{
			"bb_upper":   bb.Upper,
			"bb_middle":  bb.Middle,
			"bb_lower":   bb.Lower,
			"bb_width":   bb.Width,
			"rsi":        rsi,
			"close":      closePrice,
			"volume":     volume,
			"avg_volume": avgVolume,
		},
	}

	if !s.initialized {
		s.prevClose = closePrice
		s.initialized = true
		return sig, nil
	}

	// Buy: price breaks above upper band with volume confirmation
	if closePrice > bb.Upper && s.prevClose <= bb.Upper {
		// Volume must exceed average (momentum confirmation)
		if avgVolume > 0 && volume > avgVolume {
			sig.Action = strategy.Buy
			sig.Strength = clamp((closePrice-bb.Upper)/bb.Upper*100, 0, 1)
			if sig.Strength == 0 {
				sig.Strength = 0.5
			}
			sig.Reason = fmt.Sprintf(
				"BB breakout above upper band: close=%.2f > upper=%.2f, vol=%.0f > avg=%.0f, RSI=%.1f",
				closePrice, bb.Upper, volume, avgVolume, rsi,
			)
		}
	}

	// Sell: price drops below lower band
	if closePrice < bb.Lower && s.prevClose >= bb.Lower {
		sig.Action = strategy.Sell
		sig.Strength = clamp((bb.Lower-closePrice)/bb.Lower*100, 0, 1)
		if sig.Strength == 0 {
			sig.Strength = 0.5
		}
		sig.Reason = fmt.Sprintf(
			"BB breakdown below lower band: close=%.2f < lower=%.2f, RSI=%.1f",
			closePrice, bb.Lower, rsi,
		)
	}

	s.prevClose = closePrice

	return sig, nil
}

func (s *BBBreakoutStrategy) OnTradeExecuted(trade *exchange.Trade) {
	// No special state tracking needed
}

// computeAvgVolume calculates average volume over the volumePeriod.
func (s *BBBreakoutStrategy) computeAvgVolume(klines []exchange.Kline) float64 {
	n := len(klines)
	if n < 2 {
		return 0
	}

	period := s.volumePeriod
	// Exclude the last candle from average to compare against it
	available := n - 1
	if period > available {
		period = available
	}

	sum := 0.0
	start := n - 1 - period
	for i := start; i < n-1; i++ {
		sum += klines[i].Volume
	}
	return sum / float64(period)
}

// Ensure compile-time interface compliance.
var _ strategy.Strategy = (*BBBreakoutStrategy)(nil)
