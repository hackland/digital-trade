package modules

import "github.com/jayce/btc-trader/internal/strategy"

// BBPositionModule scores based on price position within Bollinger Bands
// and band squeeze detection.
type BBPositionModule struct {
	period      int
	mult        int
	prevWidth   float64
	initialized bool
}

func (m *BBPositionModule) Name() string        { return "bb_position" }
func (m *BBPositionModule) Category() string     { return "momentum" }
func (m *BBPositionModule) Label() string        { return "布林带位置" }
func (m *BBPositionModule) DefaultWeight() float64 { return 0.10 }

func (m *BBPositionModule) Description() string {
	return "价格在布林带中的相对位置，触及下轨看涨、上轨轻度看跌，带宽变化确认趋势"
}

func (m *BBPositionModule) ParamSchema() []ParamSchema {
	return []ParamSchema{
		{Key: "period", Label: "周期", Type: "int", Default: 20, Min: 10, Max: 50, Step: 1},
		{Key: "mult", Label: "倍数", Type: "int", Default: 2, Min: 1, Max: 3, Step: 1},
	}
}

func (m *BBPositionModule) Init(params map[string]interface{}) {
	m.period = 20
	m.mult = 2
	if v, ok := params["period"]; ok {
		m.period = toInt(v)
	}
	if v, ok := params["mult"]; ok {
		m.mult = toInt(v)
	}
}

func (m *BBPositionModule) RequiredIndicators() []strategy.IndicatorRequirement {
	return []strategy.IndicatorRequirement{
		{Name: "BB", Params: map[string]int{"period": m.period, "mult": m.mult}},
	}
}

func (m *BBPositionModule) RequiredHistory() int {
	return m.period + 5
}

func (m *BBPositionModule) Score(snap *strategy.MarketSnapshot) float64 {
	bb := snap.Indicators.BB
	if bb.Upper == bb.Lower || len(snap.Klines) == 0 {
		return 0
	}

	closePrice := snap.Klines[len(snap.Klines)-1].Close

	// Position within bands: 0 = lower band, 1 = upper band
	bandRange := bb.Upper - bb.Lower
	position := (closePrice - bb.Lower) / bandRange // 0..1

	// Only contrarian at band extremes (outside bands or very close to edges)
	positionScore := 0.0
	if position < 0.05 {
		positionScore = 0.4 // touching lower band = buy
	} else if position > 0.95 {
		positionScore = -0.2 // touching upper band = mild sell (trends ride upper band)
	} else {
		// In normal range: trend-following -- above middle is bullish
		positionScore = tanhScore(position, 0.5, 0.4) * 0.3
	}

	// Band width change: expanding bands confirm a move
	widthScore := 0.0
	if m.initialized && m.prevWidth > 0 {
		widthChange := (bb.Width - m.prevWidth) / m.prevWidth
		if widthChange < -0.1 {
			widthScore = 0.1 // squeeze = potential breakout
		} else if widthChange > 0.1 {
			// Bands expanding with price above middle = trend confirmation
			if position > 0.5 {
				widthScore = 0.2
			} else {
				widthScore = -0.2
			}
		}
	}

	m.prevWidth = bb.Width
	m.initialized = true

	return clamp(positionScore+widthScore, -1.0, 1.0)
}
