package trend

import (
	"context"
	"fmt"

	"github.com/jayce/btc-trader/internal/exchange"
	"github.com/jayce/btc-trader/internal/strategy"
)

// VolumeTrendStrategy generates signals using multi-indicator confluence:
// Trend (EMA) + Volume (放量) + Money Flow (MFI) + Momentum (RSI).
//
// The idea: only enter when ALL conditions confirm, reducing false signals
// compared to single-indicator strategies like EMA crossover.
//
// Buy conditions (ALL must be met):
//  1. EMA(fast) > EMA(slow)                     → trend is up
//  2. Volume > VolumeSMA * volMultiplier         → 放量 (real money flowing in)
//  3. MFI > mfiBuyMin                            → net money inflow
//  4. RSI in [rsiBuyMin, rsiBuyMax]              → mid-trend (not chasing highs)
//  5. CMF > 0                                    → buying pressure > selling pressure
//
// Sell conditions (ANY triggers):
//  - MFI < mfiSellThreshold                      → money flowing out
//  - EMA(fast) < EMA(slow)                       → trend reversal
//  - CMF < cmfSellThreshold for consecutive bars  → sustained selling pressure
type VolumeTrendStrategy struct {
	// EMA periods for trend
	fastPeriod int
	slowPeriod int
	// Volume confirmation
	volumePeriod  int     // period for volume SMA
	volMultiplier float64 // volume must exceed avgVol * this (default 1.5)
	// MFI thresholds
	mfiPeriod        int
	mfiBuyMin        float64 // min MFI to buy (default 50, net inflow)
	mfiSellThreshold float64 // sell when MFI drops below (default 40)
	// RSI range filter
	rsiPeriod int
	rsiBuyMin float64 // min RSI for buy (default 40)
	rsiBuyMax float64 // max RSI for buy (default 65)
	// CMF
	cmfPeriod        int
	cmfSellThreshold float64 // sell when CMF below this (default -0.1)
	cmfSellBars      int     // consecutive bars CMF below threshold to sell

	// State
	prevFastEMA   float64
	prevSlowEMA   float64
	cmfBelowCount int // consecutive bars CMF below threshold
	initialized   bool
}

// NewVolumeTrendStrategy creates a strategy with default parameters.
func NewVolumeTrendStrategy() *VolumeTrendStrategy {
	return &VolumeTrendStrategy{
		fastPeriod:       20,
		slowPeriod:       50,
		volumePeriod:     20,
		volMultiplier:    1.5,
		mfiPeriod:        14,
		mfiBuyMin:        50,
		mfiSellThreshold: 40,
		rsiPeriod:        14,
		rsiBuyMin:        40,
		rsiBuyMax:        65,
		cmfPeriod:        20,
		cmfSellThreshold: -0.1,
		cmfSellBars:      3,
	}
}

func (s *VolumeTrendStrategy) Name() string {
	return "volume_trend"
}

func (s *VolumeTrendStrategy) Init(cfg map[string]interface{}) error {
	if v, ok := cfg["fast_period"]; ok {
		s.fastPeriod = toInt(v)
	}
	if v, ok := cfg["slow_period"]; ok {
		s.slowPeriod = toInt(v)
	}
	if v, ok := cfg["volume_period"]; ok {
		s.volumePeriod = toInt(v)
	}
	if v, ok := cfg["vol_multiplier"]; ok {
		s.volMultiplier = toFloat(v)
	}
	if v, ok := cfg["mfi_period"]; ok {
		s.mfiPeriod = toInt(v)
	}
	if v, ok := cfg["mfi_buy_min"]; ok {
		s.mfiBuyMin = toFloat(v)
	}
	if v, ok := cfg["mfi_sell_threshold"]; ok {
		s.mfiSellThreshold = toFloat(v)
	}
	if v, ok := cfg["rsi_period"]; ok {
		s.rsiPeriod = toInt(v)
	}
	if v, ok := cfg["rsi_buy_min"]; ok {
		s.rsiBuyMin = toFloat(v)
	}
	if v, ok := cfg["rsi_buy_max"]; ok {
		s.rsiBuyMax = toFloat(v)
	}
	if v, ok := cfg["cmf_period"]; ok {
		s.cmfPeriod = toInt(v)
	}
	if v, ok := cfg["cmf_sell_threshold"]; ok {
		s.cmfSellThreshold = toFloat(v)
	}
	if v, ok := cfg["cmf_sell_bars"]; ok {
		s.cmfSellBars = toInt(v)
	}
	return nil
}

func (s *VolumeTrendStrategy) RequiredIndicators() []strategy.IndicatorRequirement {
	return []strategy.IndicatorRequirement{
		{Name: "EMA", Params: map[string]int{"period": s.fastPeriod}},
		{Name: "EMA", Params: map[string]int{"period": s.slowPeriod}},
		{Name: "VolumeSMA", Params: map[string]int{"period": s.volumePeriod}},
		{Name: "MFI", Params: map[string]int{"period": s.mfiPeriod}},
		{Name: "RSI", Params: map[string]int{"period": s.rsiPeriod}},
		{Name: "CMF", Params: map[string]int{"period": s.cmfPeriod}},
	}
}

func (s *VolumeTrendStrategy) RequiredHistory() int {
	maxPeriod := s.slowPeriod
	if s.volumePeriod > maxPeriod {
		maxPeriod = s.volumePeriod
	}
	if s.cmfPeriod > maxPeriod {
		maxPeriod = s.cmfPeriod
	}
	return maxPeriod + 10
}

func (s *VolumeTrendStrategy) Evaluate(ctx context.Context, snap *strategy.MarketSnapshot) (*strategy.Signal, error) {
	fastEMA := snap.Indicators.EMA[s.fastPeriod]
	slowEMA := snap.Indicators.EMA[s.slowPeriod]
	mfi := snap.Indicators.MFI[s.mfiPeriod]
	rsi := snap.Indicators.RSI[s.rsiPeriod]
	cmf := snap.Indicators.CMF[s.cmfPeriod]
	volumeAvg := snap.Indicators.VolumeSMA[s.volumePeriod]

	var closePrice, volume float64
	if len(snap.Klines) > 0 {
		last := snap.Klines[len(snap.Klines)-1]
		closePrice = last.Close
		volume = last.Volume
	}

	sig := &strategy.Signal{
		Action:    strategy.Hold,
		Symbol:    snap.Symbol,
		Strategy:  s.Name(),
		Timestamp: snap.Timestamp,
		Indicators: map[string]float64{
			"fast_ema":   fastEMA,
			"slow_ema":   slowEMA,
			"mfi":        mfi,
			"rsi":        rsi,
			"cmf":        cmf,
			"close":      closePrice,
			"volume":     volume,
			"volume_avg": volumeAvg,
		},
	}

	if !s.initialized {
		s.prevFastEMA = fastEMA
		s.prevSlowEMA = slowEMA
		s.initialized = true
		return sig, nil
	}

	// Track CMF below threshold
	if cmf < s.cmfSellThreshold {
		s.cmfBelowCount++
	} else {
		s.cmfBelowCount = 0
	}

	hasPosition := snap.Position != nil && snap.Position.Quantity > 0

	// --- BUY: all conditions must be met ---
	if !hasPosition {
		trendUp := fastEMA > slowEMA
		volumeConfirm := volumeAvg > 0 && volume > volumeAvg*s.volMultiplier
		mfiConfirm := mfi > s.mfiBuyMin
		rsiInRange := rsi >= s.rsiBuyMin && rsi <= s.rsiBuyMax
		cmfPositive := cmf > 0

		if trendUp && volumeConfirm && mfiConfirm && rsiInRange && cmfPositive {
			sig.Action = strategy.Buy
			// Strength based on volume ratio and CMF
			volRatio := 0.5
			if volumeAvg > 0 {
				volRatio = clamp(volume/volumeAvg/s.volMultiplier*0.5, 0.1, 1.0)
			}
			sig.Strength = clamp(volRatio+cmf, 0.1, 1.0)
			sig.Reason = fmt.Sprintf(
				"Volume trend buy: EMA(%d)=%.2f > EMA(%d)=%.2f, vol=%.0f > avg*%.1f=%.0f, MFI=%.1f, RSI=%.1f, CMF=%.3f",
				s.fastPeriod, fastEMA, s.slowPeriod, slowEMA,
				volume, s.volMultiplier, volumeAvg*s.volMultiplier,
				mfi, rsi, cmf,
			)
		}
	}

	// --- SELL: any condition triggers ---
	if hasPosition {
		// Condition 1: MFI drops below threshold (money flowing out)
		if mfi < s.mfiSellThreshold {
			sig.Action = strategy.Sell
			sig.Strength = clamp((s.mfiSellThreshold-mfi)/s.mfiSellThreshold, 0.3, 1.0)
			sig.Reason = fmt.Sprintf(
				"Volume trend sell: MFI=%.1f < %.0f (money outflow), RSI=%.1f, CMF=%.3f",
				mfi, s.mfiSellThreshold, rsi, cmf,
			)
		}

		// Condition 2: Trend reversal (death cross)
		if s.prevFastEMA >= s.prevSlowEMA && fastEMA < slowEMA {
			sig.Action = strategy.Sell
			sig.Strength = 0.7
			sig.Reason = fmt.Sprintf(
				"Volume trend sell: EMA death cross EMA(%d)=%.2f < EMA(%d)=%.2f, MFI=%.1f, CMF=%.3f",
				s.fastPeriod, fastEMA, s.slowPeriod, slowEMA, mfi, cmf,
			)
		}

		// Condition 3: Sustained selling pressure (CMF below threshold for N bars)
		if s.cmfBelowCount >= s.cmfSellBars {
			sig.Action = strategy.Sell
			sig.Strength = 0.6
			sig.Reason = fmt.Sprintf(
				"Volume trend sell: CMF=%.3f < %.2f for %d bars (sustained selling pressure), MFI=%.1f",
				cmf, s.cmfSellThreshold, s.cmfBelowCount, mfi,
			)
		}
	}

	s.prevFastEMA = fastEMA
	s.prevSlowEMA = slowEMA

	return sig, nil
}

func (s *VolumeTrendStrategy) OnTradeExecuted(trade *exchange.Trade) {
	// Reset CMF counter on trade to avoid stale state
	s.cmfBelowCount = 0
}

// Ensure compile-time interface compliance.
var _ strategy.Strategy = (*VolumeTrendStrategy)(nil)
