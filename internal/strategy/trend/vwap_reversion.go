package trend

import (
	"context"
	"fmt"

	"github.com/jayce/btc-trader/internal/exchange"
	"github.com/jayce/btc-trader/internal/strategy"
)

// VWAPReversionStrategy generates signals based on price deviation from VWAP,
// confirmed by MFI (money flow) and volume contraction/expansion.
//
// Core idea: price tends to revert to VWAP (the "fair" price).
// When price is far below VWAP and selling pressure exhausts (MFI oversold +
// volume shrinking), it's a buy opportunity. Vice versa for sells.
//
// Buy conditions (ALL must be met):
//   - Close < VWAP * (1 - devThreshold)   (price significantly below fair value)
//   - MFI < mfiBuyThreshold               (money flow exhausted / oversold)
//   - Current volume < VolumeSMA * volShrinkRatio (selling volume drying up)
//
// Sell conditions (ANY triggers):
//   - Close > VWAP * (1 + devThreshold) AND MFI > mfiSellThreshold (overbought)
//   - OR MFI > mfiExitThreshold (extreme overbought, exit regardless)
type VWAPReversionStrategy struct {
	// VWAP deviation threshold (e.g., 0.01 = 1%)
	devThreshold float64
	// MFI thresholds
	mfiPeriod        int
	mfiBuyThreshold  float64 // buy when MFI below this (default 30)
	mfiSellThreshold float64 // sell when MFI above this (default 70)
	mfiExitThreshold float64 // force exit when MFI above this (default 80)
	// Volume filter
	volumePeriod   int     // period for volume SMA
	volShrinkRatio float64 // buy when volume < avgVol * ratio (default 0.8)
	// RSI confirmation
	rsiPeriod int
	rsiMax    float64 // don't buy when RSI above this

	prevClose float64
	prevVWAP  float64
	prevMFI   float64
	callCount int
}

// NewVWAPReversionStrategy creates a strategy with default parameters.
func NewVWAPReversionStrategy() *VWAPReversionStrategy {
	return &VWAPReversionStrategy{
		devThreshold:     0.005, // 0.5% deviation from VWAP
		mfiPeriod:        14,
		mfiBuyThreshold:  30,
		mfiSellThreshold: 70,
		mfiExitThreshold: 80,
		volumePeriod:     20,
		volShrinkRatio:   0.8,
		rsiPeriod:        14,
		rsiMax:           65,
	}
}

func (s *VWAPReversionStrategy) Name() string {
	return "vwap_reversion"
}

func (s *VWAPReversionStrategy) Init(cfg map[string]interface{}) error {
	if v, ok := cfg["dev_threshold"]; ok {
		s.devThreshold = toFloat(v)
	}
	if v, ok := cfg["mfi_period"]; ok {
		s.mfiPeriod = toInt(v)
	}
	if v, ok := cfg["mfi_buy_threshold"]; ok {
		s.mfiBuyThreshold = toFloat(v)
	}
	if v, ok := cfg["mfi_sell_threshold"]; ok {
		s.mfiSellThreshold = toFloat(v)
	}
	if v, ok := cfg["mfi_exit_threshold"]; ok {
		s.mfiExitThreshold = toFloat(v)
	}
	if v, ok := cfg["volume_period"]; ok {
		s.volumePeriod = toInt(v)
	}
	if v, ok := cfg["vol_shrink_ratio"]; ok {
		s.volShrinkRatio = toFloat(v)
	}
	if v, ok := cfg["rsi_period"]; ok {
		s.rsiPeriod = toInt(v)
	}
	if v, ok := cfg["rsi_max"]; ok {
		s.rsiMax = toFloat(v)
	}
	return nil
}

func (s *VWAPReversionStrategy) RequiredIndicators() []strategy.IndicatorRequirement {
	return []strategy.IndicatorRequirement{
		{Name: "VWAP", Params: map[string]int{}},
		{Name: "MFI", Params: map[string]int{"period": s.mfiPeriod}},
		{Name: "RSI", Params: map[string]int{"period": s.rsiPeriod}},
		{Name: "VolumeSMA", Params: map[string]int{"period": s.volumePeriod}},
	}
}

func (s *VWAPReversionStrategy) RequiredHistory() int {
	maxPeriod := s.volumePeriod
	if s.mfiPeriod > maxPeriod {
		maxPeriod = s.mfiPeriod
	}
	if s.rsiPeriod > maxPeriod {
		maxPeriod = s.rsiPeriod
	}
	return maxPeriod + 10
}

func (s *VWAPReversionStrategy) Evaluate(ctx context.Context, snap *strategy.MarketSnapshot) (*strategy.Signal, error) {
	vwap := snap.Indicators.VWAP
	mfi := snap.Indicators.MFI[s.mfiPeriod]
	rsi := snap.Indicators.RSI[s.rsiPeriod]
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
			"vwap":       vwap,
			"mfi":        mfi,
			"rsi":        rsi,
			"close":      closePrice,
			"volume":     volume,
			"volume_avg": volumeAvg,
		},
	}

	s.callCount++
	if s.callCount <= 2 {
		// Need at least 2 bars to compare
		s.prevClose = closePrice
		s.prevVWAP = vwap
		s.prevMFI = mfi
		return sig, nil
	}

	if vwap <= 0 {
		return sig, nil
	}

	// Calculate price deviation from VWAP
	deviation := (closePrice - vwap) / vwap

	hasPosition := snap.Position != nil && snap.Position.Quantity > 0

	// --- BUY logic ---
	if !hasPosition {
		// Price significantly below VWAP
		if deviation < -s.devThreshold {
			// MFI indicates selling exhaustion
			if mfi < s.mfiBuyThreshold {
				// Volume shrinking (selling pressure drying up)
				volumeOk := volumeAvg <= 0 || volume < volumeAvg*s.volShrinkRatio
				// RSI not too high
				rsiOk := rsi < s.rsiMax

				if volumeOk && rsiOk {
					sig.Action = strategy.Buy
					sig.Strength = clamp(-deviation/s.devThreshold*0.3, 0.1, 1.0)
					sig.Reason = fmt.Sprintf(
						"VWAP reversion buy: close=%.2f < VWAP=%.2f (dev=%.2f%%), MFI=%.1f, RSI=%.1f, vol=%.0f < avg=%.0f",
						closePrice, vwap, deviation*100, mfi, rsi, volume, volumeAvg,
					)
				}
			}
		}
	}

	// --- SELL logic ---
	if hasPosition {
		// Condition 1: Price above VWAP + overbought MFI
		if deviation > s.devThreshold && mfi > s.mfiSellThreshold {
			sig.Action = strategy.Sell
			sig.Strength = clamp(deviation/s.devThreshold*0.3, 0.1, 1.0)
			sig.Reason = fmt.Sprintf(
				"VWAP reversion sell: close=%.2f > VWAP=%.2f (dev=+%.2f%%), MFI=%.1f (overbought)",
				closePrice, vwap, deviation*100, mfi,
			)
		}

		// Condition 2: Extreme MFI → force exit
		if mfi > s.mfiExitThreshold {
			sig.Action = strategy.Sell
			sig.Strength = 0.9
			sig.Reason = fmt.Sprintf(
				"VWAP reversion exit: MFI=%.1f > %.0f (extreme overbought), close=%.2f, VWAP=%.2f",
				mfi, s.mfiExitThreshold, closePrice, vwap,
			)
		}
	}

	s.prevClose = closePrice
	s.prevVWAP = vwap
	s.prevMFI = mfi

	return sig, nil
}

func (s *VWAPReversionStrategy) OnTradeExecuted(trade *exchange.Trade) {
	// No special state tracking needed
}

// Ensure compile-time interface compliance.
var _ strategy.Strategy = (*VWAPReversionStrategy)(nil)
