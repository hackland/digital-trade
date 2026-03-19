package modules

import "github.com/jayce/btc-trader/internal/strategy"

// KDJModule scores based on KDJ stochastic golden/death cross and J-value extremes.
type KDJModule struct {
	period      int
	kSmooth     int
	dSmooth     int
	prevK       float64
	prevD       float64
	initialized bool
}

func (m *KDJModule) Name() string        { return "kdj" }
func (m *KDJModule) Category() string     { return "momentum" }
func (m *KDJModule) Label() string        { return "KDJ 随机指标" }
func (m *KDJModule) DefaultWeight() float64 { return 0.15 }

func (m *KDJModule) Description() string {
	return "K/D金叉死叉信号，J值超买超卖(>90卖，<10买)，短线动量指标"
}

func (m *KDJModule) ParamSchema() []ParamSchema {
	return []ParamSchema{
		{Key: "period", Label: "周期", Type: "int", Default: 9, Min: 5, Max: 30, Step: 1},
		{Key: "k_smooth", Label: "K平滑", Type: "int", Default: 3, Min: 2, Max: 5, Step: 1},
		{Key: "d_smooth", Label: "D平滑", Type: "int", Default: 3, Min: 2, Max: 5, Step: 1},
	}
}

func (m *KDJModule) Init(params map[string]interface{}) {
	m.period = 9
	m.kSmooth = 3
	m.dSmooth = 3
	if v, ok := params["period"]; ok {
		m.period = toInt(v)
	}
	if v, ok := params["k_smooth"]; ok {
		m.kSmooth = toInt(v)
	}
	if v, ok := params["d_smooth"]; ok {
		m.dSmooth = toInt(v)
	}
}

func (m *KDJModule) RequiredIndicators() []strategy.IndicatorRequirement {
	return []strategy.IndicatorRequirement{
		{Name: "KDJ", Params: map[string]int{"period": m.period, "k_smooth": m.kSmooth, "d_smooth": m.dSmooth}},
	}
}

func (m *KDJModule) RequiredHistory() int {
	return m.period + 10
}

func (m *KDJModule) Score(snap *strategy.MarketSnapshot) float64 {
	kdj := snap.Indicators.KDJ
	k, d, j := kdj.K, kdj.D, kdj.J

	if !m.initialized {
		m.prevK = k
		m.prevD = d
		m.initialized = true
		return 0
	}

	score := 0.0

	// J-value: only contrarian at genuine extremes (J < 10 or J > 90)
	if j < 10 {
		score += tanhScore(10-j, 0, 8) * 0.2 // oversold = buy
	} else if j > 90 {
		score += -tanhScore(j-90, 0, 8) * 0.2 // overbought = sell
	}

	// K/D crossover
	if m.prevK <= m.prevD && k > d {
		score += 0.3 // golden cross
	} else if m.prevK >= m.prevD && k < d {
		score -= 0.3 // death cross
	}

	// K value momentum -- primary signal driver
	kChange := k - m.prevK
	score += tanhScore(kChange, 0, 8) * 0.5

	m.prevK = k
	m.prevD = d

	return clamp(score, -1.0, 1.0)
}
