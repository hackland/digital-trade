package trend

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

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
	mu sync.RWMutex // protects reconfiguration and all per-symbol state below

	// Module configuration (name/weight/params), shared across symbols.
	// Used to spin up an independent set of module instances per symbol —
	// each module (e.g. MACD, ema_cross) carries its own "prev value" state
	// for crossover detection, so instances must never be shared between symbols.
	moduleSpecs []moduleSpec
	// templateModules mirrors moduleSpecs as instantiated modules, used only
	// to answer metadata queries (RequiredIndicators/RequiredHistory/GetConfig).
	// Its internal running state is never used for scoring.
	templateModules []weightedMod

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
	confirmBars int

	// Cooldown after exit
	cooldownBars int

	// Minimum hold period: don't sell before this many bars
	minHoldBars int

	// ATR trailing stop
	atrStopMult          float64
	atrPeriod            int
	hardStopPct          float64 // max loss % from entry before ATR activates (0=disabled)
	atrActivateProfitPct float64 // ATR trailing only after this % profit (0=always active)
	klineInterval        string  // "1m", "1h" 等,用于重启恢复 barsSinceEntry
	emaCrossMin          float64 // 做多时 ema_cross 模块分的最低门槛，0=禁用

	// --- Short signal parameters (alert-only, independent of long) ---
	shortEnabled      bool
	shortThreshold    float64 // composite < this → Short signal
	coverThreshold    float64 // composite > this → Cover signal
	shortConfirmBars  int
	shortMinHoldBars  int
	shortATRStopMult  float64
	shortCooldownBars int

	// Short optimization parameters
	shortATRStopActivatePct float64 // ATR止损激活门槛: 浮盈达到此百分比才启用ATR trailing stop (0=始终启用)
	shortMinScoreAbs        float64 // 最小信号绝对值: |composite| 必须 >= 此值才开空 (0=不过滤)
	shortATRVolatilityMin   float64 // 最小波动率: ATR/price百分比 >= 此值才开空 (0=不过滤)

	// ADX trend strength filter for shorts
	shortADXEnabled  bool    // 是否启用ADX过滤
	shortADXPeriod   int     // ADX周期 (默认14)
	shortADXMin      float64 // ADX最小值, >此值才开空 (默认20, 表示有趋势)
	shortADXDIFilter bool    // 是否要求 -DI > +DI (空头趋势确认)

	// Per-symbol runtime state: modules instances, position tracking, virtual
	// short position. Keyed by symbol so BTCUSDT and ETHUSDT (etc.) never
	// share an entry price / high-water mark / indicator history.
	states map[string]*symbolState

	// Last evaluation diagnostics per symbol (updated every Evaluate call)
	lastDiagBySymbol map[string]*Diagnostics
}

// moduleSpec is the configuration needed to (re)instantiate a scoring module.
type moduleSpec struct {
	name   string
	weight float64
	params map[string]interface{}
}

// symbolState holds all per-symbol runtime/mutable state: module instances
// (each with independent internal history for crossover/momentum detection),
// long position tracking, and virtual short position tracking.
type symbolState struct {
	weightedModules []weightedMod

	// Signal confirmation / cooldown (long side)
	confirmCount  int // consecutive bars above buy threshold
	cooldownCount int

	// Long position tracking
	highWaterMark  float64
	entryPrice     float64
	barsSinceEntry int

	// Short runtime state (virtual position for signal tracking)
	shortConfirmCount   int
	shortCooldownCount  int
	inShortPosition     bool
	shortEntryPrice     float64
	shortLowWaterMark   float64 // lowest price since short entry (for trailing stop)
	shortBarsSinceEntry int
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
	ATRStopMult   float64 `json:"atr_stop_mult"`
	ATRValue      float64 `json:"atr_value"`
	StopPrice     float64 `json:"stop_price"`
	HardStopPrice float64 `json:"hard_stop_price"`
	ClosePrice    float64 `json:"close_price"`
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
		buyThreshold:         0.15,
		sellThreshold:        -0.50,
		trendFilterEnabled:   true,
		trendPeriod:          50,
		htfEnabled:           false,
		htfInterval:          "4h",
		htfPeriod:            20,
		confirmBars:          1,
		cooldownBars:         12,
		minHoldBars:          18,
		atrStopMult:          4.0,
		atrPeriod:            14,
		hardStopPct:          0,
		atrActivateProfitPct: 0,
		emaCrossMin:          0.15,
		// Short defaults
		shortEnabled:      false,
		shortThreshold:    -0.35, // stricter default; replaces need for shortMinScoreAbs filter
		coverThreshold:    0.15,
		shortConfirmBars:  2,
		shortMinHoldBars:  8,
		shortATRStopMult:  2.5,
		shortCooldownBars: 6,
		// Short optimization defaults
		shortATRStopActivatePct: 3.0, // activate trailing stop only after 3% profit
		shortMinScoreAbs:        0,   // 0 = no filter
		shortATRVolatilityMin:   0,   // 0 = no filter
		// ADX defaults
		shortADXEnabled:  false,
		shortADXPeriod:   14,
		shortADXMin:      20,
		shortADXDIFilter: true,

		states:           make(map[string]*symbolState),
		lastDiagBySymbol: make(map[string]*Diagnostics),
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

	// Reset all per-symbol trading state (module instances get rebuilt lazily
	// from the new moduleSpecs on next Evaluate/OnTradeExecuted call).
	s.states = make(map[string]*symbolState)
	s.templateModules = nil // Clear modules before re-init
	s.moduleSpecs = nil

	return s.init(cfg)
}

// GetConfig returns the current running configuration.
func (s *CustomWeightedStrategy) GetConfig() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	mods := make([]map[string]interface{}, 0, len(s.templateModules))
	for _, wm := range s.templateModules {
		mods = append(mods, map[string]interface{}{
			"name":   wm.module.Name(),
			"weight": wm.weight,
		})
	}

	return map[string]interface{}{
		"modules":                 mods,
		"buy_threshold":           s.buyThreshold,
		"sell_threshold":          s.sellThreshold,
		"confirm_bars":            s.confirmBars,
		"cooldown_bars":           s.cooldownBars,
		"min_hold_bars":           s.minHoldBars,
		"atr_stop_mult":           s.atrStopMult,
		"atr_period":              s.atrPeriod,
		"hard_stop_pct":           s.hardStopPct,
		"atr_activate_profit_pct": s.atrActivateProfitPct,
		"trend_filter":            s.trendFilterEnabled,
		"trend_period":            s.trendPeriod,
		"htf_enabled":             s.htfEnabled,
		"htf_interval":            s.htfInterval,
		"htf_period":              s.htfPeriod,
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

// GetDiagnostics returns the latest evaluation diagnostics for the given symbol. Thread-safe.
func (s *CustomWeightedStrategy) GetDiagnostics(symbol string) *Diagnostics {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastDiagBySymbol[symbol]
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
	if v, ok := cfg["hard_stop_pct"]; ok {
		s.hardStopPct = toFloat(v)
	}
	if v, ok := cfg["atr_activate_profit_pct"]; ok {
		s.atrActivateProfitPct = toFloat(v)
	}
	if v, ok := cfg["min_hold_bars"]; ok {
		s.minHoldBars = toInt(v)
	}
	if v, ok := cfg["ema_cross_min"]; ok {
		s.emaCrossMin = toFloat(v)
	}
	if v, ok := cfg["interval"]; ok {
		if s2, ok := v.(string); ok && s2 != "" {
			s.klineInterval = s2
		}
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
	if v, ok := cfg["short_atr_stop_activate_pct"]; ok {
		s.shortATRStopActivatePct = toFloat(v)
	}
	if v, ok := cfg["short_min_score_abs"]; ok {
		s.shortMinScoreAbs = toFloat(v)
	}
	if v, ok := cfg["short_atr_volatility_min"]; ok {
		s.shortATRVolatilityMin = toFloat(v)
	}
	if v, ok := cfg["short_adx_enabled"]; ok {
		s.shortADXEnabled = toBool(v)
	}
	if v, ok := cfg["short_adx_period"]; ok {
		s.shortADXPeriod = toInt(v)
	}
	if v, ok := cfg["short_adx_min"]; ok {
		s.shortADXMin = toFloat(v)
	}
	if v, ok := cfg["short_adx_di_filter"]; ok {
		s.shortADXDIFilter = toBool(v)
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

		s.templateModules = append(s.templateModules, weightedMod{module: mod, weight: weight})
		s.moduleSpecs = append(s.moduleSpecs, moduleSpec{name: name, weight: weight, params: params})
	}

	if len(s.templateModules) == 0 {
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
		s.templateModules = append(s.templateModules, weightedMod{module: mod, weight: d.weight})
		s.moduleSpecs = append(s.moduleSpecs, moduleSpec{name: d.name, weight: d.weight, params: nil})
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

	// Need ADX for short trend strength filter
	if s.shortADXEnabled && s.shortADXPeriod > 0 {
		adxKey := fmt.Sprintf("ADX_%d", s.shortADXPeriod)
		seen[adxKey] = struct{}{}
		result = append(result, strategy.IndicatorRequirement{
			Name: "ADX", Params: map[string]int{"period": s.shortADXPeriod},
		})
	}

	for _, wm := range s.templateModules {
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

// getOrCreateState returns the per-symbol runtime state, lazily creating a
// fresh set of module instances (independent internal history) on first use.
// Caller must hold s.mu (write lock).
func (s *CustomWeightedStrategy) getOrCreateState(symbol string) *symbolState {
	if st, ok := s.states[symbol]; ok {
		return st
	}
	st := &symbolState{}
	for _, spec := range s.moduleSpecs {
		mod, ok := modules.Create(spec.name, spec.params)
		if !ok {
			continue
		}
		st.weightedModules = append(st.weightedModules, weightedMod{module: mod, weight: spec.weight})
	}
	s.states[symbol] = st
	return st
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
	for _, wm := range s.templateModules {
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
	// Full lock, not RLock: this mutates per-symbol state (confirm/cooldown
	// counters, entry price, high-water mark) and may lazily create a new
	// symbolState entry in the map.
	s.mu.Lock()
	defer s.mu.Unlock()

	st := s.getOrCreateState(snap.Symbol)

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
	for _, wm := range st.weightedModules {
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
	klineHigh := 0.0
	if len(snap.Klines) > 0 {
		lastK := snap.Klines[len(snap.Klines)-1]
		closePrice = lastK.Close
		klineHigh = lastK.High
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
	htfDataOK := false // true only when we actually computed a real HTF distance, vs. missing/warming-up data
	if s.htfEnabled && len(snap.HTFKlines) > 0 {
		htfEMA := snap.HTFIndicators.EMA[s.htfPeriod]
		htfPrice := snap.HTFKlines[len(snap.HTFKlines)-1].Close
		if htfEMA > 0 && htfPrice > 0 {
			htfBullish = htfPrice > htfEMA
			htfDist = (htfPrice - htfEMA) / htfEMA * 100
			htfDataOK = true
			sig.Indicators["htf_ema_dist_pct"] = htfDist
			sig.Indicators["htf_bullish"] = boolToFloat(htfBullish)
		}
	}

	// holdReason tracks why no buy/sell was triggered (for diagnostics)
	holdReason := ""
	stopPrice := 0.0
	hardStopPrice := 0.0

	// --- BUY LOGIC ---
	if !hasPosition {
		if st.cooldownCount > 0 {
			holdReason = fmt.Sprintf("冷却期中，剩余 %d 根K线", st.cooldownCount)
			st.cooldownCount--
		} else if !trendBullish {
			holdReason = fmt.Sprintf("趋势过滤：价格低于EMA(%d)，距离 %.2f%%", s.trendPeriod, trendDist)
			st.confirmCount = 0
		} else if !htfBullish {
			holdReason = fmt.Sprintf("大周期过滤：HTF价格低于EMA(%d)，距离 %.2f%%", s.htfPeriod, htfDist)
			st.confirmCount = 0
			htfBlocked = true
			sig.Indicators["htf_blocked"] = 1.0
		} else if s.emaCrossMin > 0 && moduleScores["ema_cross"] < s.emaCrossMin {
			holdReason = fmt.Sprintf("EMA cross 分 %.3f 未达最低门槛 %.2f", moduleScores["ema_cross"], s.emaCrossMin)
			st.confirmCount = 0
		} else if composite < s.effectiveBuyThreshold(htfDataOK, htfDist, moduleScores["ema_cross"]) {
			effective := s.effectiveBuyThreshold(htfDataOK, htfDist, moduleScores["ema_cross"])
			holdReason = fmt.Sprintf("综合评分 %.3f 未达动态买入阈值 %.2f（HTF距离 %.1f%%）", composite, effective, htfDist)
			st.confirmCount = 0
		} else {
			st.confirmCount++
			if st.confirmCount >= s.confirmBars {
				effective := s.effectiveBuyThreshold(htfDataOK, htfDist, moduleScores["ema_cross"])
				htfLabel := "看空"
				if htfBullish {
					htfLabel = "看多"
				}
				// 历史数据显示 ema_cross 分偏弱或 htf 距离贴近边缘区(0~0.8%)时，
				// 买入后更容易被止损，即使综合分和其它模块分都不低。仅作为人工
				// 复核提示，不影响买入决策本身。
				caution := ""
				if moduleScores["ema_cross"] < 0.25 || (s.htfEnabled && htfDist < 0.8) {
					caution = "，⚠️入场质量偏弱，建议人工复核"
				}
				sig.Action = strategy.Buy
				sig.Strength = clamp(composite, 0.1, 1.0)
				sig.Reason = fmt.Sprintf(
					"Custom buy (score=%.2f, threshold=%.2f, htf=%s(%.1f%%)): %s%s",
					composite, effective, htfLabel, htfDist, strings.Join(scoreParts, ", "), caution,
				)
				st.confirmCount = 0
			} else {
				holdReason = fmt.Sprintf("确认中 %d/%d 根K线（评分 %.3f 已达阈值 %.2f）", st.confirmCount, s.confirmBars, composite, s.buyThreshold)
			}
		}
	}

	// --- SELL LOGIC ---
	if hasPosition {
		// 恢复 entryPrice：程序重启后策略内部状态丢失,
		// 但 snap.Position.AvgEntryPrice 从仓位管理器恢复了。
		if st.entryPrice <= 0 && snap.Position.AvgEntryPrice > 0 {
			st.entryPrice = snap.Position.AvgEntryPrice
		}
		// 如果 highWaterMark 也未初始化，用 entryPrice 兜底
		if st.highWaterMark <= 0 && st.entryPrice > 0 {
			st.highWaterMark = st.entryPrice
		}

		st.barsSinceEntry++

		// 用K线最高价追踪(而非收盘价),确保捕捉到盘中最高点
		if klineHigh > st.highWaterMark {
			st.highWaterMark = klineHigh
		}

		// 硬止损价（ATR 激活前的兜底，0=禁用）
		if s.hardStopPct > 0 && st.entryPrice > 0 {
			hardStopPrice = st.entryPrice * (1 - s.hardStopPct/100)
		}

		// 当前浮盈百分比，决定是否激活 ATR trailing
		currentProfitPct := 0.0
		if st.entryPrice > 0 && closePrice > 0 {
			currentProfitPct = (closePrice - st.entryPrice) / st.entryPrice * 100
		}
		atrTrailingActive := s.atrActivateProfitPct == 0 || currentProfitPct >= s.atrActivateProfitPct

		// 始终计算 ATR 止损价（用于前端展示），但仅在激活时触发出场
		if atr > 0 && st.highWaterMark > 0 {
			mult := s.atrStopMult
			if st.entryPrice > 0 {
				profitPct := (st.highWaterMark - st.entryPrice) / st.entryPrice * 100
				if profitPct > 30 {
					mult *= 0.65
				} else if profitPct > 15 {
					mult *= 0.8
				}
			}
			stopPrice = st.highWaterMark - atr*mult
		}

		if s.minHoldBars > 0 && st.barsSinceEntry < s.minHoldBars {
			effectiveStop := stopPrice
			if !atrTrailingActive && hardStopPrice > 0 {
				effectiveStop = hardStopPrice
			}
			holdReason = fmt.Sprintf("最短持仓期中 %d/%d 根K线（止损价=%.2f）", st.barsSinceEntry, s.minHoldBars, effectiveStop)
		} else {
			// Sell condition 1: 硬止损（持仓锁结束后的绝对兜底）
			if hardStopPrice > 0 && closePrice <= hardStopPrice {
				sig.Action = strategy.Sell
				sig.Strength = 1.0
				sig.Reason = fmt.Sprintf(
					"Hard stop: price=%.2f below entry*%.1f%%=%.2f (entry=%.2f)",
					closePrice, s.hardStopPct, hardStopPrice, st.entryPrice,
				)
			}

			// Sell condition 2: ATR trailing stop（仅在激活状态下触发）
			if sig.Action != strategy.Sell && atrTrailingActive && stopPrice > 0 && closePrice <= stopPrice {
				sig.Action = strategy.Sell
				sig.Strength = 0.8
				sig.Reason = fmt.Sprintf(
					"ATR trailing stop: price=%.2f below stop=%.2f (high=%.2f, ATR=%.2f×%.1f)",
					closePrice, stopPrice, st.highWaterMark, atr, s.atrStopMult,
				)
			}

			// Sell condition 3: Composite score deeply negative
			if sig.Action != strategy.Sell && s.sellThreshold > -1.0 && composite <= s.sellThreshold {
				sig.Action = strategy.Sell
				sig.Strength = clamp(-composite, 0.1, 1.0)
				sig.Reason = fmt.Sprintf(
					"Custom sell (score=%.2f, threshold=%.2f): %s",
					composite, s.sellThreshold, strings.Join(scoreParts, ", "),
				)
			}

			if sig.Action == strategy.Hold {
				activeMark := ""
				if s.atrActivateProfitPct > 0 {
					if atrTrailingActive {
						activeMark = fmt.Sprintf("，ATR trailing 已激活（浮盈=%.2f%%）", currentProfitPct)
					} else {
						activeMark = fmt.Sprintf("，等待浮盈 %.1f%% 激活ATR（当前=%.2f%%）", s.atrActivateProfitPct, currentProfitPct)
					}
				}
				effectiveStop := stopPrice
				if !atrTrailingActive && hardStopPrice > 0 {
					effectiveStop = hardStopPrice
				}
				holdReason = fmt.Sprintf("持仓中 %d 根K线，止损价=%.2f（当前=%.2f），评分=%.3f 未触发卖出阈值 %.2f%s",
					st.barsSinceEntry, effectiveStop, closePrice, composite, s.sellThreshold, activeMark)
			}
		}
	}

	// --- SHORT LOGIC (independent of long, evaluated when long action is Hold) ---
	if s.shortEnabled && sig.Action == strategy.Hold {
		sig = s.evaluateShort(st, sig, composite, scoreParts, closePrice, atr, trendBullish, htfBullish, snap.Indicators.ADX)
	}

	// --- Save diagnostics ---
	s.lastDiagBySymbol[snap.Symbol] = &Diagnostics{
		Timestamp:      snap.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
		Symbol:         snap.Symbol,
		Action:         sig.Action.String(),
		CompositeScore: composite,
		ModuleScores:   moduleScores,
		ModuleWeights:  moduleWeights,
		BuyThreshold:   s.buyThreshold,
		SellThreshold:  s.sellThreshold,
		HasPosition:    hasPosition,
		EntryPrice:     st.entryPrice,
		HighWaterMark:  st.highWaterMark,
		BarsSinceEntry: st.barsSinceEntry,
		ConfirmCount:   st.confirmCount,
		ConfirmBars:    s.confirmBars,
		CooldownCount:  st.cooldownCount,
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
		HardStopPrice:  hardStopPrice,
		ClosePrice:     closePrice,
		HoldReason:     holdReason,
		Reason:         sig.Reason,
	}

	return sig, nil
}

// evaluateShort handles virtual short position management and signal generation.
// Short entry: composite deeply negative + bearish trend confirmation.
// Short exit: composite turns positive OR ATR trailing stop (inverted).
func (s *CustomWeightedStrategy) evaluateShort(st *symbolState, sig *strategy.Signal, composite float64, scoreParts []string, closePrice, atr float64, trendBullish, htfBullish bool, adx strategy.ADXValue) *strategy.Signal {
	if st.inShortPosition {
		st.shortBarsSinceEntry++

		// Track low water mark (lowest price since entry)
		if closePrice < st.shortLowWaterMark || st.shortLowWaterMark == 0 {
			st.shortLowWaterMark = closePrice
		}

		// Min hold period
		if s.shortMinHoldBars > 0 && st.shortBarsSinceEntry < s.shortMinHoldBars {
			return sig
		}

		// Cover condition 1: ATR trailing stop (inverted — price rises above low + ATR*mult)
		// Optimization: only activate ATR stop after position has reached minimum profit
		atrStopActive := true
		if s.shortATRStopActivatePct > 0 && st.shortEntryPrice > 0 {
			currentProfitPct := (st.shortEntryPrice - st.shortLowWaterMark) / st.shortEntryPrice * 100
			atrStopActive = currentProfitPct >= s.shortATRStopActivatePct
		}

		if atrStopActive && atr > 0 && st.shortLowWaterMark > 0 && s.shortATRStopMult > 0 {
			mult := s.shortATRStopMult

			// Profit-based tightening for shorts
			if st.shortEntryPrice > 0 {
				profitPct := (st.shortEntryPrice - st.shortLowWaterMark) / st.shortEntryPrice * 100
				if profitPct > 30 {
					mult *= 0.65
				} else if profitPct > 15 {
					mult *= 0.8
				}
			}

			stopPrice := st.shortLowWaterMark + atr*mult
			if closePrice >= stopPrice {
				sig.Action = strategy.Cover
				sig.Strength = 0.8
				sig.Reason = fmt.Sprintf(
					"Short ATR trailing stop: price=%.2f above stop=%.2f (low=%.2f, ATR=%.2f×%.1f)",
					closePrice, stopPrice, st.shortLowWaterMark, atr, mult,
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
		if st.shortCooldownCount > 0 {
			st.shortCooldownCount--
			return sig
		}

		// Trend filter: only short when trend is BEARISH (price below EMA)
		if s.trendFilterEnabled && trendBullish {
			st.shortConfirmCount = 0
			return sig
		}

		// HTF filter: only short when higher TF is BEARISH
		if s.htfEnabled && htfBullish {
			st.shortConfirmCount = 0
			return sig
		}

		// ADX trend strength filter: only short when there's a confirmed downtrend
		if s.shortADXEnabled && adx.Period > 0 {
			// ADX too low = no trend (sideways market) → don't short
			if adx.ADX < s.shortADXMin {
				st.shortConfirmCount = 0
				return sig
			}
			// DI filter: only short when -DI > +DI (bearish directional movement)
			if s.shortADXDIFilter && adx.MinusDI <= adx.PlusDI {
				st.shortConfirmCount = 0
				return sig
			}
		}

		// Volatility filter: ATR/price must exceed minimum (skip low-volatility/ranging markets)
		if s.shortATRVolatilityMin > 0 && closePrice > 0 && atr > 0 {
			atrPct := atr / closePrice * 100
			if atrPct < s.shortATRVolatilityMin {
				st.shortConfirmCount = 0
				return sig
			}
		}

		// Score must be below short threshold
		if composite <= s.shortThreshold {
			// Signal strength filter: |composite| must be large enough
			if s.shortMinScoreAbs > 0 && (-composite) < s.shortMinScoreAbs {
				// Score crossed threshold but too weak — don't count
				return sig
			}
			st.shortConfirmCount++
		} else {
			st.shortConfirmCount = 0
		}

		if st.shortConfirmCount >= s.shortConfirmBars {
			sig.Action = strategy.Short
			sig.Strength = clamp(-composite, 0.1, 1.0)
			sig.Reason = fmt.Sprintf(
				"Short entry (score=%.2f, threshold=%.2f): %s",
				composite, s.shortThreshold, strings.Join(scoreParts, ", "),
			)
			st.shortConfirmCount = 0
		}
	}

	return sig
}

// OnShortSignalProcessed updates virtual short position state for the given
// symbol. Called by both backtest engine and live trader when a short signal
// is processed.
func (s *CustomWeightedStrategy) OnShortSignalProcessed(symbol string, action strategy.Action, price float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.getOrCreateState(symbol)

	switch action {
	case strategy.Short:
		st.inShortPosition = true
		st.shortEntryPrice = price
		st.shortLowWaterMark = price
		st.shortBarsSinceEntry = 0
		st.shortConfirmCount = 0
	case strategy.Cover:
		st.inShortPosition = false
		st.shortEntryPrice = 0
		st.shortLowWaterMark = 0
		st.shortBarsSinceEntry = 0
		st.shortCooldownCount = s.shortCooldownBars
	}
}

func (s *CustomWeightedStrategy) OnTradeExecuted(trade *exchange.Trade) {
	// Lock (write) — runs on the order manager goroutine, racing with Evaluate
	// which holds the same mutex.
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.getOrCreateState(trade.Symbol)

	if trade.Side == exchange.OrderSideBuy {
		st.entryPrice = trade.Price
		st.highWaterMark = trade.Price
		st.confirmCount = 0
		// 如果 trade.Timestamp 不是当前时间(重启恢复场景),根据时间差估算已过的 bars 数。
		// 正常下单时 Timestamp ≈ now, elapsed ≈ 0, barsSinceEntry = 0。
		// 重启恢复时 Timestamp = 真实入场时间, elapsed = 已过时长, 恢复正确的 bars。
		elapsed := time.Since(trade.Timestamp)
		if elapsed > 2*time.Minute && s.barDuration() > 0 {
			st.barsSinceEntry = int(elapsed / s.barDuration())
		} else {
			st.barsSinceEntry = 0
		}
	} else {
		st.entryPrice = 0
		st.highWaterMark = 0
		st.barsSinceEntry = 0
		st.cooldownCount = s.cooldownBars // start cooldown
	}
}

// SetHighWaterMark overrides the tracked highest price since entry for the
// given symbol. Used on startup to recover the real high from DB klines,
// since OnTradeExecuted only sets highWaterMark = entryPrice.
func (s *CustomWeightedStrategy) SetHighWaterMark(symbol string, price float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.getOrCreateState(symbol)
	if price > st.highWaterMark {
		st.highWaterMark = price
	}
}

// barDuration returns the duration of one kline bar based on klineInterval.
func (s *CustomWeightedStrategy) barDuration() time.Duration {
	switch s.klineInterval {
	case "1m":
		return time.Minute
	case "3m":
		return 3 * time.Minute
	case "5m":
		return 5 * time.Minute
	case "15m":
		return 15 * time.Minute
	case "30m":
		return 30 * time.Minute
	case "1h":
		return time.Hour
	case "4h":
		return 4 * time.Hour
	case "1d":
		return 24 * time.Hour
	default:
		return 0
	}
}

// boolToFloat converts bool to float64 for indicator logging.
func boolToFloat(b bool) float64 {
	if b {
		return 1.0
	}
	return 0.0
}

// effectiveBuyThreshold 根据日线EMA10距离和EMA cross分动态调整买入阈值。
// 距离越小或越大（偏热）→ 要求更强的信号。HTF未启用或数据缺失（htfDataOK=false）时退回基础值——
// 注意数据缺失不等于"距离为0"，避免把真实的边缘区(0~0.8%)误判成无过滤。
func (s *CustomWeightedStrategy) effectiveBuyThreshold(htfDataOK bool, htfDist, emaCrossScore float64) float64 {
	base := s.buyThreshold
	if !s.htfEnabled || !htfDataOK {
		return base
	}
	switch {
	case htfDist > 7.0:
		// 超买：极严格
		return base * 1.8
	case htfDist > 3.0:
		// 偏热：EMA cross 强则维持原值，弱则收紧
		if emaCrossScore >= base*1.75 {
			return base
		}
		return base * 1.5
	case htfDist >= 0.8:
		// 最佳区(0.8%+)：原值不变
		return base
	default:
		// 边缘区(0~0.8%)：刚刚站上EMA，轻微收紧
		return base * 1.5
	}
}

var _ strategy.Strategy = (*CustomWeightedStrategy)(nil)
