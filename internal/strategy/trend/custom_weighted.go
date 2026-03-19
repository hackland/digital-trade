package trend

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/jayce/btc-trader/internal/exchange"
	"github.com/jayce/btc-trader/internal/strategy"
	"github.com/jayce/btc-trader/internal/strategy/modules"
)

// CustomWeightedStrategy allows free combination of indicator modules with
// user-defined weights. Configured entirely from the frontend API.
//
// Each module independently scores -1.0 (strong sell) to +1.0 (strong buy).
// The composite score is the weighted sum of all enabled modules.
//
// Features:
//   - Trend filter: EMA-based trend gate to avoid bear market entries
//   - Multi-timeframe filter: higher-TF EMA gate (e.g., 4h) to avoid counter-trend trades
//   - Signal confirmation: requires consecutive bars above threshold
//   - Cooldown: minimum bars between exit and next entry
//   - Minimum hold period: prevents premature exits
//   - ATR trailing stop: volatility-adaptive stop loss
type CustomWeightedStrategy struct {
	mu sync.RWMutex // protects reconfiguration

	// Active modules with weights
	weightedModules []weightedMod

	// Signal thresholds
	buyThreshold  float64
	sellThreshold float64

	// Trend filter: only buy when price > EMA(trendPeriod) on primary timeframe
	trendFilterEnabled bool
	trendPeriod        int // EMA period for trend filter (e.g., 50)

	// Multi-timeframe filter: gate BUY with higher-TF EMA trend
	htfEnabled  bool   // enable higher-timeframe filter
	htfInterval string // e.g., "4h" (must match snapshot.HTFInterval)
	htfPeriod   int    // EMA period on higher TF (e.g., 20 on 4h = 80h lookback)

	// Signal confirmation
	confirmBars  int
	confirmCount int // consecutive bars above buy threshold

	// Cooldown after exit
	cooldownBars  int
	cooldownCount int

	// Minimum hold period: don't sell before this many bars
	minHoldBars int

	// ATR trailing stop
	atrStopMult    float64
	atrPeriod      int
	highWaterMark  float64
	entryPrice     float64
	barsSinceEntry int
}

type weightedMod struct {
	module modules.ScoringModule
	weight float64
}

func NewCustomWeightedStrategy() *CustomWeightedStrategy {
	return &CustomWeightedStrategy{
		buyThreshold:       0.15,
		sellThreshold:      -0.50,
		trendFilterEnabled: true,
		trendPeriod:        50,
		htfEnabled:         false,
		htfInterval:        "4h",
		htfPeriod:          20,
		confirmBars:        1,
		cooldownBars:       2,
		minHoldBars:        6,
		atrStopMult:        3.0,
		atrPeriod:          14,
	}
}

func (s *CustomWeightedStrategy) Name() string {
	return "custom_weighted"
}

// Reconfigure hot-reloads strategy parameters. Thread-safe.
// Resets trading state (cooldown, confirm count, etc.) since config changed.
func (s *CustomWeightedStrategy) Reconfigure(cfg map[string]interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Reset trading state
	s.confirmCount = 0
	s.cooldownCount = 0
	s.barsSinceEntry = 0
	s.highWaterMark = 0
	s.entryPrice = 0
	s.weightedModules = nil // Clear modules before re-init

	return s.init(cfg)
}

// GetConfig returns the current running configuration.
func (s *CustomWeightedStrategy) GetConfig() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	mods := make([]map[string]interface{}, 0, len(s.weightedModules))
	for _, wm := range s.weightedModules {
		mods = append(mods, map[string]interface{}{
			"name":   wm.module.Name(),
			"weight": wm.weight,
		})
	}

	return map[string]interface{}{
		"modules":        mods,
		"buy_threshold":  s.buyThreshold,
		"sell_threshold": s.sellThreshold,
		"confirm_bars":   s.confirmBars,
		"cooldown_bars":  s.cooldownBars,
		"min_hold_bars":  s.minHoldBars,
		"atr_stop_mult":  s.atrStopMult,
		"atr_period":     s.atrPeriod,
		"trend_filter":   s.trendFilterEnabled,
		"trend_period":   s.trendPeriod,
		"htf_enabled":    s.htfEnabled,
		"htf_interval":   s.htfInterval,
		"htf_period":     s.htfPeriod,
	}
}

func (s *CustomWeightedStrategy) Init(cfg map[string]interface{}) error {
	return s.init(cfg)
}

func (s *CustomWeightedStrategy) init(cfg map[string]interface{}) error {
	// Parse control parameters
	if v, ok := cfg["buy_threshold"]; ok {
		s.buyThreshold = toFloat(v)
	}
	if v, ok := cfg["sell_threshold"]; ok {
		s.sellThreshold = toFloat(v)
	}
	if v, ok := cfg["confirm_bars"]; ok {
		s.confirmBars = toInt(v)
	}
	if v, ok := cfg["cooldown_bars"]; ok {
		s.cooldownBars = toInt(v)
	}
	if v, ok := cfg["atr_stop_mult"]; ok {
		s.atrStopMult = toFloat(v)
	}
	if v, ok := cfg["atr_period"]; ok {
		s.atrPeriod = toInt(v)
	}
	if v, ok := cfg["min_hold_bars"]; ok {
		s.minHoldBars = toInt(v)
	}
	if v, ok := cfg["trend_filter"]; ok {
		s.trendFilterEnabled = toBool(v)
	}
	if v, ok := cfg["trend_period"]; ok {
		s.trendPeriod = toInt(v)
	}

	// Multi-timeframe filter
	if v, ok := cfg["htf_enabled"]; ok {
		s.htfEnabled = toBool(v)
	}
	if v, ok := cfg["htf_interval"]; ok {
		if s2, ok := v.(string); ok && s2 != "" {
			s.htfInterval = s2
		}
	}
	if v, ok := cfg["htf_period"]; ok {
		s.htfPeriod = toInt(v)
	}

	// Parse modules from config
	// Expected format: "modules": [{"name": "rsi", "weight": 0.3, "params": {"period": 14}}, ...]
	modulesRaw, ok := cfg["modules"]
	if !ok {
		// Default modules if none specified
		return s.initDefaultModules()
	}

	modulesList, ok := modulesRaw.([]interface{})
	if !ok {
		return fmt.Errorf("modules must be an array")
	}

	for _, item := range modulesList {
		modCfg, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		name, _ := modCfg["name"].(string)
		if name == "" {
			continue
		}

		weight := 0.0
		if w, ok := modCfg["weight"]; ok {
			weight = toFloat(w)
		}
		if weight <= 0 {
			continue
		}

		params := map[string]interface{}{}
		if p, ok := modCfg["params"].(map[string]interface{}); ok {
			params = p
		}

		mod, found := modules.Create(name, params)
		if !found {
			return fmt.Errorf("unknown module: %s, available: %v", name, modules.Available())
		}

		s.weightedModules = append(s.weightedModules, weightedMod{module: mod, weight: weight})
	}

	if len(s.weightedModules) == 0 {
		return fmt.Errorf("no valid modules configured")
	}

	return nil
}

func (s *CustomWeightedStrategy) initDefaultModules() error {
	defaults := []struct {
		name   string
		weight float64
	}{
		{"ema_cross", 0.25},
		{"macd", 0.20},
		{"rsi", 0.15},
		{"mfi", 0.10},
		{"volume_ratio", 0.15},
		{"cmf", 0.15},
	}

	for _, d := range defaults {
		mod, _ := modules.Create(d.name, nil)
		s.weightedModules = append(s.weightedModules, weightedMod{module: mod, weight: d.weight})
	}
	return nil
}

func (s *CustomWeightedStrategy) RequiredIndicators() []strategy.IndicatorRequirement {
	seen := make(map[string]struct{})
	var result []strategy.IndicatorRequirement

	// Always need ATR for trailing stop
	atrKey := fmt.Sprintf("ATR_%d", s.atrPeriod)
	seen[atrKey] = struct{}{}
	result = append(result, strategy.IndicatorRequirement{
		Name: "ATR", Params: map[string]int{"period": s.atrPeriod},
	})

	// Need EMA for trend filter
	if s.trendFilterEnabled && s.trendPeriod > 0 {
		emaKey := fmt.Sprintf("EMA_%d", s.trendPeriod)
		seen[emaKey] = struct{}{}
		result = append(result, strategy.IndicatorRequirement{
			Name: "EMA", Params: map[string]int{"period": s.trendPeriod},
		})
	}

	for _, wm := range s.weightedModules {
		for _, req := range wm.module.RequiredIndicators() {
			key := req.Name + fmt.Sprintf("%v", req.Params)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, req)
		}
	}

	return result
}

// HTFIndicatorRequirements returns indicator requirements for the higher timeframe.
// The caller (trader/engine) should compute these on HTF klines and set them in snapshot.HTFIndicators.
func (s *CustomWeightedStrategy) HTFIndicatorRequirements() []strategy.IndicatorRequirement {
	if !s.htfEnabled || s.htfPeriod <= 0 {
		return nil
	}
	return []strategy.IndicatorRequirement{
		{Name: "EMA", Params: map[string]int{"period": s.htfPeriod}},
	}
}

// HTFInterval returns the configured higher-timeframe interval (e.g., "4h").
// Returns empty string if HTF filter is disabled.
func (s *CustomWeightedStrategy) HTFInterval() string {
	if !s.htfEnabled {
		return ""
	}
	return s.htfInterval
}

// HTFHistoryRequired returns minimum HTF klines needed.
func (s *CustomWeightedStrategy) HTFHistoryRequired() int {
	if !s.htfEnabled {
		return 0
	}
	return s.htfPeriod + 10
}

func (s *CustomWeightedStrategy) RequiredHistory() int {
	maxHist := 60 // minimum reasonable
	for _, wm := range s.weightedModules {
		if h := wm.module.RequiredHistory(); h > maxHist {
			maxHist = h
		}
	}
	// Trend filter needs more history
	if s.trendFilterEnabled && s.trendPeriod+10 > maxHist {
		maxHist = s.trendPeriod + 10
	}
	return maxHist + 10
}

func (s *CustomWeightedStrategy) Evaluate(ctx context.Context, snap *strategy.MarketSnapshot) (*strategy.Signal, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sig := &strategy.Signal{
		Action:     strategy.Hold,
		Symbol:     snap.Symbol,
		Strategy:   s.Name(),
		Timestamp:  snap.Timestamp,
		Indicators: make(map[string]float64),
	}

	// Compute scores from all modules
	composite := 0.0
	totalWeight := 0.0
	var scoreParts []string

	for _, wm := range s.weightedModules {
		score := wm.module.Score(snap)
		weighted := score * wm.weight
		composite += weighted
		totalWeight += wm.weight

		sig.Indicators[wm.module.Name()+"_score"] = score
		scoreParts = append(scoreParts, fmt.Sprintf("%s=%.2f", wm.module.Name(), score))
	}

	// Normalize if weights don't sum to 1
	if totalWeight > 0 && totalWeight != 1.0 {
		composite /= totalWeight
	}

	sig.Indicators["composite_score"] = composite

	hasPosition := snap.Position != nil && snap.Position.Quantity > 0
	closePrice := 0.0
	if len(snap.Klines) > 0 {
		closePrice = snap.Klines[len(snap.Klines)-1].Close
	}
	atr := snap.Indicators.ATR[s.atrPeriod]

	// Trend filter: check if price is above long-term EMA
	trendBullish := true // default: no filter
	if s.trendFilterEnabled && s.trendPeriod > 0 {
		trendEMA := snap.Indicators.EMA[s.trendPeriod]
		if trendEMA > 0 && closePrice > 0 {
			// Price must be above EMA to be bullish
			trendBullish = closePrice > trendEMA
			// Also track how far above/below (for signal indicator logging)
			trendDist := (closePrice - trendEMA) / trendEMA * 100
			sig.Indicators["trend_ema_dist_pct"] = trendDist
			sig.Indicators["trend_bullish"] = boolToFloat(trendBullish)
		}
	}

	// Multi-timeframe trend filter: check if higher TF is bullish
	htfBullish := true // default: no filter
	if s.htfEnabled && len(snap.HTFKlines) > 0 {
		htfEMA := snap.HTFIndicators.EMA[s.htfPeriod]
		htfPrice := snap.HTFKlines[len(snap.HTFKlines)-1].Close
		if htfEMA > 0 && htfPrice > 0 {
			htfBullish = htfPrice > htfEMA
			sig.Indicators["htf_ema_dist_pct"] = (htfPrice - htfEMA) / htfEMA * 100
			sig.Indicators["htf_bullish"] = boolToFloat(htfBullish)
		}
	}

	// --- BUY LOGIC ---
	if !hasPosition {
		// Cooldown check
		if s.cooldownCount > 0 {
			s.cooldownCount--
			return sig, nil
		}

		// Trend filter gate: don't even accumulate confirmCount in bear market
		if !trendBullish {
			s.confirmCount = 0
			return sig, nil
		}

		// Multi-TF gate: higher timeframe must also be bullish
		if !htfBullish {
			s.confirmCount = 0
			sig.Indicators["htf_blocked"] = 1.0
			return sig, nil
		}

		if composite >= s.buyThreshold {
			s.confirmCount++
		} else {
			s.confirmCount = 0
		}

		if s.confirmCount >= s.confirmBars {
			sig.Action = strategy.Buy
			sig.Strength = clamp(composite, 0.1, 1.0)
			sig.Reason = fmt.Sprintf(
				"Custom buy (score=%.2f, threshold=%.2f): %s",
				composite, s.buyThreshold, strings.Join(scoreParts, ", "),
			)
			s.confirmCount = 0
		}
	}

	// --- SELL LOGIC ---
	if hasPosition {
		s.barsSinceEntry++

		// Track high water mark
		if closePrice > s.highWaterMark {
			s.highWaterMark = closePrice
		}

		// Minimum hold period: don't sell too early (prevents signal flip-flop)
		if s.minHoldBars > 0 && s.barsSinceEntry < s.minHoldBars {
			return sig, nil
		}

		// Sell condition 1: ATR trailing stop (primary exit mechanism)
		if atr > 0 && s.highWaterMark > 0 {
			mult := s.atrStopMult

			// Profit-based tightening (existing logic, applied on top)
			if s.entryPrice > 0 {
				profitPct := (s.highWaterMark - s.entryPrice) / s.entryPrice * 100
				if profitPct > 30 {
					mult *= 0.65 // aggressive tighten at 30%+ profit
				} else if profitPct > 15 {
					mult *= 0.8 // moderate tighten at 15%+
				}
			}

			stopPrice := s.highWaterMark - atr*mult

			if closePrice <= stopPrice {
				sig.Action = strategy.Sell
				sig.Strength = 0.8
				sig.Reason = fmt.Sprintf(
					"ATR trailing stop: price=%.2f below stop=%.2f (high=%.2f, ATR=%.2f×%.1f)",
					closePrice, stopPrice, s.highWaterMark, atr, mult,
				)
			}
		}

		// Sell condition 2: Composite score deeply negative
		// Only triggers if sell_threshold is not disabled (> -1.0)
		if sig.Action != strategy.Sell && s.sellThreshold > -1.0 && composite <= s.sellThreshold {
			sig.Action = strategy.Sell
			sig.Strength = clamp(-composite, 0.1, 1.0)
			sig.Reason = fmt.Sprintf(
				"Custom sell (score=%.2f, threshold=%.2f): %s",
				composite, s.sellThreshold, strings.Join(scoreParts, ", "),
			)
		}
	}

	return sig, nil
}

func (s *CustomWeightedStrategy) OnTradeExecuted(trade *exchange.Trade) {
	if trade.Side == exchange.OrderSideBuy {
		s.entryPrice = trade.Price
		s.highWaterMark = trade.Price
		s.barsSinceEntry = 0
		s.confirmCount = 0
	} else {
		s.entryPrice = 0
		s.highWaterMark = 0
		s.barsSinceEntry = 0
		s.cooldownCount = s.cooldownBars // start cooldown
	}
}

// boolToFloat converts bool to float64 for indicator logging.
func boolToFloat(b bool) float64 {
	if b {
		return 1.0
	}
	return 0.0
}

var _ strategy.Strategy = (*CustomWeightedStrategy)(nil)
