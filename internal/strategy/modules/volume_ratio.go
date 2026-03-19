package modules

import "github.com/jayce/btc-trader/internal/strategy"

// VolumeRatioModule scores based on current volume vs average volume,
// weighted by price direction for trend confirmation.
type VolumeRatioModule struct {
	period int
}

func (m *VolumeRatioModule) Name() string        { return "volume_ratio" }
func (m *VolumeRatioModule) Category() string     { return "volume" }
func (m *VolumeRatioModule) Label() string        { return "量比" }
func (m *VolumeRatioModule) DefaultWeight() float64 { return 0.10 }

func (m *VolumeRatioModule) Description() string {
	return "当前成交量与均量的比值，放量确认趋势方向，缩量表示趋势减弱"
}

func (m *VolumeRatioModule) ParamSchema() []ParamSchema {
	return []ParamSchema{
		{Key: "period", Label: "均量周期", Type: "int", Default: 20, Min: 5, Max: 50, Step: 1},
	}
}

func (m *VolumeRatioModule) Init(params map[string]interface{}) {
	m.period = 20
	if v, ok := params["period"]; ok {
		m.period = toInt(v)
	}
}

func (m *VolumeRatioModule) RequiredIndicators() []strategy.IndicatorRequirement {
	return []strategy.IndicatorRequirement{
		{Name: "VolumeSMA", Params: map[string]int{"period": m.period}},
	}
}

func (m *VolumeRatioModule) RequiredHistory() int {
	return m.period + 5
}

func (m *VolumeRatioModule) Score(snap *strategy.MarketSnapshot) float64 {
	avgVolume := snap.Indicators.VolumeSMA[m.period]
	if avgVolume <= 0 || len(snap.Klines) < 2 {
		return 0
	}

	klines := snap.Klines
	volume := klines[len(klines)-1].Volume
	ratio := volume / avgVolume

	// Determine price direction
	currClose := klines[len(klines)-1].Close
	prevClose := klines[len(klines)-2].Close
	direction := 1.0
	if prevClose > 0 && currClose < prevClose {
		direction = -1.0 // down candle
	}

	// Volume confirmation: high volume confirms the direction
	// Low volume = weak/unreliable move (score near 0)
	confirmation := tanhScore(ratio, 1.0, 0.7)

	return confirmation * direction
}
