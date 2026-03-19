package modules

import "github.com/jayce/btc-trader/internal/strategy"

// VROCModule scores based on Volume Rate of Change — volume momentum,
// weighted by price direction.
type VROCModule struct {
	period int
}

func (m *VROCModule) Name() string        { return "vroc" }
func (m *VROCModule) Category() string     { return "volume" }
func (m *VROCModule) Label() string        { return "VROC 量变速率" }
func (m *VROCModule) DefaultWeight() float64 { return 0.10 }

func (m *VROCModule) Description() string {
	return "成交量变化率×价格方向，放量上涨为正，放量下跌为负，缩量趋势减弱"
}

func (m *VROCModule) ParamSchema() []ParamSchema {
	return []ParamSchema{
		{Key: "period", Label: "周期", Type: "int", Default: 10, Min: 3, Max: 30, Step: 1},
	}
}

func (m *VROCModule) Init(params map[string]interface{}) {
	m.period = 10
	if v, ok := params["period"]; ok {
		m.period = toInt(v)
	}
}

func (m *VROCModule) RequiredIndicators() []strategy.IndicatorRequirement {
	return []strategy.IndicatorRequirement{
		{Name: "VROC", Params: map[string]int{"period": m.period}},
	}
}

func (m *VROCModule) RequiredHistory() int {
	return m.period + 5
}

func (m *VROCModule) Score(snap *strategy.MarketSnapshot) float64 {
	vroc := snap.Indicators.VROC[m.period]
	if len(snap.Klines) < 2 {
		return 0
	}

	// Determine price direction
	klines := snap.Klines
	currClose := klines[len(klines)-1].Close
	prevClose := klines[len(klines)-2].Close
	direction := 1.0
	if prevClose > 0 && currClose < prevClose {
		direction = -1.0
	}

	// VROC confirms direction: expanding volume in the direction of the move
	return tanhScore(vroc, 0, 50) * direction
}
