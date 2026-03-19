package modules

import "github.com/jayce/btc-trader/internal/strategy"

// RSIModule scores based on RSI overbought/oversold levels and momentum.
type RSIModule struct {
	period      int
	prevRSI     float64
	initialized bool
}

func (m *RSIModule) Name() string        { return "rsi" }
func (m *RSIModule) Category() string     { return "momentum" }
func (m *RSIModule) Label() string        { return "RSI 相对强弱" }
func (m *RSIModule) DefaultWeight() float64 { return 0.15 }

func (m *RSIModule) Description() string {
	return "衡量价格超买超卖状态，<30看涨，>70看跌，结合动量方向"
}

func (m *RSIModule) ParamSchema() []ParamSchema {
	return []ParamSchema{
		{Key: "period", Label: "周期", Type: "int", Default: 14, Min: 5, Max: 50, Step: 1},
	}
}

func (m *RSIModule) Init(params map[string]interface{}) {
	m.period = 14
	if v, ok := params["period"]; ok {
		m.period = toInt(v)
	}
}

func (m *RSIModule) RequiredIndicators() []strategy.IndicatorRequirement {
	return []strategy.IndicatorRequirement{
		{Name: "RSI", Params: map[string]int{"period": m.period}},
	}
}

func (m *RSIModule) RequiredHistory() int {
	return m.period + 10
}

func (m *RSIModule) Score(snap *strategy.MarketSnapshot) float64 {
	rsi := snap.Indicators.RSI[m.period]

	// Only apply contrarian signal at true extremes (< 25 or > 75)
	// In the normal range (25-75), let momentum drive the score
	positionScore := 0.0
	if rsi < 25 {
		positionScore = tanhScore(25-rsi, 0, 10) * 0.4 // oversold = buy
	} else if rsi > 75 {
		positionScore = -tanhScore(rsi-75, 0, 10) * 0.4 // overbought = sell
	}

	// RSI momentum: rising RSI = bullish, the primary signal
	momentumScore := 0.0
	if m.initialized {
		rsiChange := rsi - m.prevRSI
		momentumScore = tanhScore(rsiChange, 0, 5) * 0.6
	}

	m.prevRSI = rsi
	m.initialized = true

	return clamp(positionScore+momentumScore, -1.0, 1.0)
}
