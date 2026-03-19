package trend

import (
	"context"
	"fmt"
	"strings"

	"github.com/jayce/btc-trader/internal/exchange"
	"github.com/jayce/btc-trader/internal/strategy"
)

// CompositeScoreStrategy uses a weighted scoring system across multiple indicators.
//
// Instead of boolean AND/OR logic, each indicator contributes a normalized score
// from -1.0 (strong sell) to +1.0 (strong buy). The weighted sum of all scores
// determines the trading action.
//
// Indicators and default weights:
//   - Trend (EMA cross):  30%  → direction of the market
//   - Momentum (RSI):     20%  → overbought/oversold + momentum shift
//   - Money Flow (MFI):   20%  → real money flowing in/out
//   - Volume:             15%  → volume confirmation
//   - Buying Pressure:    15%  → CMF buying/selling pressure
//
// Buy:  composite score > buyThreshold (default 0.4)
// Sell: composite score < sellThreshold (default -0.3)  OR  trailing stop hit
type CompositeScoreStrategy struct {
	// EMA
	fastPeriod int
	slowPeriod int
	// RSI
	rsiPeriod     int
	rsiOverbought float64
	rsiOversold   float64
	// MFI
	mfiPeriod int
	// Volume
	volumePeriod int
	// CMF
	cmfPeriod int

	// Weights (should sum to 1.0)
	weightTrend    float64
	weightMomentum float64
	weightMFI      float64
	weightVolume   float64
	weightCMF      float64

	// Thresholds
	buyThreshold  float64 // composite score above this → buy
	sellThreshold float64 // composite score below this → sell

	// Trailing stop
	trailingStopPct float64 // e.g., 0.03 = 3% trailing stop
	highWaterMark   float64 // track highest price since entry

	// Anti-whipsaw controls
	minBarsSell    int     // minimum bars held before strategy-level sell allowed
	emaDeadZonePct float64 // EMA crossover dead zone (e.g., 0.002 = 0.2%)

	// State
	prevFastEMA    float64
	prevSlowEMA    float64
	prevRSI        float64
	prevMFI        float64
	entryPrice     float64
	initialized    bool
	barsSinceEntry int
}

// NewCompositeScoreStrategy creates the strategy with default parameters.
func NewCompositeScoreStrategy() *CompositeScoreStrategy {
	return &CompositeScoreStrategy{
		fastPeriod:      9,
		slowPeriod:      21,
		rsiPeriod:       14,
		rsiOverbought:   70,
		rsiOversold:     30,
		mfiPeriod:       14,
		volumePeriod:    20,
		cmfPeriod:       20,
		weightTrend:     0.30,
		weightMomentum:  0.20,
		weightMFI:       0.20,
		weightVolume:    0.15,
		weightCMF:       0.15,
		buyThreshold:    0.4,
		sellThreshold:   -0.4,
		trailingStopPct: 0.03,   // 3% trailing stop (适配1h框架)
		minBarsSell:     3,      // 至少持仓3根K线
		emaDeadZonePct:  0.002,  // EMA交叉死区 0.2%
	}
}

func (s *CompositeScoreStrategy) Name() string {
	return "composite_score"
}

func (s *CompositeScoreStrategy) Init(cfg map[string]interface{}) error {
	if v, ok := cfg["fast_period"]; ok {
		s.fastPeriod = toInt(v)
	}
	if v, ok := cfg["slow_period"]; ok {
		s.slowPeriod = toInt(v)
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
	if v, ok := cfg["mfi_period"]; ok {
		s.mfiPeriod = toInt(v)
	}
	if v, ok := cfg["volume_period"]; ok {
		s.volumePeriod = toInt(v)
	}
	if v, ok := cfg["cmf_period"]; ok {
		s.cmfPeriod = toInt(v)
	}
	if v, ok := cfg["weight_trend"]; ok {
		s.weightTrend = toFloat(v)
	}
	if v, ok := cfg["weight_momentum"]; ok {
		s.weightMomentum = toFloat(v)
	}
	if v, ok := cfg["weight_mfi"]; ok {
		s.weightMFI = toFloat(v)
	}
	if v, ok := cfg["weight_volume"]; ok {
		s.weightVolume = toFloat(v)
	}
	if v, ok := cfg["weight_cmf"]; ok {
		s.weightCMF = toFloat(v)
	}
	if v, ok := cfg["buy_threshold"]; ok {
		s.buyThreshold = toFloat(v)
	}
	if v, ok := cfg["sell_threshold"]; ok {
		s.sellThreshold = toFloat(v)
	}
	if v, ok := cfg["trailing_stop_pct"]; ok {
		s.trailingStopPct = toFloat(v)
	}
	if v, ok := cfg["min_bars_before_sell"]; ok {
		s.minBarsSell = toInt(v)
	}
	if v, ok := cfg["ema_dead_zone_pct"]; ok {
		s.emaDeadZonePct = toFloat(v)
	}
	return nil
}

func (s *CompositeScoreStrategy) RequiredIndicators() []strategy.IndicatorRequirement {
	return []strategy.IndicatorRequirement{
		{Name: "EMA", Params: map[string]int{"period": s.fastPeriod}},
		{Name: "EMA", Params: map[string]int{"period": s.slowPeriod}},
		{Name: "RSI", Params: map[string]int{"period": s.rsiPeriod}},
		{Name: "MFI", Params: map[string]int{"period": s.mfiPeriod}},
		{Name: "VolumeSMA", Params: map[string]int{"period": s.volumePeriod}},
		{Name: "CMF", Params: map[string]int{"period": s.cmfPeriod}},
		{Name: "ATR", Params: map[string]int{"period": 14}},
	}
}

func (s *CompositeScoreStrategy) RequiredHistory() int {
	maxPeriod := s.slowPeriod
	if s.volumePeriod > maxPeriod {
		maxPeriod = s.volumePeriod
	}
	if s.cmfPeriod > maxPeriod {
		maxPeriod = s.cmfPeriod
	}
	return maxPeriod + 15
}

func (s *CompositeScoreStrategy) Evaluate(ctx context.Context, snap *strategy.MarketSnapshot) (*strategy.Signal, error) {
	fastEMA := snap.Indicators.EMA[s.fastPeriod]
	slowEMA := snap.Indicators.EMA[s.slowPeriod]
	rsi := snap.Indicators.RSI[s.rsiPeriod]
	mfi := snap.Indicators.MFI[s.mfiPeriod]
	cmf := snap.Indicators.CMF[s.cmfPeriod]
	volumeAvg := snap.Indicators.VolumeSMA[s.volumePeriod]
	atr := snap.Indicators.ATR[14]

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
			"rsi":        rsi,
			"mfi":        mfi,
			"cmf":        cmf,
			"volume":     volume,
			"volume_avg": volumeAvg,
			"atr":        atr,
		},
	}

	if !s.initialized {
		s.prevFastEMA = fastEMA
		s.prevSlowEMA = slowEMA
		s.prevRSI = rsi
		s.prevMFI = mfi
		s.initialized = true
		return sig, nil
	}

	hasPosition := snap.Position != nil && snap.Position.Quantity > 0

	// --- Compute individual scores (-1.0 to +1.0) ---
	var scores []scoreEntry

	// 1. Trend score: EMA alignment + crossover momentum
	trendScore := s.computeTrendScore(fastEMA, slowEMA, closePrice)
	scores = append(scores, scoreEntry{"trend", trendScore, s.weightTrend})

	// 2. Momentum score: RSI position + direction
	momentumScore := s.computeMomentumScore(rsi)
	scores = append(scores, scoreEntry{"momentum", momentumScore, s.weightMomentum})

	// 3. Money flow score: MFI
	mfiScore := s.computeMFIScore(mfi)
	scores = append(scores, scoreEntry{"mfi", mfiScore, s.weightMFI})

	// 4. Volume score: relative volume
	volumeScore := s.computeVolumeScore(volume, volumeAvg)
	scores = append(scores, scoreEntry{"volume", volumeScore, s.weightVolume})

	// 5. CMF score: buying/selling pressure
	cmfScore := s.computeCMFScore(cmf)
	scores = append(scores, scoreEntry{"cmf", cmfScore, s.weightCMF})

	// --- Composite score ---
	composite := 0.0
	for _, sc := range scores {
		composite += sc.score * sc.weight
	}

	sig.Indicators["composite_score"] = composite
	sig.Indicators["trend_score"] = trendScore
	sig.Indicators["momentum_score"] = momentumScore
	sig.Indicators["mfi_score"] = mfiScore
	sig.Indicators["volume_score"] = volumeScore
	sig.Indicators["cmf_score"] = cmfScore

	// --- Trading logic ---
	if !hasPosition {
		// BUY: composite score exceeds buy threshold
		if composite >= s.buyThreshold {
			sig.Action = strategy.Buy
			sig.Strength = clamp(composite, 0.1, 1.0)
			sig.Reason = fmt.Sprintf(
				"Composite buy (score=%.2f): %s",
				composite, s.formatScores(scores),
			)
		}
	} else {
		s.barsSinceEntry++

		// Track high water mark for trailing stop
		if closePrice > s.highWaterMark {
			s.highWaterMark = closePrice
		}

		// SELL condition 1: Trailing stop (always active, ignores minBarsSell)
		if s.trailingStopPct > 0 && s.highWaterMark > 0 {
			dropPct := (s.highWaterMark - closePrice) / s.highWaterMark
			if dropPct >= s.trailingStopPct {
				sig.Action = strategy.Sell
				sig.Strength = 0.8
				sig.Reason = fmt.Sprintf(
					"Trailing stop: price=%.2f dropped %.1f%% from high=%.2f (threshold=%.1f%%)",
					closePrice, dropPct*100, s.highWaterMark, s.trailingStopPct*100,
				)
			}
		}

		// Strategy-level sells require minimum holding period to avoid whipsaws.
		// Trailing stop (above) and exchange-level SL/TP are NOT affected.
		canStrategySell := s.barsSinceEntry >= s.minBarsSell

		// SELL condition 2: Composite score deeply negative
		if sig.Action != strategy.Sell && canStrategySell && composite <= s.sellThreshold {
			sig.Action = strategy.Sell
			sig.Strength = clamp(-composite, 0.1, 1.0)
			sig.Reason = fmt.Sprintf(
				"Composite sell (score=%.2f, bars=%d): %s",
				composite, s.barsSinceEntry, s.formatScores(scores),
			)
		}

		// SELL condition 3: Strong reversal signals (any single indicator extreme)
		if sig.Action != strategy.Sell && canStrategySell {
			if rsi > 80 && mfi > 80 {
				sig.Action = strategy.Sell
				sig.Strength = 0.7
				sig.Reason = fmt.Sprintf(
					"Extreme overbought exit: RSI=%.1f, MFI=%.1f (bars=%d)",
					rsi, mfi, s.barsSinceEntry,
				)
			}
		}
	}

	// Update state
	s.prevFastEMA = fastEMA
	s.prevSlowEMA = slowEMA
	s.prevRSI = rsi
	s.prevMFI = mfi

	return sig, nil
}

// --- Score computation functions ---
// Each returns a value from -1.0 (strong sell) to +1.0 (strong buy)

// computeTrendScore: EMA alignment and price position.
// Includes a dead zone around EMA crossover to prevent whipsaw signals.
func (s *CompositeScoreStrategy) computeTrendScore(fastEMA, slowEMA, close float64) float64 {
	if slowEMA == 0 {
		return 0
	}

	score := 0.0

	// EMA alignment: fast > slow = bullish
	emaDiff := (fastEMA - slowEMA) / slowEMA * 100
	// Normalize: ±1% difference maps to ±0.5 score
	score += clamp(emaDiff*50, -0.5, 0.5)

	// Price vs fast EMA: price above fast EMA = bullish
	if fastEMA > 0 {
		priceVsEMA := (close - fastEMA) / fastEMA * 100
		score += clamp(priceVsEMA*25, -0.3, 0.3)
	}

	// Crossover momentum with dead zone buffer:
	// Only count as a real crossover if the EMA separation exceeds the dead zone.
	// This prevents rapid flip-flopping when price oscillates around EMA.
	deadZone := s.emaDeadZonePct * slowEMA // absolute dead zone in price units
	prevBullish := s.prevFastEMA > s.prevSlowEMA+deadZone
	prevBearish := s.prevFastEMA < s.prevSlowEMA-deadZone
	nowBullish := fastEMA > slowEMA+deadZone
	nowBearish := fastEMA < slowEMA-deadZone

	if !prevBullish && nowBullish {
		score += 0.2 // confirmed golden cross (outside dead zone)
	} else if !prevBearish && nowBearish {
		score -= 0.2 // confirmed death cross (outside dead zone)
	}

	return clamp(score, -1.0, 1.0)
}

// computeMomentumScore: RSI-based momentum
func (s *CompositeScoreStrategy) computeMomentumScore(rsi float64) float64 {
	score := 0.0

	// RSI position: map 0-100 to score
	// RSI 50 = neutral (0), RSI 30 = oversold (+0.5 buy), RSI 70 = overbought (-0.5 sell)
	if rsi < s.rsiOversold {
		// Oversold → buy signal, stronger as RSI drops
		score = clamp((s.rsiOversold-rsi)/s.rsiOversold, 0.2, 0.7)
	} else if rsi > s.rsiOverbought {
		// Overbought → sell signal
		score = -clamp((rsi-s.rsiOverbought)/(100-s.rsiOverbought), 0.2, 0.7)
	} else {
		// In normal range: slight bias based on position
		// 40-60 = neutral, below 40 = slight buy, above 60 = slight sell
		mid := (s.rsiOverbought + s.rsiOversold) / 2
		score = clamp((mid-rsi)/mid*0.3, -0.3, 0.3)
	}

	// RSI momentum: rising RSI = bullish
	rsiChange := rsi - s.prevRSI
	score += clamp(rsiChange/10, -0.3, 0.3)

	return clamp(score, -1.0, 1.0)
}

// computeMFIScore: Money Flow Index based score
func (s *CompositeScoreStrategy) computeMFIScore(mfi float64) float64 {
	score := 0.0

	// MFI position
	if mfi < 20 {
		score = 0.7 // strong buy: money flow exhausted
	} else if mfi < 35 {
		score = 0.4 // moderate buy
	} else if mfi > 80 {
		score = -0.7 // strong sell: overbought money flow
	} else if mfi > 65 {
		score = -0.3 // moderate sell pressure
	} else {
		// Neutral zone 35-65: slight bias based on direction
		score = clamp((50-mfi)/50*0.2, -0.2, 0.2)
	}

	// MFI momentum
	mfiChange := mfi - s.prevMFI
	score += clamp(mfiChange/15, -0.3, 0.3)

	return clamp(score, -1.0, 1.0)
}

// computeVolumeScore: relative volume
func (s *CompositeScoreStrategy) computeVolumeScore(volume, avgVolume float64) float64 {
	if avgVolume <= 0 {
		return 0
	}

	ratio := volume / avgVolume

	// Volume ratio scoring:
	// < 0.5x avg = low volume (-0.3, weak/unreliable move)
	// 0.5-1.0x = normal (0)
	// 1.0-2.0x = above average (+0.3 to +0.6)
	// > 2.0x = high volume (+0.6 to +1.0, strong confirmation)
	if ratio < 0.5 {
		return -0.3
	} else if ratio < 0.8 {
		return -0.1
	} else if ratio < 1.2 {
		return 0.1
	} else if ratio < 2.0 {
		return clamp((ratio-1.0)*0.5, 0.1, 0.6)
	}
	return clamp(0.6+(ratio-2.0)*0.2, 0.6, 1.0)
}

// computeCMFScore: Chaikin Money Flow
func (s *CompositeScoreStrategy) computeCMFScore(cmf float64) float64 {
	// CMF range is -1 to +1, but typically stays within -0.3 to +0.3
	// Amplify and use directly
	return clamp(cmf*3, -1.0, 1.0)
}

// --- Helpers ---

type scoreEntry struct {
	name   string
	score  float64
	weight float64
}

func (s *CompositeScoreStrategy) formatScores(scores []scoreEntry) string {
	parts := make([]string, 0, len(scores))
	for _, sc := range scores {
		parts = append(parts, fmt.Sprintf("%s=%.2f", sc.name, sc.score))
	}
	return strings.Join(parts, ", ")
}

func (s *CompositeScoreStrategy) OnTradeExecuted(trade *exchange.Trade) {
	if trade.Side == exchange.OrderSideBuy {
		s.entryPrice = trade.Price
		s.highWaterMark = trade.Price
		s.barsSinceEntry = 0
	} else {
		s.entryPrice = 0
		s.highWaterMark = 0
		s.barsSinceEntry = 0
	}
}

// Ensure compile-time interface compliance.
var _ strategy.Strategy = (*CompositeScoreStrategy)(nil)
