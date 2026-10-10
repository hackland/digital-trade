package mtfmacd

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jayce/btc-trader/internal/exchange"
	"go.uber.org/zap"
)

const (
	pollInterval = 30 * time.Second
	fetchBars    = 300
	maxHistory   = 200
	// A trigger older than this (e.g. found right after a restart) is shown but not pushed.
	pushFreshness = 20 * time.Minute
)

// Signal is one fired resonance signal.
type Signal struct {
	Eval
	FiredAt time.Time `json:"fired_at"`
}

// Watcher polls klines and raises resonance signals.
type Watcher struct {
	ex     exchange.Exchange
	symbol string
	logger *zap.Logger

	mu       sync.RWMutex
	params   Params
	tg       bool
	notify   func(ctx context.Context, text string)
	latest   *Eval
	updated  time.Time
	lastErr  string
	history  []Signal
	lastFire map[string]time.Time // direction -> 15m candle end already fired
}

// NewWatcher creates a watcher for one symbol.
func NewWatcher(ex exchange.Exchange, symbol string, p Params, telegram bool, logger *zap.Logger) *Watcher {
	return &Watcher{ex: ex, symbol: symbol, params: p, tg: telegram, logger: logger, lastFire: map[string]time.Time{}}
}

// SetNotifier installs the push function (e.g. Telegram). Safe to call after Run starts.
func (w *Watcher) SetNotifier(fn func(ctx context.Context, text string)) {
	w.mu.Lock()
	w.notify = fn
	w.mu.Unlock()
}

func (w *Watcher) Symbol() string { return w.symbol }

func (w *Watcher) Params() Params {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.params
}

// SetRequire15mBeyondZero toggles the 15m zero-axis filter at runtime.
func (w *Watcher) SetRequire15mBeyondZero(v bool) {
	w.mu.Lock()
	w.params.Require15mBeyondZero = v
	w.mu.Unlock()
}

// Status returns the latest evaluation, when it was computed and the last error.
func (w *Watcher) Status() (*Eval, time.Time, string) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.latest == nil {
		return nil, w.updated, w.lastErr
	}
	cp := *w.latest
	return &cp, w.updated, w.lastErr
}

// History returns fired signals, newest first.
func (w *Watcher) History() []Signal {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]Signal, len(w.history))
	for i, s := range w.history {
		out[len(w.history)-1-i] = s
	}
	return out
}

// Run polls until ctx is cancelled.
func (w *Watcher) Run(ctx context.Context) {
	w.logger.Info("mtf macd watcher started", zap.String("symbol", w.symbol))
	w.tick(ctx)
	t := time.NewTicker(pollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.tick(ctx)
		}
	}
}

// FetchClosed returns the latest `limit` closed candles.
func FetchClosed(ctx context.Context, ex exchange.Exchange, symbol, interval string, limit int) ([]exchange.Kline, error) {
	ks, err := ex.GetKlines(ctx, exchange.KlineRequest{Symbol: symbol, Interval: interval, Limit: limit})
	if err != nil {
		return nil, err
	}
	now := time.Now()
	for len(ks) > 0 && ks[len(ks)-1].CloseTime.After(now) {
		ks = ks[:len(ks)-1]
	}
	return ks, nil
}

func (w *Watcher) tick(ctx context.Context) {
	p := w.Params()
	var ss [3]*Series
	for i, iv := range []string{"4h", "1h", "15m"} {
		ks, err := FetchClosed(ctx, w.ex, w.symbol, iv, fetchBars)
		if err != nil || len(ks) == 0 {
			w.mu.Lock()
			w.lastErr = fmt.Sprintf("获取 %s K线失败: %v", iv, err)
			w.mu.Unlock()
			w.logger.Warn("mtf macd fetch failed", zap.String("interval", iv), zap.Error(err))
			return
		}
		ss[i] = BuildSeries(iv, ks, p)
	}
	last := len(ss[2].Klines) - 1
	ev, _, _ := Evaluate(w.symbol, ss[0], ss[1], ss[2], last, 0, 0, p)

	w.mu.Lock()
	w.latest, w.updated, w.lastErr = &ev, time.Now(), ""
	fire := ev.Triggered && !w.lastFire[ev.Direction].Equal(ev.Time)
	var notify func(context.Context, string)
	if fire {
		w.lastFire[ev.Direction] = ev.Time
		w.history = append(w.history, Signal{Eval: ev, FiredAt: time.Now()})
		if len(w.history) > maxHistory {
			w.history = w.history[len(w.history)-maxHistory:]
		}
		if w.tg && time.Since(ev.Time) <= pushFreshness {
			notify = w.notify
		}
	}
	w.mu.Unlock()

	if fire {
		w.logger.Info("mtf macd signal", zap.String("dir", ev.Direction), zap.Float64("price", ev.Price), zap.Time("candle", ev.Time))
		if notify != nil {
			notify(ctx, FormatMessage(ev, p))
		}
	}
}

// FormatMessage builds the Telegram text.
func FormatMessage(ev Eval, p Params) string {
	icon, dir, cross := "🟢", "做多", "金叉"
	if ev.Direction == DirShort {
		icon, dir, cross = "🔴", "做空", "死叉"
	}
	zero := ""
	if p.Require15mBeyondZero {
		zero = "（已启用15m 0轴过滤）"
	}
	return fmt.Sprintf("%s *【三周期共振】%s %s* %s\n价格: %.2f\n4h: DIF %.2f / DEA %.2f\n1h: DIF %.2f / DEA %.2f\n15m: %s，DIF %.2f / DEA %.2f\n15m K线收盘: %s",
		icon, ev.Symbol, dir, zero, ev.Price,
		ev.H4.MACD, ev.H4.Signal, ev.H1.MACD, ev.H1.Signal,
		cross, ev.M15.MACD, ev.M15.Signal,
		ev.Time.Local().Format("01-02 15:04"))
}
