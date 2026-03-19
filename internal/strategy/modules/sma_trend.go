package modules

import "github.com/jayce/btc-trader/internal/strategy"

// SMATrendModule scores based on price position relative to SMA.
type SMATrendModule struct {
	period  int
	prevSMA float64
	initialized bool
}

func (m *SMATrendModule) Name() string        { return "sma_trend" }
func (m *SMATrendModule) Category() string     { return "trend" }
func (m *SMATrendModule) Label() string        { return "SMA 趋势" }
func (m *SMATrendModule) DefaultWeight() float64 { return 0.10 }

func (m *SMATrendModule) Description() string {
	return "价格在SMA上方看涨，下方看跌，结合SMA方向判断趋势"
}

func (m *SMATrendModule) ParamSchema() []ParamSchema {
	return []ParamSchema{
		{Key: "period", Label: "周期", Type: "int", Default: 50, Min: 10, Max: 200, Step: 5},
	}
}

func (m *SMATrendModule) Init(params map[string]interface{}) {
	m.period = 50
	if v, ok := params["period"]; ok {
		m.period = toInt(v)
	}
}

func (m *SMATrendModule) RequiredIndicators() []strategy.IndicatorRequirement {
	return []strategy.IndicatorRequirement{
		{Name: "SMA", Params: map[string]int{"period": m.period}},
	}
}

func (m *SMATrendModule) RequiredHistory() int {
	return m.period + 5
}

func (m *SMATrendModule) Score(snap *strategy.MarketSnapshot) float64 {
	sma := snap.Indicators.SMA[m.period]
	if sma == 0 || len(snap.Klines) == 0 {
		return 0
	}

	closePrice := snap.Klines[len(snap.Klines)-1].Close

	// Price vs SMA
	deviation := (closePrice - sma) / sma * 100
	score := tanhScore(deviation, 0, 1.5) * 0.6

	// SMA slope (direction)
	if m.initialized && m.prevSMA > 0 {
		smaChange := (sma - m.prevSMA) / m.prevSMA * 100
		score += tanhScore(smaChange, 0, 0.1) * 0.4
	}

	m.prevSMA = sma
	m.initialized = true

	return clamp(score, -1.0, 1.0)
}
