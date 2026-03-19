package modules

import "github.com/jayce/btc-trader/internal/strategy"

// CMFModule scores based on Chaikin Money Flow buying/selling pressure.
type CMFModule struct {
	period int
}

func (m *CMFModule) Name() string        { return "cmf" }
func (m *CMFModule) Category() string     { return "money_flow" }
func (m *CMFModule) Label() string        { return "CMF 买卖压力" }
func (m *CMFModule) DefaultWeight() float64 { return 0.10 }

func (m *CMFModule) Description() string {
	return "柴金资金流指标，正值表示买方压力，负值表示卖方压力"
}

func (m *CMFModule) ParamSchema() []ParamSchema {
	return []ParamSchema{
		{Key: "period", Label: "周期", Type: "int", Default: 20, Min: 10, Max: 40, Step: 1},
	}
}

func (m *CMFModule) Init(params map[string]interface{}) {
	m.period = 20
	if v, ok := params["period"]; ok {
		m.period = toInt(v)
	}
}

func (m *CMFModule) RequiredIndicators() []strategy.IndicatorRequirement {
	return []strategy.IndicatorRequirement{
		{Name: "CMF", Params: map[string]int{"period": m.period}},
	}
}

func (m *CMFModule) RequiredHistory() int {
	return m.period + 5
}

func (m *CMFModule) Score(snap *strategy.MarketSnapshot) float64 {
	cmf := snap.Indicators.CMF[m.period]
	// CMF range is typically -0.3 to +0.3, amplify with tanh
	return tanhScore(cmf, 0, 0.15)
}
