package modules

import "github.com/jayce/btc-trader/internal/strategy"

// OBVTrendModule scores based on On-Balance Volume trend direction.
type OBVTrendModule struct {
	prevOBV     float64
	prevPrevOBV float64
	initialized int // count of bars seen
}

func (m *OBVTrendModule) Name() string        { return "obv_trend" }
func (m *OBVTrendModule) Category() string     { return "volume" }
func (m *OBVTrendModule) Label() string        { return "OBV 趋势" }
func (m *OBVTrendModule) DefaultWeight() float64 { return 0.10 }

func (m *OBVTrendModule) Description() string {
	return "累积成交量趋势，OBV上升表示买方主导，OBV下降表示卖方主导"
}

func (m *OBVTrendModule) ParamSchema() []ParamSchema {
	return []ParamSchema{}
}

func (m *OBVTrendModule) Init(params map[string]interface{}) {}

func (m *OBVTrendModule) RequiredIndicators() []strategy.IndicatorRequirement {
	return []strategy.IndicatorRequirement{
		{Name: "OBV"},
	}
}

func (m *OBVTrendModule) RequiredHistory() int {
	return 20
}

func (m *OBVTrendModule) Score(snap *strategy.MarketSnapshot) float64 {
	obv := snap.Indicators.OBV

	if m.initialized < 2 {
		m.prevPrevOBV = m.prevOBV
		m.prevOBV = obv
		m.initialized++
		return 0
	}

	// OBV momentum: direction and acceleration
	obvChange := obv - m.prevOBV
	obvAccel := obvChange - (m.prevOBV - m.prevPrevOBV)

	// Normalize by PREVIOUS absolute OBV level (before state update)
	denominator := abs(m.prevOBV) + 1
	relChange := obvChange / denominator
	relAccel := obvAccel / denominator

	// Now update state
	m.prevPrevOBV = m.prevOBV
	m.prevOBV = obv

	score := tanhScore(relChange, 0, 0.01)*0.6 + tanhScore(relAccel, 0, 0.005)*0.4

	return clamp(score, -1.0, 1.0)
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
