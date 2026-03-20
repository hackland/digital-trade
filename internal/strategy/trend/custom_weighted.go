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

	// --- Short signal parameters (alert-only, independent of long) ---
	shortEnabled      bool
	shortThreshold    float64 // composite < this → Short signal
	coverThreshold    float64 // composite > this → Cover signal
	shortConfirmBars  int
	shortMinHoldBars  int
	shortATRStopMult  float64
	shortCooldownBars int

	// Short runtime state (virtual position for signal tracking)
	shortConfirmCount   int
	shortCooldownCount  int
	inShortPosition     bool
	shortEntryPrice     float64
	shortLowWaterMark   float64 // lowest price since short entry (for trailing stop)
	shortBarsSinceEntry int

	// Last evaluation diagnostics (updated every Evaluate call)
	lastDiag *Diagnostics
}

// Diagnostics holds the latest strategy evaluation state for live monitoring.
type Diagnostics struct {
	Timestamp      string             `json:"timestamp"` // last eval time
	Symbol         string             `json:"symbol"`
	Action         string             `json:"action"` // last signal action
	CompositeScore float64            `json:"composite_score"`
	ModuleScores   map[string]float64 `json:"module_scores"`  // module → raw score
	ModuleWeights  map[string]float64 `json:"module_weights"` // module → weight
	BuyThreshold   float64            `json:"buy_threshold"`
	SellThreshold  float64            `json:"sell_threshold"`
	// Runtime state
	HasPosition    bool    `json:"has_position"`
	EntryPrice     float64 `json:"entry_price"`
	HighWaterMark  float64 `json:"high_water_mark"`
	BarsSinceEntry int     `json:"bars_since_entry"`
	ConfirmCount   int     `json:"confirm_count"`
	ConfirmBars    int     `json:"confirm_bars"`
	CooldownCount  int     `json:"cooldown_count"`
	CooldownBars   int     `json:"cooldown_bars"`
	MinHoldBars    int     `json:"min_hold_bars"`
	// Trend filter
	TrendFilterOn bool    `json:"trend_filter_on"`
	TrendBullish  bool    `json:"trend_bullish"`
	TrendEMADist  float64 `json:"trend_ema_dist_pct"`
	// HTF filter
	HTFEnabled bool    `json:"htf_enabled"`
	HTFBullish bool    `json:"htf_bullish"`
	HTFBlocked bool    `json:"htf_blocked"`
	HTFEMADist float64 `json:"htf_ema_dist_pct"`
	// ATR stop
	ATRStopMult float64 `json:"atr_stop_mult"`
	ATRValue    float64 `json:"atr_value"`
	StopPrice   float64 `json:"stop_price"`
	ClosePrice  float64 `json:"close_price"`
	// Reason (human readable)
	HoldReason string `json:"hold_reason"`
	Reason     string `json:"reason"`
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
		// Short defaults
		shortEnabled:      false,
		shortThreshold:    -0.25,
		coverThreshold:    0.15,
		shortConfirmBars:  1,
		shortMinHoldBars:  12,
		shortATRStopMult:  3.0,
		shortCooldownBars: 4,
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
	// Reset short state
	s.shortConfirmCount = 0
	s.shortCooldownCount = 0
	s.inShortPosition = false
	s.shortEntryPrice = 0
	s.shortLowWaterMark = 0
	s.shortBarsSinceEntry = 0
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
		// Short params
		"short_enabled":       s.shortEnabled,
		"short_threshold":     s.shortThreshold,
		"cover_threshold":     s.coverThreshold,
		"short_confirm_bars":  s.shortConfirmBars,
		"short_min_hold_bars": s.shortMinHoldBars,
		"short_atr_stop_mult": s.shortATRStopMult,
		"short_cooldown_bars": s.shortCooldownBars,
	}
}

// GetDiagnostics returns the latest evaluation diagnostics. Thread-safe.
func (s *CustomWeightedStrategy) GetDiagnostics() *Diagnostics {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastDiag
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

	// Short parameters
	if v, ok := cfg["short_enabled"]; ok {
		s.shortEnabled = toBool(v)
	}
	if v, ok := cfg["short_threshold"]; ok {
		s.shortThreshold = toFloat(v)
	}
	if v, ok := cfg["cover_threshold"]; ok {
		s.coverThreshold = toFloat(v)
	}
	if v, ok := cfg["short_confirm_bars"]; ok {
		s.shortConfirmBars = toInt(v)
	}
	if v, ok := cfg["short_min_hold_bars"]; ok {
		s.shortMinHoldBars = toInt(v)
	}
	if v, ok := cfg["short_atr_stop_mult"]; ok {
		s.shortATRStopMult = toFloat(v)
	}
	if v, ok := cfg["short_cooldown_bars"]; ok {
		s.shortCooldownBars = toInt(v)
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

	moduleScores := make(map[string]float64)
	moduleWeights := make(map[string]float64)
	for _, wm := range s.weightedModules {
		score := wm.module.Score(snap)
		weighted := score * wm.weight
		composite += weighted
		totalWeight += wm.weight

		sig.Indicators[wm.module.Name()+"_score"] = score
		scoreParts = append(scoreParts, fmt.Sprintf("%s=%.2f", wm.module.Name(), score))
		moduleScores[wm.module.Name()] = score
		moduleWeights[wm.module.Name()] = wm.weight
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
	trendDist := 0.0
	if s.trendFilterEnabled && s.trendPeriod > 0 {
		trendEMA := snap.Indicators.EMA[s.trendPeriod]
		if trendEMA > 0 && closePrice > 0 {
			trendBullish = closePrice > trendEMA
			trendDist = (closePrice - trendEMA) / trendEMA * 100
			sig.Indicators["trend_ema_dist_pct"] = trendDist
			sig.Indicators["trend_bullish"] = boolToFloat(trendBullish)
		}
	}

	// Multi-timeframe trend filter: check if higher TF is bullish
	htfBullish := true // default: no filter
	htfBlocked := false
	htfDist := 0.0
	if s.htfEnabled && len(snap.HTFKlines) > 0 {
		htfEMA := snap.HTFIndicators.EMA[s.htfPeriod]
		htfPrice := snap.HTFKlines[len(snap.HTFKlines)-1].Close
		if htfEMA > 0 && htfPrice > 0 {
			htfBullish = htfPrice > htfEMA
			htfDist = (htfPrice - htfEMA) / htfEMA * 100
			sig.Indicators["htf_ema_dist_pct"] = htfDist
			sig.Indicators["htf_bullish"] = boolToFloat(htfBullish)
		}
	}

	// holdReason tracks why no buy/sell was triggered (for diagnostics)
	holdReason := ""
	stopPrice := 0.0

	// --- BUY LOGIC ---
	if !hasPosition {
		if s.cooldownCount > 0 {
			holdReason = fmt.Sprintf("冷却期中，剩余 %d 根K线", s.cooldownCount)
			s.cooldownCount--
		} else if !trendBullish {
			holdReason = fmt.Sprintf("趋势过滤：价格低于EMA(%d)，距离 %.2f%%", s.trendPeriod, trendDist)
			s.confirmCount = 0
		} else if !htfBullish {
			holdReason = fmt.Sprintf("大周期过滤：HTF价格低于EMA(%d)，距离 %.2f%%", s.htfPeriod, htfDist)
			s.confirmCount = 0
			htfBlocked = true
			sig.Indicators["htf_blocked"] = 1.0
		} else if composite < s.buyThreshold {
			holdReason = fmt.Sprintf("综合评分 %.3f 未达买入阈值 %.2f（差 %.3f）", composite, s.buyThreshold, s.buyThreshold-composite)
			s.confirmCount = 0
		} else {
			s.confirmCount++
			if s.confirmCount >= s.confirmBars {
				sig.Action = strategy.Buy
				sig.Strength = clamp(composite, 0.1, 1.0)
				sig.Reason = fmt.Sprintf(
					"Custom buy (score=%.2f, threshold=%.2f): %s",
					composite, s.buyThreshold, strings.Join(scoreParts, ", "),
				)
				s.confirmCount = 0
			} else {
				holdReason = fmt.Sprintf("确认中 %d/%d 根K线（评分 %.3f 已达阈值 %.2f）", s.confirmCount, s.confirmBars, composite, s.buyThreshold)
			}
		}
	}

	// --- SELL LOGIC ---
	if hasPosition {
		s.barsSinceEntry++

		if closePrice > s.highWaterMark {
			s.highWaterMark = closePrice
		}

		if s.minHoldBars > 0 && s.barsSinceEntry < s.minHoldBars {
			holdReason = fmt.Sprintf("最短持仓期中 %d/%d 根K线", s.barsSinceEntry, s.minHoldBars)
		} else {
			// Sell condition 1: ATR trailing stop
			if atr > 0 && s.highWaterMark > 0 {
				mult := s.atrStopMult
				if s.entryPrice > 0 {
					profitPct := (s.highWaterMark - s.entryPrice) / s.entryPrice * 100
					if profitPct > 30 {
						mult *= 0.65
					} else if profitPct > 15 {
						mult *= 0.8
					}
				}
				stopPrice = s.highWaterMark - atr*mult
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
			if sig.Action != strategy.Sell && s.sellThreshold > -1.0 && composite <= s.sellThreshold {
				sig.Action = strategy.Sell
				sig.Strength = clamp(-composite, 0.1, 1.0)
				sig.Reason = fmt.Sprintf(
					"Custom sell (score=%.2f, threshold=%.2f): %s",
					composite, s.sellThreshold, strings.Join(scoreParts, ", "),
				)
			}

			if sig.Action == strategy.Hold {
				holdReason = fmt.Sprintf("持仓中 %d 根K线，止损价=%.2f（当前=%.2f），评分=%.3f 未触发卖出阈值 %.2f",
					s.barsSinceEntry, stopPrice, closePrice, composite, s.sellThreshold)
			}
		}
	}

	// --- SHORT LOGIC (independent of long, evaluated when long action is Hold) ---
	if s.shortEnabled && sig.Action == strategy.Hold {
		sig = s.evaluateShort(sig, composite, scoreParts, closePrice, atr, trendBullish, htfBullish)
	}

	// --- Save diagnostics ---
	s.lastDiag = &Diagnostics{
		Timestamp:      snap.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
		Symbol:         snap.Symbol,
		Action:         sig.Action.String(),
		CompositeScore: composite,
		ModuleScores:   moduleScores,
		ModuleWeights:  moduleWeights,
		BuyThreshold:   s.buyThreshold,
		SellThreshold:  s.sellThreshold,
		HasPosition:    hasPosition,
		EntryPrice:     s.entryPrice,
		HighWaterMark:  s.highWaterMark,
		BarsSinceEntry: s.barsSinceEntry,
		ConfirmCount:   s.confirmCount,
		ConfirmBars:    s.confirmBars,
		CooldownCount:  s.cooldownCount,
		CooldownBars:   s.cooldownBars,
		MinHoldBars:    s.minHoldBars,
		TrendFilterOn:  s.trendFilterEnabled,
		TrendBullish:   trendBullish,
		TrendEMADist:   trendDist,
		HTFEnabled:     s.htfEnabled,
		HTFBullish:     htfBullish,
		HTFBlocked:     htfBlocked,
		HTFEMADist:     htfDist,
		ATRStopMult:    s.atrStopMult,
		ATRValue:       atr,
		StopPrice:      stopPrice,
		ClosePrice:     closePrice,
		HoldReason:     holdReason,
		Reason:         sig.Reason,
	}

	return sig, nil
}

// evaluateShort handles virtual short position management and signal generation.
// Short entry: composite deeply negative + bearish trend confirmation.
// Short exit: composite turns positive OR ATR trailing stop (inverted).
func (s *CustomWeightedStrategy) evaluateShort(sig *strategy.Signal, composite float64, scoreParts []string, closePrice, atr float64, trendBullish, htfBullish bool) *strategy.Signal {
	if s.inShortPosition {
		s.shortBarsSinceEntry++

		// Track low water mark (lowest price since entry)
		if closePrice < s.shortLowWaterMark || s.shortLowWaterMark == 0 {
			s.shortLowWaterMark = closePrice
		}

		// Min hold period
		if s.shortMinHoldBars > 0 && s.shortBarsSinceEntry < s.shortMinHoldBars {
			return sig
		}

		// Cover condition 1: ATR trailing stop (inverted — price rises above low + ATR*mult)
		if atr > 0 && s.shortLowWaterMark > 0 && s.shortATRStopMult > 0 {
			mult := s.shortATRStopMult

			// Profit-based tightening for shorts
			if s.shortEntryPrice > 0 {
				profitPct := (s.shortEntryPrice - s.shortLowWaterMark) / s.shortEntryPrice * 100
				if profitPct > 30 {
					mult *= 0.65
				} else if profitPct > 15 {
					mult *= 0.8
				}
			}

			stopPrice := s.shortLowWaterMark + atr*mult
			if closePrice >= stopPrice {
				sig.Action = strategy.Cover
				sig.Strength = 0.8
				sig.Reason = fmt.Sprintf(
					"Short ATR trailing stop: price=%.2f above stop=%.2f (low=%.2f, ATR=%.2f×%.1f)",
					closePrice, stopPrice, s.shortLowWaterMark, atr, mult,
				)
				return sig
			}
		}

		// Cover condition 2: composite score turns strongly positive
		if s.coverThreshold < 1.0 && composite >= s.coverThreshold {
			sig.Action = strategy.Cover
			sig.Strength = clamp(composite, 0.1, 1.0)
			sig.Reason = fmt.Sprintf(
				"Short cover (score=%.2f, threshold=%.2f): %s",
				composite, s.coverThreshold, strings.Join(scoreParts, ", "),
			)
			return sig
		}
	} else {
		// Not in short position — check for short entry

		// Short cooldown
		if s.shortCooldownCount > 0 {
			s.shortCooldownCount--
			return sig
		}

		// Trend filter: only short when trend is BEARISH (price below EMA)
		if s.trendFilterEnabled && trendBullish {
			s.shortConfirmCount = 0
			return sig
		}

		// HTF filter: only short when higher TF is BEARISH
		if s.htfEnabled && htfBullish {
			s.shortConfirmCount = 0
			return sig
		}

		// Score must be below short threshold
		if composite <= s.shortThreshold {
			s.shortConfirmCount++
		} else {
			s.shortConfirmCount = 0
		}

		if s.shortConfirmCount >= s.shortConfirmBars {
			sig.Action = strategy.Short
			sig.Strength = clamp(-composite, 0.1, 1.0)
			sig.Reason = fmt.Sprintf(
				"Short entry (score=%.2f, threshold=%.2f): %s",
				composite, s.shortThreshold, strings.Join(scoreParts, ", "),
			)
			s.shortConfirmCount = 0
		}
	}

	return sig
}

// OnShortSignalProcessed updates virtual short position state.
// Called by both backtest engine and live trader when a short signal is processed.
func (s *CustomWeightedStrategy) OnShortSignalProcessed(action strategy.Action, price float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch action {
	case strategy.Short:
		s.inShortPosition = true
		s.shortEntryPrice = price
		s.shortLowWaterMark = price
		s.shortBarsSinceEntry = 0
		s.shortConfirmCount = 0
	case strategy.Cover:
		s.inShortPosition = false
		s.shortEntryPrice = 0
		s.shortLowWaterMark = 0
		s.shortBarsSinceEntry = 0
		s.shortCooldownCount = s.shortCooldownBars
	}
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
