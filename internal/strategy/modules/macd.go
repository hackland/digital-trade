package modules

import (
	"github.com/jayce/btc-trader/internal/strategy"
)

// MACDModule scores based on MACD histogram direction and signal line crossover.
type MACDModule struct {
	fast          int
	slow          int
	signal        int
	prevHistogram float64
	initialized   bool
}

func (m *MACDModule) Name() string        { return "macd" }
func (m *MACDModule) Category() string     { return "trend" }
func (m *MACDModule) Label() string        { return "MACD" }
func (m *MACDModule) DefaultWeight() float64 { return 0.20 }

func (m *MACDModule) Description() string {
	return "MACD柱状图方向+信号线交叉，趋势动量指标"
}

func (m *MACDModule) ParamSchema() []ParamSchema {
	return []ParamSchema{
		{Key: "fast", Label: "快线", Type: "int", Default: 12, Min: 5, Max: 20, Step: 1},
		{Key: "slow", Label: "慢线", Type: "int", Default: 26, Min: 15, Max: 40, Step: 1},
		{Key: "signal", Label: "信号线", Type: "int", Default: 9, Min: 5, Max: 15, Step: 1},
	}
}

func (m *MACDModule) Init(params map[string]interface{}) {
	m.fast = 12
	m.slow = 26
	m.signal = 9
	if v, ok := params["fast"]; ok {
		m.fast = toInt(v)
	}
	if v, ok := params["slow"]; ok {
		m.slow = toInt(v)
	}
	if v, ok := params["signal"]; ok {
		m.signal = toInt(v)
	}
}

func (m *MACDModule) RequiredIndicators() []strategy.IndicatorRequirement {
	return []strategy.IndicatorRequirement{
		{Name: "MACD", Params: map[string]int{"fast": m.fast, "slow": m.slow, "signal": m.signal}},
	}
}

func (m *MACDModule) RequiredHistory() int {
	return m.slow + m.signal + 5
}

func (m *MACDModule) Score(snap *strategy.MarketSnapshot) float64 {
	macd := snap.Indicators.MACD
	histogram := macd.Histogram

	if !m.initialized {
		m.prevHistogram = histogram
		m.initialized = true
		return 0
	}

	score := 0.0

	// Histogram direction and magnitude
	// Use a stable scale based on price level (0.1% of price as MACD scale)
	scale := 50.0 // fallback
	if len(snap.Klines) > 0 {
		price := snap.Klines[len(snap.Klines)-1].Close
		if price > 0 {
			scale = price * 0.001 // 0.1% of price
		}
	}
	score += tanhScore(histogram, 0, scale) * 0.5

	// Histogram zero-line crossover
	if m.prevHistogram <= 0 && histogram > 0 {
		score += 0.3 // bullish crossover
	} else if m.prevHistogram >= 0 && histogram < 0 {
		score -= 0.3 // bearish crossover
	}

	// Histogram momentum (expanding vs contracting)
	histChange := histogram - m.prevHistogram
	score += tanhScore(histChange, 0, scale*0.5) * 0.2

	m.prevHistogram = histogram

	return clamp(score, -1.0, 1.0)
}
