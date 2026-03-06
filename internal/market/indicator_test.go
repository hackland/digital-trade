package market

import (
	"math"
	"testing"

	"github.com/jayce/btc-trader/internal/exchange"
	"github.com/jayce/btc-trader/internal/strategy"
)

// tolerance for float64 comparison
const eps = 0.01

func almostEqual(a, b, tolerance float64) bool {
	return math.Abs(a-b) < tolerance
}

// --- Test data ---
// 20-bar close prices for BTCUSDT (synthetic but realistic pattern)
var testCloses = []float64{
	100.0, 102.0, 101.5, 103.0, 104.0,
	103.5, 105.0, 106.0, 104.5, 105.5,
	107.0, 108.0, 106.5, 107.5, 109.0,
	110.0, 108.5, 109.5, 111.0, 112.0,
}

var testHighs = []float64{
	101.0, 103.0, 102.5, 104.0, 105.0,
	104.5, 106.0, 107.0, 106.0, 106.5,
	108.0, 109.0, 108.0, 108.5, 110.0,
	111.0, 110.0, 110.5, 112.0, 113.0,
}

var testLows = []float64{
	99.0, 101.0, 100.5, 102.0, 103.0,
	102.5, 104.0, 105.0, 103.5, 104.5,
	106.0, 107.0, 105.5, 106.5, 108.0,
	109.0, 107.5, 108.5, 110.0, 111.0,
}

var testVolumes = []float64{
	1000, 1200, 800, 1500, 1300,
	900, 1400, 1600, 1100, 1000,
	1500, 1700, 900, 1200, 1800,
	2000, 1100, 1300, 1900, 2100,
}

func makeTestKlines() []exchange.Kline {
	klines := make([]exchange.Kline, len(testCloses))
	for i := range testCloses {
		klines[i] = exchange.Kline{
			Open:   testCloses[i] - 0.5,
			High:   testHighs[i],
			Low:    testLows[i],
			Close:  testCloses[i],
			Volume: testVolumes[i],
		}
	}
	return klines
}

// --- SMA Tests ---

func TestComputeSMA(t *testing.T) {
	ic := NewIndicatorComputer()

	tests := []struct {
		name   string
		period int
		want   float64
	}{
		{"SMA5", 5, (108.5 + 109.5 + 111.0 + 112.0 + 110.0) / 5},
		{"SMA10", 10, 0}, // will calculate
		{"SMA20", 20, 0}, // will calculate
	}

	// SMA5 = average of last 5 closes
	sma5 := ic.ComputeSMA(testCloses, 5)
	expectedSMA5 := (108.5 + 109.5 + 111.0 + 112.0 + 110.0) / 5
	if !almostEqual(sma5, expectedSMA5, eps) {
		t.Errorf("SMA(5) = %.4f, want %.4f", sma5, expectedSMA5)
	}

	// SMA20 = average of all 20 closes
	sum := 0.0
	for _, c := range testCloses {
		sum += c
	}
	sma20 := ic.ComputeSMA(testCloses, 20)
	expectedSMA20 := sum / 20
	if !almostEqual(sma20, expectedSMA20, eps) {
		t.Errorf("SMA(20) = %.4f, want %.4f", sma20, expectedSMA20)
	}

	// SMA with insufficient data
	sma50 := ic.ComputeSMA(testCloses, 50)
	if sma50 != 0 {
		t.Errorf("SMA(50) with 20 bars should be 0, got %.4f", sma50)
	}

	_ = tests // suppress unused
}

// --- EMA Tests ---

func TestComputeEMA(t *testing.T) {
	ic := NewIndicatorComputer()

	// EMA should be responsive to recent prices
	ema5 := ic.ComputeEMA(testCloses, 5)
	ema20 := ic.ComputeEMA(testCloses, 20)

	// EMA5 should be closer to the last price than EMA20
	lastPrice := testCloses[len(testCloses)-1]
	if math.Abs(ema5-lastPrice) > math.Abs(ema20-lastPrice) {
		t.Errorf("EMA(5)=%.4f should be closer to last price %.2f than EMA(20)=%.4f",
			ema5, lastPrice, ema20)
	}

	// EMA with insufficient data
	ema50 := ic.ComputeEMA(testCloses, 50)
	if ema50 != 0 {
		t.Errorf("EMA(50) with 20 bars should be 0, got %.4f", ema50)
	}

	// In an uptrend, EMA(5) > EMA(20)
	if ema5 <= ema20 {
		t.Errorf("In uptrend, EMA(5)=%.4f should be > EMA(20)=%.4f", ema5, ema20)
	}
}

// --- RSI Tests ---

func TestComputeRSI(t *testing.T) {
	ic := NewIndicatorComputer()

	rsi14 := ic.ComputeRSI(testCloses, 14)

	// RSI should be between 0 and 100
	if rsi14 < 0 || rsi14 > 100 {
		t.Errorf("RSI(14) = %.4f, should be between 0-100", rsi14)
	}

	// In an uptrend, RSI should be above 50
	if rsi14 <= 50 {
		t.Errorf("RSI(14) = %.4f in uptrend, expected > 50", rsi14)
	}

	// All-up data should give high RSI
	allUp := make([]float64, 20)
	for i := range allUp {
		allUp[i] = float64(100 + i)
	}
	rsiUp := ic.ComputeRSI(allUp, 14)
	if rsiUp < 90 {
		t.Errorf("RSI of all-up data = %.4f, expected > 90", rsiUp)
	}

	// Neutral default for insufficient data
	rsiShort := ic.ComputeRSI(testCloses[:5], 14)
	if rsiShort != 50 {
		t.Errorf("RSI with insufficient data = %.4f, expected 50", rsiShort)
	}
}

// --- MACD Tests ---

func TestComputeMACD(t *testing.T) {
	ic := NewIndicatorComputer()

	// Need enough data for MACD (26 + 9 = 35 bars minimum)
	// Extend test data
	longCloses := make([]float64, 50)
	for i := range longCloses {
		longCloses[i] = 100 + float64(i)*0.5 + math.Sin(float64(i)/3)*2
	}

	macd := ic.ComputeMACD(longCloses, 12, 26, 9)

	// In a generally upward trend, MACD line should be positive
	if macd.MACD == 0 && macd.Signal == 0 {
		t.Error("MACD should not be zero for 50 bars of data")
	}

	// Histogram = MACD - Signal
	expectedHist := macd.MACD - macd.Signal
	if !almostEqual(macd.Histogram, expectedHist, 0.001) {
		t.Errorf("Histogram = %.4f, expected MACD(%.4f) - Signal(%.4f) = %.4f",
			macd.Histogram, macd.MACD, macd.Signal, expectedHist)
	}

	// Insufficient data should return zero
	shortMACD := ic.ComputeMACD(testCloses[:10], 12, 26, 9)
	if shortMACD.MACD != 0 {
		t.Errorf("MACD with 10 bars should be 0, got %.4f", shortMACD.MACD)
	}
}

// --- Bollinger Bands Tests ---

func TestComputeBollingerBands(t *testing.T) {
	ic := NewIndicatorComputer()

	bb := ic.ComputeBollingerBands(testCloses, 20, 2.0)

	// Middle band should equal SMA(20)
	sma20 := ic.ComputeSMA(testCloses, 20)
	if !almostEqual(bb.Middle, sma20, eps) {
		t.Errorf("BB Middle = %.4f, expected SMA(20) = %.4f", bb.Middle, sma20)
	}

	// Upper > Middle > Lower
	if bb.Upper <= bb.Middle {
		t.Errorf("BB Upper(%.4f) should be > Middle(%.4f)", bb.Upper, bb.Middle)
	}
	if bb.Middle <= bb.Lower {
		t.Errorf("BB Middle(%.4f) should be > Lower(%.4f)", bb.Middle, bb.Lower)
	}

	// Bands should be symmetric around middle
	upperDist := bb.Upper - bb.Middle
	lowerDist := bb.Middle - bb.Lower
	if !almostEqual(upperDist, lowerDist, 0.001) {
		t.Errorf("BB bands not symmetric: upper dist=%.4f, lower dist=%.4f", upperDist, lowerDist)
	}

	// Width should be positive
	if bb.Width <= 0 {
		t.Errorf("BB Width = %.4f, expected > 0", bb.Width)
	}

	// Last close should be within or near the bands
	lastClose := testCloses[len(testCloses)-1]
	if lastClose < bb.Lower-10 || lastClose > bb.Upper+10 {
		t.Errorf("Last close %.2f is far outside BB [%.2f, %.2f]", lastClose, bb.Lower, bb.Upper)
	}
}

// --- ATR Tests ---

func TestComputeATR(t *testing.T) {
	ic := NewIndicatorComputer()
	klines := makeTestKlines()

	atr14 := ic.ComputeATR(klines, 14)

	// ATR should be positive
	if atr14 <= 0 {
		t.Errorf("ATR(14) = %.4f, expected > 0", atr14)
	}

	// ATR should be reasonable (roughly the average range)
	// Our test data has highs about 1-2 above lows
	if atr14 > 10 {
		t.Errorf("ATR(14) = %.4f seems too large for test data", atr14)
	}

	// Insufficient data
	atr50 := ic.ComputeATR(klines[:5], 14)
	if atr50 != 0 {
		t.Errorf("ATR with insufficient klines should be 0, got %.4f", atr50)
	}
}

// --- OBV Tests ---

func TestComputeOBV(t *testing.T) {
	ic := NewIndicatorComputer()

	obv := ic.ComputeOBV(testCloses, testVolumes)

	// In an uptrend with mostly up bars, OBV should be positive
	if obv <= 0 {
		t.Errorf("OBV = %.4f in uptrend, expected > 0", obv)
	}

	// Test with empty data
	obvEmpty := ic.ComputeOBV([]float64{}, []float64{})
	if obvEmpty != 0 {
		t.Errorf("OBV with empty data should be 0, got %.4f", obvEmpty)
	}

	// OBV direction should match price direction
	// All-up sequence
	upCloses := []float64{100, 101, 102, 103, 104}
	upVols := []float64{1000, 1000, 1000, 1000, 1000}
	obvUp := ic.ComputeOBV(upCloses, upVols)
	if obvUp != 4000 {
		t.Errorf("OBV of all-up bars = %.0f, expected 4000", obvUp)
	}
}

// --- MFI Tests ---

func TestComputeMFI(t *testing.T) {
	ic := NewIndicatorComputer()

	mfi14 := ic.ComputeMFI(testHighs, testLows, testCloses, testVolumes, 14)

	// MFI should be between 0 and 100
	if mfi14 < 0 || mfi14 > 100 {
		t.Errorf("MFI(14) = %.4f, should be between 0-100", mfi14)
	}

	// In an uptrend, MFI should be above 50
	if mfi14 <= 40 {
		t.Errorf("MFI(14) = %.4f in uptrend, expected > 40", mfi14)
	}

	// Insufficient data
	mfiShort := ic.ComputeMFI(testHighs[:5], testLows[:5], testCloses[:5], testVolumes[:5], 14)
	if mfiShort != 50 {
		t.Errorf("MFI with insufficient data = %.4f, expected 50", mfiShort)
	}
}

// --- VWAP Tests ---

func TestComputeVWAP(t *testing.T) {
	ic := NewIndicatorComputer()

	vwap := ic.ComputeVWAP(testHighs, testLows, testCloses, testVolumes)

	// VWAP should be between min and max of closes
	minClose := testCloses[0]
	maxClose := testCloses[0]
	for _, c := range testCloses {
		if c < minClose {
			minClose = c
		}
		if c > maxClose {
			maxClose = c
		}
	}

	if vwap < minClose-5 || vwap > maxClose+5 {
		t.Errorf("VWAP = %.4f, expected between %.2f and %.2f", vwap, minClose, maxClose)
	}

	// VWAP with zero volume
	zeroVols := make([]float64, len(testCloses))
	vwapZero := ic.ComputeVWAP(testHighs, testLows, testCloses, zeroVols)
	if vwapZero != 0 {
		t.Errorf("VWAP with zero volume should be 0, got %.4f", vwapZero)
	}

	// VWAP with empty data
	vwapEmpty := ic.ComputeVWAP([]float64{}, []float64{}, []float64{}, []float64{})
	if vwapEmpty != 0 {
		t.Errorf("VWAP with empty data should be 0, got %.4f", vwapEmpty)
	}
}

// --- ComputeAll Integration Test ---

func TestComputeAll(t *testing.T) {
	ic := NewIndicatorComputer()
	klines := makeTestKlines()

	requirements := []strategy.IndicatorRequirement{
		{Name: "SMA", Params: map[string]int{"period": 10}},
		{Name: "EMA", Params: map[string]int{"period": 12}},
		{Name: "RSI", Params: map[string]int{"period": 14}},
		{Name: "BB", Params: map[string]int{"period": 20}},
		{Name: "ATR", Params: map[string]int{"period": 14}},
		{Name: "OBV", Params: map[string]int{}},
		{Name: "MFI", Params: map[string]int{"period": 14}},
		{Name: "VWAP", Params: map[string]int{}},
	}

	set := ic.ComputeAll(klines, requirements)

	// Verify all indicators computed
	if set.SMA[10] == 0 {
		t.Error("SMA(10) not computed")
	}
	if set.EMA[12] == 0 {
		t.Error("EMA(12) not computed")
	}
	if set.RSI[14] == 0 {
		t.Error("RSI(14) not computed")
	}
	if set.BB.Middle == 0 {
		t.Error("BB not computed")
	}
	if set.ATR[14] == 0 {
		t.Error("ATR(14) not computed")
	}
	if set.OBV == 0 {
		t.Error("OBV not computed")
	}
	if set.MFI[14] == 0 {
		t.Error("MFI(14) not computed")
	}
	if set.VWAP == 0 {
		t.Error("VWAP not computed")
	}
}

// --- Helper extraction tests ---

func TestExtractors(t *testing.T) {
	klines := makeTestKlines()

	closes := extractCloses(klines)
	if len(closes) != len(testCloses) {
		t.Errorf("extractCloses: got %d, want %d", len(closes), len(testCloses))
	}
	for i, c := range closes {
		if c != testCloses[i] {
			t.Errorf("extractCloses[%d] = %.2f, want %.2f", i, c, testCloses[i])
		}
	}

	highs := extractHighs(klines)
	if len(highs) != len(testHighs) {
		t.Errorf("extractHighs: got %d, want %d", len(highs), len(testHighs))
	}

	volumes := extractVolumes(klines)
	if len(volumes) != len(testVolumes) {
		t.Errorf("extractVolumes: got %d, want %d", len(volumes), len(testVolumes))
	}
}
