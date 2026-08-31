package app

import (
	"context"
	"sync"
	"time"

	"github.com/jayce/btc-trader/internal/storage"
	"github.com/jayce/btc-trader/internal/strategy"
	"github.com/jayce/btc-trader/internal/web/handler"
	"go.uber.org/zap"
)

// Default assumptions for virtual (alert-only) P&L tracking. There is no
// live equivalent of backtest's InitialCash/FeeRate in config today, so
// these mirror the values used by the backtest/optimization tooling
// (internal/backtest) closely enough to be directly comparable, and are
// kept as named constants so they're easy to find and change.
const (
	virtualInitialEquity = 10000.0 // starting virtual equity per (symbol, direction) ledger
	virtualFeeRate       = 0.001   // 0.1% per fill, mirrors typical Binance spot taker fee
)

// virtualLedgerKey identifies one independent virtual equity curve: a
// symbol crossed with a trade direction ("LONG" or "SHORT"). Longs and
// shorts are tracked separately, and each symbol is isolated from others —
// this mirrors backtest's separate long/short equity accounting.
type virtualLedgerKey struct {
	Symbol    string
	Direction string // "LONG" | "SHORT"
}

// virtualLedgerState is the in-memory state for one (symbol, direction) pair.
type virtualLedgerState struct {
	equity     float64
	inPosition bool
	entryPrice float64
	quantity   float64
}

// VirtualLedger tracks hypothetical P&L / equity for alert-only signals —
// i.e. signals that never place a real exchange order because they are
// either a Short/Cover (spot can't short, always alert-only) or a Buy/Sell
// blocked by app.signal_only. It persists every open/close leg to the
// virtual_trades table so the running equity curve survives a process
// restart (mirrors the real-position recovery pattern in trader.go).
//
// This is purely application/orchestration-level bookkeeping — it does not
// touch CustomWeightedStrategy's internal states map, and never places
// real orders or writes to the real trades table.
type VirtualLedger struct {
	store  storage.VirtualTradeRepository
	logger *zap.Logger

	mu    sync.Mutex
	state map[virtualLedgerKey]*virtualLedgerState

	allocPct float64 // fraction of virtual equity risked per trade
}

// NewVirtualLedger creates a virtual ledger. allocPct should mirror the
// live risk config's position sizing (e.g. cfg.Risk.AllocPct) so the
// virtual P&L is comparable to what a real trade would have risked; a
// sensible default is substituted if allocPct is not in (0, 1].
func NewVirtualLedger(store storage.VirtualTradeRepository, allocPct float64, logger *zap.Logger) *VirtualLedger {
	if allocPct <= 0 || allocPct > 1 {
		allocPct = 0.9
	}
	return &VirtualLedger{
		store:    store,
		logger:   logger,
		state:    make(map[virtualLedgerKey]*virtualLedgerState),
		allocPct: allocPct,
	}
}

func directionForAction(action strategy.Action) string {
	if action.IsShort() {
		return "SHORT"
	}
	return "LONG"
}

// getOrInit returns the in-memory state for (symbol, direction), lazily
// seeding it with the initial equity if never seen before in this process.
func (l *VirtualLedger) getOrInit(symbol, direction string) *virtualLedgerState {
	key := virtualLedgerKey{Symbol: symbol, Direction: direction}
	st, ok := l.state[key]
	if !ok {
		st = &virtualLedgerState{equity: virtualInitialEquity}
		l.state[key] = st
	}
	return st
}

// VirtualFillResult summarizes the effect of processing one alert-only
// signal through the ledger, for inclusion in logs/Telegram messages.
type VirtualFillResult struct {
	Symbol      string
	Direction   string // "LONG" | "SHORT"
	Opened      bool   // true if this leg opened a new virtual position
	Closed      bool   // true if this leg closed an existing virtual position
	EntryPrice  float64
	ExitPrice   float64
	PnLPct      float64 // only meaningful when Closed
	PnLUSDT     float64 // only meaningful when Closed
	EquityAfter float64
}

// OnAlertOnlySignal records the effect of an alert-only signal (Buy/Sell
// blocked by signal_only, or Short/Cover which is always alert-only) into
// the virtual ledger, persisting the leg so it survives restart. price is
// the close price of the kline that produced the signal. Returns nil if
// the action is Hold or doesn't correspond to an open/close leg we track
// (e.g. a Sell/Cover with no open virtual position — nothing to close).
func (l *VirtualLedger) OnAlertOnlySignal(ctx context.Context, sig *strategy.Signal, price float64) *VirtualFillResult {
	if sig == nil || price <= 0 {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	direction := directionForAction(sig.Action)
	st := l.getOrInit(sig.Symbol, direction)

	switch sig.Action {
	case strategy.Buy, strategy.Short:
		return l.openLocked(ctx, st, sig, price, direction)
	case strategy.Sell, strategy.Cover:
		return l.closeLocked(ctx, st, sig, price, direction)
	default:
		return nil
	}
}

func (l *VirtualLedger) openLocked(ctx context.Context, st *virtualLedgerState, sig *strategy.Signal, price float64, direction string) *VirtualFillResult {
	if st.inPosition {
		// Already have a virtual position open on this side; ignore duplicate
		// open signals rather than overwrite the tracked entry.
		return nil
	}

	qty := (st.equity * l.allocPct) / price
	fee := price * qty * virtualFeeRate

	st.inPosition = true
	st.entryPrice = price
	st.quantity = qty

	side := "BUY"
	if sig.Action == strategy.Short {
		side = "SHORT"
	}

	rec := &storage.VirtualTradeRecord{
		Symbol:       sig.Symbol,
		Side:         side,
		Price:        price,
		Quantity:     qty,
		Fee:          fee,
		Reason:       sig.Reason,
		StrategyName: sig.Strategy,
		Timestamp:    signalTimestamp(sig),
	}
	if err := l.store.SaveVirtualTrade(ctx, rec); err != nil {
		l.logger.Error("save virtual trade (open)", zap.String("symbol", sig.Symbol), zap.Error(err))
	}

	return &VirtualFillResult{
		Symbol:      sig.Symbol,
		Direction:   direction,
		Opened:      true,
		EntryPrice:  price,
		EquityAfter: st.equity,
	}
}

func (l *VirtualLedger) closeLocked(ctx context.Context, st *virtualLedgerState, sig *strategy.Signal, price float64, direction string) *VirtualFillResult {
	if !st.inPosition || st.entryPrice <= 0 {
		// Nothing virtual to close (e.g. Cover with no prior virtual Short —
		// can legitimately happen right after startup before recovery, or if
		// the strategy's own state and the ledger's ever diverge).
		return nil
	}

	entry := st.entryPrice
	qty := st.quantity

	var pnlPct float64
	if direction == "SHORT" {
		pnlPct = (entry - price) / entry
	} else {
		pnlPct = (price - entry) / entry
	}

	// Two-way fee estimate (entry + exit), mirrors backtest's round-trip fee
	// pattern (netPnL := grossPnL - entryFee - exitFee in internal/backtest/result.go).
	entryFee := entry * qty * virtualFeeRate
	exitFee := price * qty * virtualFeeRate
	grossPnL := pnlPct * entry * qty
	netPnL := grossPnL - entryFee - exitFee

	st.equity *= 1 + pnlPct*l.allocPct
	st.equity -= entryFee + exitFee
	st.inPosition = false
	st.entryPrice = 0
	st.quantity = 0

	side := "SELL"
	if sig.Action == strategy.Cover {
		side = "COVER"
	}

	pnl := netPnL
	equityAfter := st.equity
	rec := &storage.VirtualTradeRecord{
		Symbol:       sig.Symbol,
		Side:         side,
		Price:        price,
		Quantity:     qty,
		Fee:          exitFee,
		PnL:          &pnl,
		EquityAfter:  &equityAfter,
		Reason:       sig.Reason,
		StrategyName: sig.Strategy,
		Timestamp:    signalTimestamp(sig),
	}
	if err := l.store.SaveVirtualTrade(ctx, rec); err != nil {
		l.logger.Error("save virtual trade (close)", zap.String("symbol", sig.Symbol), zap.Error(err))
	}

	return &VirtualFillResult{
		Symbol:      sig.Symbol,
		Direction:   direction,
		Closed:      true,
		EntryPrice:  entry,
		ExitPrice:   price,
		PnLPct:      pnlPct * 100,
		PnLUSDT:     netPnL,
		EquityAfter: st.equity,
	}
}

func signalTimestamp(sig *strategy.Signal) time.Time {
	if sig.Timestamp.IsZero() {
		return time.Now()
	}
	return sig.Timestamp
}

// Recover reconstructs in-memory ledger state from the last persisted
// virtual_trades rows for each (symbol, direction) pair, so equity and any
// open virtual position survive a process restart. Mirrors the shape of
// the real-position recovery block in trader.go Run(): read the latest
// record, and if it's an unmatched "open" leg, restore the entry price;
// otherwise (or if none exist) start from the initial equity / flat state.
func (l *VirtualLedger) Recover(ctx context.Context, symbols []string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, symbol := range symbols {
		for _, direction := range []string{"LONG", "SHORT"} {
			st := l.getOrInit(symbol, direction)

			openSide, closeSide := "BUY", "SELL"
			if direction == "SHORT" {
				openSide, closeSide = "SHORT", "COVER"
			}

			latestClose, err := l.store.GetLatestVirtualTrade(ctx, symbol, closeSide)
			if err != nil {
				l.logger.Warn("virtual ledger recovery: query latest close failed",
					zap.String("symbol", symbol), zap.String("direction", direction), zap.Error(err))
			}
			latestOpen, err := l.store.GetLatestVirtualTrade(ctx, symbol, openSide)
			if err != nil {
				l.logger.Warn("virtual ledger recovery: query latest open failed",
					zap.String("symbol", symbol), zap.String("direction", direction), zap.Error(err))
			}

			if latestClose != nil && latestClose.EquityAfter != nil {
				st.equity = *latestClose.EquityAfter
			}

			// An open leg with no matching close after it means the virtual
			// position is still open — restore entry price/quantity so the
			// next alert-only close signal computes PnL correctly.
			if latestOpen != nil && (latestClose == nil || latestOpen.Timestamp.After(latestClose.Timestamp)) {
				st.inPosition = true
				st.entryPrice = latestOpen.Price
				st.quantity = latestOpen.Quantity
				l.logger.Info("virtual ledger: recovered open position",
					zap.String("symbol", symbol),
					zap.String("direction", direction),
					zap.Float64("entry_price", latestOpen.Price),
					zap.Float64("equity", st.equity),
				)
			} else {
				l.logger.Info("virtual ledger: recovered flat/closed state",
					zap.String("symbol", symbol),
					zap.String("direction", direction),
					zap.Float64("equity", st.equity),
				)
			}
		}
	}
}

// Snapshot returns a read-only copy of current ledger state for the given
// symbol, keyed by direction ("LONG"/"SHORT"). Implements
// handler.VirtualLedgerReader for the dashboard read endpoint
// (GET /api/v1/virtual-ledger).
func (l *VirtualLedger) Snapshot(symbol string) map[string]handler.VirtualLedgerSnapshotView {
	l.mu.Lock()
	defer l.mu.Unlock()

	out := make(map[string]handler.VirtualLedgerSnapshotView)
	for _, direction := range []string{"LONG", "SHORT"} {
		key := virtualLedgerKey{Symbol: symbol, Direction: direction}
		st, ok := l.state[key]
		if !ok {
			out[direction] = handler.VirtualLedgerSnapshotView{Equity: virtualInitialEquity}
			continue
		}
		out[direction] = handler.VirtualLedgerSnapshotView{
			Equity:     st.equity,
			InPosition: st.inPosition,
			EntryPrice: st.entryPrice,
			Quantity:   st.quantity,
		}
	}
	return out
}

// logVirtualLedgerResult logs the outcome of a virtual ledger fill, if any.
// No-op when vres is nil (e.g. Hold, a real-order path, or a close signal
// with no matching virtual open to settle against).
func logVirtualLedgerResult(logger *zap.Logger, vres *VirtualFillResult) {
	if vres == nil {
		return
	}
	if vres.Closed {
		logger.Info("virtual ledger: closed",
			zap.String("symbol", vres.Symbol),
			zap.String("direction", vres.Direction),
			zap.Float64("entry_price", vres.EntryPrice),
			zap.Float64("exit_price", vres.ExitPrice),
			zap.Float64("pnl_pct", vres.PnLPct),
			zap.Float64("pnl_usdt", vres.PnLUSDT),
			zap.Float64("equity_after", vres.EquityAfter),
		)
	} else if vres.Opened {
		logger.Info("virtual ledger: opened",
			zap.String("symbol", vres.Symbol),
			zap.String("direction", vres.Direction),
			zap.Float64("entry_price", vres.EntryPrice),
			zap.Float64("equity", vres.EquityAfter),
		)
	}
}

// virtualFillMetadata converts a VirtualFillResult into the flat
// map[string]float64 shape eventbus.SignalEvent.Metadata expects, so
// Telegram/dashboard consumers of EventSignal can surface the hypothetical
// PnL and running equity without a new event type. Returns nil when there's
// nothing to report (vres == nil), which keeps existing consumers unaffected.
func virtualFillMetadata(vres *VirtualFillResult) map[string]float64 {
	if vres == nil {
		return nil
	}
	md := map[string]float64{
		"virtual_equity": vres.EquityAfter,
	}
	if vres.Opened {
		md["virtual_opened"] = 1
		md["virtual_entry_price"] = vres.EntryPrice
	}
	if vres.Closed {
		md["virtual_closed"] = 1
		md["virtual_entry_price"] = vres.EntryPrice
		md["virtual_exit_price"] = vres.ExitPrice
		md["virtual_pnl_pct"] = vres.PnLPct
		md["virtual_pnl_usdt"] = vres.PnLUSDT
	}
	return md
}
