package mtfmacd

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/jayce/btc-trader/internal/exchange"
)

// Horizons are the forward windows (in 15m bars) used to judge a signal.
var Horizons = []int{4, 8, 16, 32, 96} // 1h 2h 4h 8h 24h

// BTSignal is one historical signal with its forward outcome.
type BTSignal struct {
	Time      time.Time `json:"time"`
	Direction string    `json:"direction"`
	Price     float64   `json:"price"`
	BelowZero bool      `json:"beyond_zero"` // 15m DIF在反方向0轴一侧(多:<0, 空:>0)
	// Forward return % in the signal direction at each horizon (nil when not enough data).
	Returns map[int]*float64 `json:"returns"`
	H4DIF   float64          `json:"h4_dif"`
	H1DIF   float64          `json:"h1_dif"`
	M15DIF  float64          `json:"m15_dif"`
	MFE     float64          `json:"mfe_pct"` // max favorable excursion within the longest horizon
	MAE     float64          `json:"mae_pct"` // max adverse excursion (<=0)
}

// BTBucket summarizes a set of signals at every horizon.
type BTBucket struct {
	Name      string                 `json:"name"`
	Count     int                    `json:"count"`
	ByHorizon map[int]*BTHorizonStat `json:"by_horizon"`
	AvgMFE    float64                `json:"avg_mfe_pct"`
	AvgMAE    float64                `json:"avg_mae_pct"`
}

type BTHorizonStat struct {
	Samples int     `json:"samples"`
	WinRate float64 `json:"win_rate"`
	AvgRet  float64 `json:"avg_return_pct"`
	AvgWin  float64 `json:"avg_win_pct"`
	AvgLoss float64 `json:"avg_loss_pct"`
	SumRet  float64 `json:"sum_return_pct"`
}

// BTResult is the backtest output.
type BTResult struct {
	Symbol   string     `json:"symbol"`
	Days     int        `json:"days"`
	Bars15m  int        `json:"bars_15m"`
	Signals  []BTSignal `json:"signals"`
	Buckets  []BTBucket `json:"buckets"`
	Horizons []int      `json:"horizons"`
	Candles  []Candle   `json:"candles"` // 1h candles over the test range, for charting
	Params   Params     `json:"params"`
}

// Candle is a compact OHLC bar (T = unix seconds of open time).
type Candle struct {
	T int64   `json:"t"`
	O float64 `json:"o"`
	H float64 `json:"h"`
	L float64 `json:"l"`
	C float64 `json:"c"`
}

// Bar is a candle plus its MACD values.
type Bar struct {
	Candle
	MACD   float64 `json:"macd"`
	Signal float64 `json:"signal"`
	Hist   float64 `json:"hist"`
}

// Window is the multi-timeframe context around one signal.
type Window struct {
	Time time.Time `json:"time"`
	M15  []Bar     `json:"m15"`
	H1   []Bar     `json:"h1"`
	H4   []Bar     `json:"h4"`
}

func toCandle(k exchange.Kline) Candle {
	return Candle{T: k.OpenTime.Unix(), O: k.Open, H: k.High, L: k.Low, C: k.Close}
}

func (s *Series) bars(from, to time.Time) []Bar {
	var out []Bar
	for i, k := range s.Klines {
		if k.OpenTime.Before(from) || k.OpenTime.After(to) || !s.valid(i) {
			continue
		}
		out = append(out, Bar{Candle: toCandle(k), MACD: s.macd.MACD[i], Signal: s.macd.Signal[i], Hist: s.macd.Histogram[i]})
	}
	return out
}

// SignalWindow loads 4h/1h/15m bars with MACD around time t.
func SignalWindow(ctx context.Context, ex exchange.Exchange, symbol string, t time.Time, p Params) (*Window, error) {
	w := &Window{Time: t}
	spans := map[string][2]time.Duration{
		"4h":  {-24 * 24 * time.Hour, 5 * 24 * time.Hour},
		"1h":  {-6 * 24 * time.Hour, 2 * 24 * time.Hour},
		"15m": {-30 * time.Hour, 30 * time.Hour},
	}
	for _, iv := range []string{"4h", "1h", "15m"} {
		sp := spans[iv]
		warm := 150 * intervalDur[iv]
		end := t.Add(sp[1])
		if end.After(time.Now()) {
			end = time.Now()
		}
		ks, err := FetchRange(ctx, ex, symbol, iv, t.Add(sp[0]).Add(-warm), end)
		if err != nil {
			return nil, err
		}
		bars := BuildSeries(iv, ks, p).bars(t.Add(sp[0]), end)
		switch iv {
		case "4h":
			w.H4 = bars
		case "1h":
			w.H1 = bars
		default:
			w.M15 = bars
		}
	}
	return w, nil
}

var intervalDur = map[string]time.Duration{"15m": 15 * time.Minute, "1h": time.Hour, "4h": 4 * time.Hour}

// FetchRange pages through exchange klines covering [start, end], closed candles only.
func FetchRange(ctx context.Context, ex exchange.Exchange, symbol, interval string, start, end time.Time) ([]exchange.Kline, error) {
	var all []exchange.Kline
	cur := start
	now := time.Now()
	for cur.Before(end) {
		c := cur
		ks, err := ex.GetKlines(ctx, exchange.KlineRequest{Symbol: symbol, Interval: interval, StartTime: &c, Limit: 1000})
		if err != nil {
			return nil, err
		}
		if len(ks) == 0 {
			break
		}
		all = append(all, ks...)
		next := ks[len(ks)-1].OpenTime.Add(intervalDur[interval])
		if !next.After(cur) {
			break
		}
		cur = next
	}
	for len(all) > 0 && all[len(all)-1].CloseTime.After(now) {
		all = all[:len(all)-1]
	}
	return all, nil
}

// loadSeries fetches 4h/1h/15m history (with warm-up) and builds the MACD series.
func loadSeries(ctx context.Context, ex exchange.Exchange, symbol string, days int, p Params) ([3]*Series, time.Time, error) {
	var ss [3]*Series
	end := time.Now()
	start := end.AddDate(0, 0, -days)
	warm := 120
	for i, iv := range []string{"4h", "1h", "15m"} {
		ks, err := FetchRange(ctx, ex, symbol, iv, start.Add(-time.Duration(warm)*intervalDur[iv]), end)
		if err != nil {
			return ss, start, fmt.Errorf("fetch %s: %w", iv, err)
		}
		ss[i] = BuildSeries(iv, ks, p)
	}
	if len(ss[2].Klines) < 100 {
		return ss, start, fmt.Errorf("15m K线不足: %d", len(ss[2].Klines))
	}
	return ss, start, nil
}

// Backtest replays the resonance rule over `days` of history. The 15m zero-axis
// option is evaluated as a split (all signals vs. signals that satisfy it), so
// params.Require15mBeyondZero is ignored here.
func Backtest(ctx context.Context, ex exchange.Exchange, symbol string, days int, p Params) (*BTResult, error) {
	ss, start, err := loadSeries(ctx, ex, symbol, days, p)
	if err != nil {
		return nil, err
	}
	s4, s1, s15 := ss[0], ss[1], ss[2]

	p.Require15mBeyondZero = false
	res := &BTResult{Symbol: symbol, Days: days, Bars15m: len(s15.Klines), Horizons: Horizons, Params: p}
	for _, k := range s1.Klines {
		if !k.CloseTime.Before(start) {
			res.Candles = append(res.Candles, toCandle(k))
		}
	}
	maxH := Horizons[len(Horizons)-1]
	h4, h1 := 0, 0
	for i := 1; i < len(s15.Klines); i++ {
		if s15.Klines[i].CloseTime.Before(start) {
			continue
		}
		var ev Eval
		ev, h4, h1 = Evaluate(symbol, s4, s1, s15, i, h4, h1, p)
		h4, h1 = max(h4, 0), max(h1, 0)
		if !ev.Triggered {
			continue
		}
		sig := BTSignal{Time: ev.Time, Direction: ev.Direction, Price: ev.Price, Returns: map[int]*float64{}, H4DIF: ev.H4.MACD, H1DIF: ev.H1.MACD, M15DIF: ev.M15.MACD}
		long := ev.Direction == DirLong
		sig.BelowZero = (long && ev.M15.MACD < 0) || (!long && ev.M15.MACD > 0)
		sgn := 1.0
		if !long {
			sgn = -1
		}
		for _, h := range Horizons {
			if i+h < len(s15.Klines) {
				r := sgn * (s15.Klines[i+h].Close/ev.Price - 1) * 100
				sig.Returns[h] = &r
			}
		}
		for j := i + 1; j <= i+maxH && j < len(s15.Klines); j++ {
			k := s15.Klines[j]
			fav, adv := k.High, k.Low
			if !long {
				fav, adv = k.Low, k.High
			}
			if v := sgn * (fav/ev.Price - 1) * 100; v > sig.MFE {
				sig.MFE = v
			}
			if v := sgn * (adv/ev.Price - 1) * 100; v < sig.MAE {
				sig.MAE = v
			}
		}
		res.Signals = append(res.Signals, sig)
	}

	filters := []struct {
		name string
		f    func(BTSignal) bool
	}{
		{"全部信号(不启用0轴过滤)", func(BTSignal) bool { return true }},
		{"启用15m 0轴过滤", func(s BTSignal) bool { return s.BelowZero }},
		{"仅做多(全部)", func(s BTSignal) bool { return s.Direction == DirLong }},
		{"仅做空(全部)", func(s BTSignal) bool { return s.Direction == DirShort }},
		{"仅做多(0轴过滤)", func(s BTSignal) bool { return s.Direction == DirLong && s.BelowZero }},
		{"仅做空(0轴过滤)", func(s BTSignal) bool { return s.Direction == DirShort && s.BelowZero }},
	}
	for _, f := range filters {
		if p.LongOnly && f.name != "全部信号(不启用0轴过滤)" && f.name != "启用15m 0轴过滤" {
			continue
		}
		res.Buckets = append(res.Buckets, bucket(f.name, res.Signals, f.f))
	}
	sort.SliceStable(res.Signals, func(a, b int) bool { return res.Signals[a].Time.After(res.Signals[b].Time) })
	return res, nil
}

func bucket(name string, sigs []BTSignal, keep func(BTSignal) bool) BTBucket {
	b := BTBucket{Name: name, ByHorizon: map[int]*BTHorizonStat{}}
	for _, h := range Horizons {
		b.ByHorizon[h] = &BTHorizonStat{}
	}
	var mfe, mae float64
	wins := map[int]int{}
	losses := map[int]int{}
	for _, s := range sigs {
		if !keep(s) {
			continue
		}
		b.Count++
		mfe += s.MFE
		mae += s.MAE
		for _, h := range Horizons {
			r := s.Returns[h]
			if r == nil {
				continue
			}
			st := b.ByHorizon[h]
			st.Samples++
			st.SumRet += *r
			if *r > 0 {
				wins[h]++
				st.AvgWin += *r
			} else {
				losses[h]++
				st.AvgLoss += *r
			}
		}
	}
	if b.Count > 0 {
		b.AvgMFE, b.AvgMAE = mfe/float64(b.Count), mae/float64(b.Count)
	}
	for _, h := range Horizons {
		st := b.ByHorizon[h]
		if st.Samples > 0 {
			st.WinRate = float64(wins[h]) / float64(st.Samples) * 100
			st.AvgRet = st.SumRet / float64(st.Samples)
		}
		if wins[h] > 0 {
			st.AvgWin /= float64(wins[h])
		}
		if losses[h] > 0 {
			st.AvgLoss /= float64(losses[h])
		}
	}
	return b
}
