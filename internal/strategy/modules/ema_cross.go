package modules

import "github.com/jayce/btc-trader/internal/strategy"

// EMACrossModule scores based on EMA golden/death cross and alignment.
type EMACrossModule struct {
	fastPeriod  int
	slowPeriod  int
	prevFastEMA float64
	prevSlowEMA float64
	initialized bool
}

func (m *EMACrossModule) Name() string        { return "ema_cross" }
func (m *EMACrossModule) Category() string     { return "trend" }
func (m *EMACrossModule) Label() string        { return "EMA 金叉死叉" }
func (m *EMACrossModule) DefaultWeight() float64 { return 0.20 }

func (m *EMACrossModule) Description() string {
	return "快慢EMA交叉信号，金叉看涨死叉看跌，结合均线排列和价格位置"
}

func (m *EMACrossModule) ParamSchema() []ParamSchema {
	return []ParamSchema{
		{Key: "fast_period", Label: "快线周期", Type: "int", Default: 9, Min: 3, Max: 30, Step: 1},
		{Key: "slow_period", Label: "慢线周期", Type: "int", Default: 21, Min: 10, Max: 60, Step: 1},
	}
}

func (m *EMACrossModule) Init(params map[string]interface{}) {
	m.fastPeriod = 9
	m.slowPeriod = 21
	if v, ok := params["fast_period"]; ok {
		m.fastPeriod = toInt(v)
	}
	if v, ok := params["slow_period"]; ok {
		m.slowPeriod = toInt(v)
	}
}

func (m *EMACrossModule) RequiredIndicators() []strategy.IndicatorRequirement {
	return []strategy.IndicatorRequirement{
		{Name: "EMA", Params: map[string]int{"period": m.fastPeriod}},
		{Name: "EMA", Params: map[string]int{"period": m.slowPeriod}},
	}
}

func (m *EMACrossModule) RequiredHistory() int {
	return m.slowPeriod + 10
}

func (m *EMACrossModule) Score(snap *strategy.MarketSnapshot) float64 {
	fastEMA := snap.Indicators.EMA[m.fastPeriod]
	slowEMA := snap.Indicators.EMA[m.slowPeriod]

	if !m.initialized {
		m.prevFastEMA = fastEMA
		m.prevSlowEMA = slowEMA
		m.initialized = true
		return 0
	}

	if slowEMA == 0 {
		return 0
	}

	score := 0.0

	// EMA alignment: fast > slow = bullish
	emaDiff := (fastEMA - slowEMA) / slowEMA * 100
	score += tanhScore(emaDiff, 0, 0.8) * 0.5

	// Price vs fast EMA
	if fastEMA > 0 && len(snap.Klines) > 0 {
		closePrice := snap.Klines[len(snap.Klines)-1].Close
		priceVsEMA := (closePrice - fastEMA) / fastEMA * 100
		score += tanhScore(priceVsEMA, 0, 1.0) * 0.3
	}

	// Crossover detection
	if m.prevFastEMA <= m.prevSlowEMA && fastEMA > slowEMA {
		score += 0.2
	} else if m.prevFastEMA >= m.prevSlowEMA && fastEMA < slowEMA {
		score -= 0.2
	}

	m.prevFastEMA = fastEMA
	m.prevSlowEMA = slowEMA

	return clamp(score, -1.0, 1.0)
}
