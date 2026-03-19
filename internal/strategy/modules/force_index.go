package modules

import "github.com/jayce/btc-trader/internal/strategy"

// ForceIndexModule scores based on the EMA-smoothed Force Index.
// Force Index = price change × volume. Positive = buying force, negative = selling force.
type ForceIndexModule struct {
	period int
}

func (m *ForceIndexModule) Name() string        { return "force_index" }
func (m *ForceIndexModule) Category() string     { return "volume" }
func (m *ForceIndexModule) Label() string        { return "力度指标" }
func (m *ForceIndexModule) DefaultWeight() float64 { return 0.10 }

func (m *ForceIndexModule) Description() string {
	return "价格变化×成交量，正值表示买入力度，负值表示卖出力度，EMA平滑"
}

func (m *ForceIndexModule) ParamSchema() []ParamSchema {
	return []ParamSchema{
		{Key: "period", Label: "EMA周期", Type: "int", Default: 13, Min: 2, Max: 30, Step: 1},
	}
}

func (m *ForceIndexModule) Init(params map[string]interface{}) {
	m.period = 13
	if v, ok := params["period"]; ok {
		m.period = toInt(v)
	}
}

func (m *ForceIndexModule) RequiredIndicators() []strategy.IndicatorRequirement {
	return []strategy.IndicatorRequirement{
		{Name: "ForceIndex", Params: map[string]int{"period": m.period}},
	}
}

func (m *ForceIndexModule) RequiredHistory() int {
	return m.period + 10
}

func (m *ForceIndexModule) Score(snap *strategy.MarketSnapshot) float64 {
	fi := snap.Indicators.ForceIndex[m.period]
	if len(snap.Klines) < 2 {
		return 0
	}
	closePrice := snap.Klines[len(snap.Klines)-1].Close
	if closePrice == 0 {
		return 0
	}

	// Compute average volume from recent klines for scale-invariant normalization
	totalVol := 0.0
	n := len(snap.Klines)
	count := 20
	if count > n {
		count = n
	}
	for i := n - count; i < n; i++ {
		totalVol += snap.Klines[i].Volume
	}
	avgVol := totalVol / float64(count)
	if avgVol == 0 {
		return 0
	}

	// Normalize by (price * avgVolume * typical_pct_move)
	// A 1% move at average volume should be a moderate signal
	normalizer := closePrice * 0.01 * avgVol
	if normalizer == 0 {
		return 0
	}
	normalized := fi / normalizer
	return tanhScore(normalized, 0, 1.0)
}
