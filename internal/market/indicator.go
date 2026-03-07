package market

import (
	"math"

	"github.com/jayce/btc-trader/internal/exchange"
	"github.com/jayce/btc-trader/internal/strategy"
)

// IndicatorComputer computes technical indicators from kline data.
type IndicatorComputer struct{}

// NewIndicatorComputer creates a new indicator computer.
func NewIndicatorComputer() *IndicatorComputer {
	return &IndicatorComputer{}
}

// ComputeAll computes all required indicators from the kline window.
func (ic *IndicatorComputer) ComputeAll(
	klines []exchange.Kline,
	requirements []strategy.IndicatorRequirement,
) strategy.IndicatorSet {
	closes := extractCloses(klines)
	highs := extractHighs(klines)
	lows := extractLows(klines)
	volumes := extractVolumes(klines)

	set := strategy.IndicatorSet{
		SMA:       make(map[int]float64),
		EMA:       make(map[int]float64),
		RSI:       make(map[int]float64),
		ATR:       make(map[int]float64),
		MFI:       make(map[int]float64),
		CMF:       make(map[int]float64),
		VolumeSMA: make(map[int]float64),
	}

	for _, req := range requirements {
		switch req.Name {
		case "SMA":
			period := req.Params["period"]
			set.SMA[period] = ic.ComputeSMA(closes, period)
		case "EMA":
			period := req.Params["period"]
			set.EMA[period] = ic.ComputeEMA(closes, period)
		case "MACD":
			fast := req.Params["fast"]
			slow := req.Params["slow"]
			signal := req.Params["signal"]
			if fast == 0 {
				fast = 12
			}
			if slow == 0 {
				slow = 26
			}
			if signal == 0 {
				signal = 9
			}
			set.MACD = ic.ComputeMACD(closes, fast, slow, signal)
		case "RSI":
			period := req.Params["period"]
			set.RSI[period] = ic.ComputeRSI(closes, period)
		case "BB":
			period := req.Params["period"]
			mult := 2.0
			if m, ok := req.Params["mult"]; ok {
				mult = float64(m)
			}
			set.BB = ic.ComputeBollingerBands(closes, period, mult)
		case "ATR":
			period := req.Params["period"]
			set.ATR[period] = ic.ComputeATR(klines, period)
		case "OBV":
			set.OBV = ic.ComputeOBV(closes, volumes)
		case "MFI":
			period := req.Params["period"]
			set.MFI[period] = ic.ComputeMFI(highs, lows, closes, volumes, period)
		case "VWAP":
			set.VWAP = ic.ComputeVWAP(highs, lows, closes, volumes)
		case "CMF":
			period := req.Params["period"]
			set.CMF[period] = ic.ComputeCMF(highs, lows, closes, volumes, period)
		case "ADL":
			set.ADL = ic.ComputeADL(highs, lows, closes, volumes)
		case "VolumeSMA":
			period := req.Params["period"]
			set.VolumeSMA[period] = ic.ComputeVolumeSMA(volumes, period)
		}
	}

	return set
}

// --- Price-based indicators ---

// ComputeSMA calculates Simple Moving Average.
func (ic *IndicatorComputer) ComputeSMA(closes []float64, period int) float64 {
	if len(closes) < period {
		return 0
	}
	sum := 0.0
	for _, c := range closes[len(closes)-period:] {
		sum += c
	}
	return sum / float64(period)
}

// ComputeEMA calculates Exponential Moving Average.
func (ic *IndicatorComputer) ComputeEMA(closes []float64, period int) float64 {
	if len(closes) < period {
		return 0
	}
	multiplier := 2.0 / float64(period+1)
	// Seed with SMA of first `period` values
	ema := ic.ComputeSMA(closes[:period], period)
	for _, close := range closes[period:] {
		ema = (close-ema)*multiplier + ema
	}
	return ema
}

// computeEMASeries returns the full EMA series for MACD computation.
func (ic *IndicatorComputer) computeEMASeries(closes []float64, period int) []float64 {
	if len(closes) < period {
		return nil
	}
	result := make([]float64, len(closes))
	multiplier := 2.0 / float64(period+1)

	// Seed with SMA
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += closes[i]
		result[i] = 0
	}
	result[period-1] = sum / float64(period)

	for i := period; i < len(closes); i++ {
		result[i] = (closes[i]-result[i-1])*multiplier + result[i-1]
	}
	return result
}

// ComputeMACD calculates MACD, Signal line, and Histogram.
func (ic *IndicatorComputer) ComputeMACD(closes []float64, fast, slow, signal int) strategy.MACDValue {
	if len(closes) < slow+signal {
		return strategy.MACDValue{}
	}

	fastSeries := ic.computeEMASeries(closes, fast)
	slowSeries := ic.computeEMASeries(closes, slow)

	if fastSeries == nil || slowSeries == nil {
		return strategy.MACDValue{}
	}

	// MACD line = fast EMA - slow EMA
	macdSeries := make([]float64, len(closes))
	startIdx := slow - 1
	for i := startIdx; i < len(closes); i++ {
		macdSeries[i] = fastSeries[i] - slowSeries[i]
	}

	// Signal line = EMA of MACD line
	validMACD := macdSeries[startIdx:]
	if len(validMACD) < signal {
		return strategy.MACDValue{}
	}

	signalEMA := ic.ComputeEMA(validMACD, signal)
	macdVal := macdSeries[len(macdSeries)-1]

	return strategy.MACDValue{
		MACD:      macdVal,
		Signal:    signalEMA,
		Histogram: macdVal - signalEMA,
	}
}

// ComputeRSI calculates Relative Strength Index.
func (ic *IndicatorComputer) ComputeRSI(closes []float64, period int) float64 {
	if len(closes) < period+1 {
		return 50 // Neutral default
	}

	// Use Wilder's smoothing method
	gains := 0.0
	losses := 0.0

	// Initial average gain/loss
	for i := 1; i <= period; i++ {
		change := closes[i] - closes[i-1]
		if change > 0 {
			gains += change
		} else {
			losses -= change
		}
	}

	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)

	// Smooth with subsequent data
	for i := period + 1; i < len(closes); i++ {
		change := closes[i] - closes[i-1]
		if change > 0 {
			avgGain = (avgGain*float64(period-1) + change) / float64(period)
			avgLoss = (avgLoss * float64(period-1)) / float64(period)
		} else {
			avgGain = (avgGain * float64(period-1)) / float64(period)
			avgLoss = (avgLoss*float64(period-1) - change) / float64(period)
		}
	}

	if avgLoss == 0 {
		return 100
	}
	rs := avgGain / avgLoss
	return 100 - 100/(1+rs)
}

// ComputeBollingerBands calculates Bollinger Bands.
func (ic *IndicatorComputer) ComputeBollingerBands(closes []float64, period int, stdDevMult float64) strategy.BollingerBands {
	if len(closes) < period {
		return strategy.BollingerBands{}
	}

	sma := ic.ComputeSMA(closes, period)

	variance := 0.0
	for _, c := range closes[len(closes)-period:] {
		diff := c - sma
		variance += diff * diff
	}
	stdDev := math.Sqrt(variance / float64(period))

	upper := sma + stdDevMult*stdDev
	lower := sma - stdDevMult*stdDev

	width := 0.0
	if sma > 0 {
		width = (upper - lower) / sma
	}

	return strategy.BollingerBands{
		Upper:  upper,
		Middle: sma,
		Lower:  lower,
		Width:  width,
	}
}

// ComputeATR calculates Average True Range.
func (ic *IndicatorComputer) ComputeATR(klines []exchange.Kline, period int) float64 {
	if len(klines) < period+1 {
		return 0
	}

	trueRanges := make([]float64, 0, len(klines)-1)
	for i := 1; i < len(klines); i++ {
		high := klines[i].High
		low := klines[i].Low
		prevClose := klines[i-1].Close

		tr := math.Max(high-low, math.Max(
			math.Abs(high-prevClose),
			math.Abs(low-prevClose),
		))
		trueRanges = append(trueRanges, tr)
	}

	if len(trueRanges) < period {
		return 0
	}

	// Initial ATR = SMA of first `period` true ranges
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += trueRanges[i]
	}
	atr := sum / float64(period)

	// Wilder's smoothing
	for i := period; i < len(trueRanges); i++ {
		atr = (atr*float64(period-1) + trueRanges[i]) / float64(period)
	}

	return atr
}

// --- Volume-based indicators ---

// ComputeOBV calculates On-Balance Volume.
func (ic *IndicatorComputer) ComputeOBV(closes, volumes []float64) float64 {
	if len(closes) < 2 || len(volumes) < 2 {
		return 0
	}

	obv := 0.0
	for i := 1; i < len(closes); i++ {
		if closes[i] > closes[i-1] {
			obv += volumes[i]
		} else if closes[i] < closes[i-1] {
			obv -= volumes[i]
		}
		// If equal, OBV stays the same
	}
	return obv
}

// ComputeMFI calculates Money Flow Index.
func (ic *IndicatorComputer) ComputeMFI(highs, lows, closes, volumes []float64, period int) float64 {
	n := len(closes)
	if n < period+1 {
		return 50 // Neutral default
	}

	posFlow := 0.0
	negFlow := 0.0

	for i := n - period; i < n; i++ {
		typicalPrice := (highs[i] + lows[i] + closes[i]) / 3.0
		prevTypical := (highs[i-1] + lows[i-1] + closes[i-1]) / 3.0
		rawMoneyFlow := typicalPrice * volumes[i]

		if typicalPrice > prevTypical {
			posFlow += rawMoneyFlow
		} else if typicalPrice < prevTypical {
			negFlow += rawMoneyFlow
		}
	}

	if negFlow == 0 {
		return 100
	}

	mfr := posFlow / negFlow
	return 100 - 100/(1+mfr)
}

// ComputeVWAP calculates Volume Weighted Average Price.
func (ic *IndicatorComputer) ComputeVWAP(highs, lows, closes, volumes []float64) float64 {
	if len(closes) == 0 {
		return 0
	}

	cumulativeTPV := 0.0 // typical price * volume
	cumulativeVol := 0.0

	for i := 0; i < len(closes); i++ {
		typicalPrice := (highs[i] + lows[i] + closes[i]) / 3.0
		cumulativeTPV += typicalPrice * volumes[i]
		cumulativeVol += volumes[i]
	}

	if cumulativeVol == 0 {
		return 0
	}
	return cumulativeTPV / cumulativeVol
}

// ComputeCMF calculates Chaikin Money Flow over a period.
// CMF = Sum(Money Flow Volume) / Sum(Volume) over the period.
// Range: -1.0 to +1.0. Positive = buying pressure, Negative = selling pressure.
func (ic *IndicatorComputer) ComputeCMF(highs, lows, closes, volumes []float64, period int) float64 {
	n := len(closes)
	if n < period || period <= 0 {
		return 0
	}

	sumMFV := 0.0 // Money Flow Volume
	sumVol := 0.0

	for i := n - period; i < n; i++ {
		hl := highs[i] - lows[i]
		if hl == 0 {
			continue // avoid division by zero
		}
		// Money Flow Multiplier = ((Close - Low) - (High - Close)) / (High - Low)
		mfm := ((closes[i] - lows[i]) - (highs[i] - closes[i])) / hl
		sumMFV += mfm * volumes[i]
		sumVol += volumes[i]
	}

	if sumVol == 0 {
		return 0
	}
	return sumMFV / sumVol
}

// ComputeADL calculates the Accumulation/Distribution Line (final value).
// ADL is a cumulative indicator: ADL += MFM * Volume for each bar.
func (ic *IndicatorComputer) ComputeADL(highs, lows, closes, volumes []float64) float64 {
	if len(closes) < 2 {
		return 0
	}

	adl := 0.0
	for i := 0; i < len(closes); i++ {
		hl := highs[i] - lows[i]
		if hl == 0 {
			continue
		}
		mfm := ((closes[i] - lows[i]) - (highs[i] - closes[i])) / hl
		adl += mfm * volumes[i]
	}
	return adl
}

// ComputeVolumeSMA calculates Simple Moving Average of volume over a period.
func (ic *IndicatorComputer) ComputeVolumeSMA(volumes []float64, period int) float64 {
	n := len(volumes)
	if n < period || period <= 0 {
		return 0
	}
	sum := 0.0
	for _, v := range volumes[n-period:] {
		sum += v
	}
	return sum / float64(period)
}

// --- Helpers ---

func extractCloses(klines []exchange.Kline) []float64 {
	closes := make([]float64, len(klines))
	for i, k := range klines {
		closes[i] = k.Close
	}
	return closes
}

func extractHighs(klines []exchange.Kline) []float64 {
	highs := make([]float64, len(klines))
	for i, k := range klines {
		highs[i] = k.High
	}
	return highs
}

func extractLows(klines []exchange.Kline) []float64 {
	lows := make([]float64, len(klines))
	for i, k := range klines {
		lows[i] = k.Low
	}
	return lows
}

func extractVolumes(klines []exchange.Kline) []float64 {
	volumes := make([]float64, len(klines))
	for i, k := range klines {
		volumes[i] = k.Volume
	}
	return volumes
}
