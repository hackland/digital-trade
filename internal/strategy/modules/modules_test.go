package modules

import (
	"testing"

	"github.com/jayce/btc-trader/internal/exchange"
	"github.com/jayce/btc-trader/internal/strategy"
)

// makeSnapshot creates a test MarketSnapshot with given indicators and klines.
func makeSnapshot(close, volume float64, indicators strategy.IndicatorSet) *strategy.MarketSnapshot {
	return &strategy.MarketSnapshot{
		Symbol: "BTCUSDT",
		Klines: []exchange.Kline{
			{Close: close * 0.99, High: close * 1.00, Low: close * 0.98, Volume: volume}, // prev bar
			{Close: close, High: close * 1.01, Low: close * 0.99, Volume: volume},         // current bar
		},
		Indicators: indicators,
	}
}

// makeSnapshotWithDirection creates a snapshot where price moved in a given direction.
func makeSnapshotWithDirection(close, volume float64, up bool, indicators strategy.IndicatorSet) *strategy.MarketSnapshot {
	prevClose := close * 0.98 // up direction
	if !up {
		prevClose = close * 1.02 // down direction
	}
	return &strategy.MarketSnapshot{
		Symbol: "BTCUSDT",
		Klines: []exchange.Kline{
			{Close: prevClose, High: prevClose * 1.01, Low: prevClose * 0.99, Volume: volume},
			{Close: close, High: close * 1.01, Low: close * 0.99, Volume: volume},
		},
		Indicators: indicators,
	}
}

func TestRegistryAllMeta(t *testing.T) {
	metas := AllMeta()
	if len(metas) != 12 {
		t.Errorf("expected 12 modules, got %d", len(metas))
	}

	names := make(map[string]bool)
	for _, m := range metas {
		names[m.Name] = true
		if m.Label == "" {
			t.Errorf("module %s has empty label", m.Name)
		}
		if m.Category == "" {
			t.Errorf("module %s has empty category", m.Name)
		}
	}

	expected := []string{"ema_cross", "macd", "sma_trend", "rsi", "kdj", "bb_position",
		"mfi", "cmf", "volume_ratio", "vroc", "force_index", "obv_trend"}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("missing module: %s", name)
		}
	}
}

func TestCreateModule(t *testing.T) {
	mod, ok := Create("rsi", map[string]interface{}{"period": 14})
	if !ok {
		t.Fatal("failed to create rsi module")
	}
	if mod.Name() != "rsi" {
		t.Errorf("expected name 'rsi', got %s", mod.Name())
	}

	_, ok = Create("nonexistent", nil)
	if ok {
		t.Error("should not create nonexistent module")
	}
}

func TestRSIModuleScore(t *testing.T) {
	mod, _ := Create("rsi", map[string]interface{}{"period": 14})

	// First call initializes
	snap := makeSnapshot(50000, 100, strategy.IndicatorSet{
		RSI: map[int]float64{14: 50},
	})
	mod.Score(snap) // init

	// RSI recovering from oversold: 15 → 22 (momentum positive + position oversold)
	snap.Indicators.RSI[14] = 15
	mod.Score(snap) // set prevRSI to 15
	snap.Indicators.RSI[14] = 22
	score := mod.Score(snap)
	if score <= 0 {
		t.Errorf("RSI recovering from oversold should give positive score, got %.2f", score)
	}

	// RSI dropping from overbought: 85 → 78 (momentum negative + position overbought)
	snap.Indicators.RSI[14] = 85
	mod.Score(snap) // set prevRSI to 85
	snap.Indicators.RSI[14] = 78
	score = mod.Score(snap)
	if score >= 0 {
		t.Errorf("RSI dropping from overbought should give negative score, got %.2f", score)
	}
}

func TestEMACrossModuleScore(t *testing.T) {
	mod, _ := Create("ema_cross", map[string]interface{}{"fast_period": 9, "slow_period": 21})

	// Initialize
	snap := makeSnapshot(50000, 100, strategy.IndicatorSet{
		EMA: map[int]float64{9: 50000, 21: 50000},
	})
	mod.Score(snap)

	// Golden cross: fast crosses above slow
	snap.Indicators.EMA[9] = 50200
	snap.Indicators.EMA[21] = 49800
	score := mod.Score(snap)
	if score <= 0 {
		t.Errorf("golden cross should give positive score, got %.2f", score)
	}

	// Death cross: fast crosses below slow
	snap.Indicators.EMA[9] = 49700
	snap.Indicators.EMA[21] = 50100
	score = mod.Score(snap)
	if score >= 0 {
		t.Errorf("death cross should give negative score, got %.2f", score)
	}
}

func TestMACDModuleScore(t *testing.T) {
	mod, _ := Create("macd", nil)

	// Initialize
	snap := makeSnapshot(50000, 100, strategy.IndicatorSet{
		MACD: strategy.MACDValue{MACD: 0, Signal: 0, Histogram: -10},
	})
	mod.Score(snap)

	// Bullish crossover: histogram goes positive
	snap.Indicators.MACD = strategy.MACDValue{MACD: 50, Signal: 30, Histogram: 20}
	score := mod.Score(snap)
	if score <= 0 {
		t.Errorf("bullish MACD crossover should give positive score, got %.2f", score)
	}
}

func TestKDJModuleScore(t *testing.T) {
	mod, _ := Create("kdj", nil)

	// Initialize with K below D
	snap := makeSnapshot(50000, 100, strategy.IndicatorSet{
		KDJ: strategy.KDJValue{K: 20, D: 25, J: 10},
	})
	mod.Score(snap)

	// Golden cross: K crosses above D, with strong K momentum up
	snap.Indicators.KDJ = strategy.KDJValue{K: 35, D: 30, J: 45}
	score := mod.Score(snap)
	if score <= 0 {
		t.Errorf("KDJ golden cross with momentum should give positive score, got %.2f", score)
	}
}

func TestVolumeRatioModuleScore(t *testing.T) {
	mod, _ := Create("volume_ratio", map[string]interface{}{"period": 20})

	// High volume on UP candle → positive
	snap := makeSnapshotWithDirection(50000, 200, true, strategy.IndicatorSet{
		VolumeSMA: map[int]float64{20: 100},
	})
	score := mod.Score(snap)
	if score <= 0 {
		t.Errorf("high volume on up candle should give positive score, got %.2f", score)
	}

	// High volume on DOWN candle → negative
	snap = makeSnapshotWithDirection(50000, 200, false, strategy.IndicatorSet{
		VolumeSMA: map[int]float64{20: 100},
	})
	score = mod.Score(snap)
	if score >= 0 {
		t.Errorf("high volume on down candle should give negative score, got %.2f", score)
	}
}

func TestVROCModuleScore(t *testing.T) {
	mod, _ := Create("vroc", map[string]interface{}{"period": 10})

	// Positive VROC (volume expanding) on UP candle
	snap := makeSnapshotWithDirection(50000, 100, true, strategy.IndicatorSet{
		VROC: map[int]float64{10: 80},
	})
	score := mod.Score(snap)
	if score <= 0 {
		t.Errorf("positive VROC on up candle should give positive score, got %.2f", score)
	}

	// Positive VROC on DOWN candle → negative (volume confirms the down move)
	snap = makeSnapshotWithDirection(50000, 100, false, strategy.IndicatorSet{
		VROC: map[int]float64{10: 80},
	})
	score = mod.Score(snap)
	if score >= 0 {
		t.Errorf("positive VROC on down candle should give negative score, got %.2f", score)
	}
}

func TestCMFModuleScore(t *testing.T) {
	mod, _ := Create("cmf", nil)

	// Positive CMF (buying pressure)
	snap := makeSnapshot(50000, 100, strategy.IndicatorSet{
		CMF: map[int]float64{20: 0.2},
	})
	score := mod.Score(snap)
	if score <= 0 {
		t.Errorf("positive CMF should give positive score, got %.2f", score)
	}

	// Negative CMF (selling pressure)
	snap.Indicators.CMF[20] = -0.25
	score = mod.Score(snap)
	if score >= 0 {
		t.Errorf("negative CMF should give negative score, got %.2f", score)
	}
}

func TestScoreRange(t *testing.T) {
	// All modules should return scores in [-1, 1]
	allModules := Available()
	indicators := strategy.IndicatorSet{
		SMA:        map[int]float64{50: 50000},
		EMA:        map[int]float64{9: 50100, 21: 49900},
		MACD:       strategy.MACDValue{MACD: 100, Signal: 50, Histogram: 50},
		RSI:        map[int]float64{14: 75},
		BB:         strategy.BollingerBands{Upper: 51000, Middle: 50000, Lower: 49000, Width: 0.04},
		ATR:        map[int]float64{14: 500},
		OBV:        1000000,
		MFI:        map[int]float64{14: 65},
		VWAP:       50000,
		CMF:        map[int]float64{20: 0.15},
		VolumeSMA:  map[int]float64{20: 100},
		VROC:       map[int]float64{10: 30},
		ForceIndex: map[int]float64{13: 5000},
		KDJ:        strategy.KDJValue{K: 60, D: 55, J: 70},
	}

	snap := makeSnapshot(50000, 150, indicators)

	for _, name := range allModules {
		mod, ok := Create(name, nil)
		if !ok {
			t.Errorf("failed to create module %s", name)
			continue
		}
		// Call twice (first init, second real score)
		mod.Score(snap)
		score := mod.Score(snap)
		if score < -1.0 || score > 1.0 {
			t.Errorf("module %s returned out-of-range score: %.2f", name, score)
		}
	}
}
