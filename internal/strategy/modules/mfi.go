package modules

import "github.com/jayce/btc-trader/internal/strategy"

// MFIModule scores based on Money Flow Index overbought/oversold and momentum.
type MFIModule struct {
	period      int
	prevMFI     float64
	initialized bool
}

func (m *MFIModule) Name() string        { return "mfi" }
func (m *MFIModule) Category() string     { return "money_flow" }
func (m *MFIModule) Label() string        { return "MFI 资金流量" }
func (m *MFIModule) DefaultWeight() float64 { return 0.15 }

func (m *MFIModule) Description() string {
	return "结合价格和成交量的资金流量指标，<20资金流入枯竭看涨，>80资金过热看跌"
}

func (m *MFIModule) ParamSchema() []ParamSchema {
	return []ParamSchema{
		{Key: "period", Label: "周期", Type: "int", Default: 14, Min: 5, Max: 30, Step: 1},
	}
}

func (m *MFIModule) Init(params map[string]interface{}) {
	m.period = 14
	if v, ok := params["period"]; ok {
		m.period = toInt(v)
	}
}

func (m *MFIModule) RequiredIndicators() []strategy.IndicatorRequirement {
	return []strategy.IndicatorRequirement{
		{Name: "MFI", Params: map[string]int{"period": m.period}},
	}
}

func (m *MFIModule) RequiredHistory() int {
	return m.period + 5
}

func (m *MFIModule) Score(snap *strategy.MarketSnapshot) float64 {
	mfi := snap.Indicators.MFI[m.period]

	// Only apply contrarian signal at true extremes
	positionScore := 0.0
	if mfi < 20 {
		positionScore = tanhScore(20-mfi, 0, 8) * 0.4 // exhaustion = buy
	} else if mfi > 80 {
		positionScore = -tanhScore(mfi-80, 0, 8) * 0.4 // overheated = sell
	}

	// MFI momentum: rising MFI = money flowing in = bullish
	momentumScore := 0.0
	if m.initialized {
		mfiChange := mfi - m.prevMFI
		momentumScore = tanhScore(mfiChange, 0, 7) * 0.6
	}

	m.prevMFI = mfi
	m.initialized = true

	return clamp(positionScore+momentumScore, -1.0, 1.0)
}
