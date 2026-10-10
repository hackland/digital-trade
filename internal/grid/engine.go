package grid

import (
	"fmt"

	"github.com/jayce/btc-trader/internal/exchange"
)

// Engine drives a grid-trading backtest simulation.
//
// Model: the range [LowerPrice, UpperPrice] is divided into slots by
// equal-ratio grid levels. Each slot i spans [levels[i], levels[i+1]] and is
// in one of three states: "armed" (a resting buy order sits at levels[i]),
// "holding" (bought at levels[i], a resting sell order sits at levels[i+1]),
// or neither (price has never traded above levels[i], so no buy order has
// ever had reason to rest there yet).
//
// Per bar, a slot's state transition is decided from its state as of the
// START of the bar, and all transitions are applied only once the whole bar
// has been processed — so a buy filled this bar can't also have its paired
// sell fill in the same bar, and a level newly discovered (armed) this bar
// can't also fill a buy in the same bar. This avoids modeling the exact
// intrabar price path, at the cost of slightly under-counting fills compared
// to a true tick-level simulation — using 1m klines keeps that gap small.
type Engine struct {
	cfg Config
}

// NewEngine creates a grid backtest engine.
func NewEngine(cfg Config) *Engine {
	return &Engine{cfg: cfg}
}

// Run executes the grid backtest on the given kline data.
func (e *Engine) Run(klines []exchange.Kline) (*Result, error) {
	if err := e.cfg.Validate(); err != nil {
		return nil, err
	}
	if len(klines) == 0 {
		return nil, fmt.Errorf("no kline data to backtest")
	}

	levels := e.cfg.BuildLevels()
	nSlots := len(levels) - 1
	if nSlots < 1 {
		return nil, fmt.Errorf("grid_spacing_pct too large for the given range: fewer than 2 levels generated")
	}

	startPrice := klines[0].Open
	armed := make([]bool, nSlots)
	holding := make([]bool, nSlots)
	qty := make([]float64, nSlots)
	costBasis := make([]float64, nSlots) // cash paid (incl. buy fee) for the currently-held lot in this slot
	for i := 0; i < nSlots; i++ {
		armed[i] = levels[i] < startPrice
	}

	cash := e.cfg.InitialCash
	var trades []Trade
	var equityCurve []EquityPoint
	stoppedOut := false
	stopReason := ""
	maxConcurrent := 0
	peakEquity := e.cfg.InitialCash
	maxDrawdown := 0.0

	stopLossPrice := 0.0
	if e.cfg.StopLossBelowPct > 0 {
		stopLossPrice = e.cfg.LowerPrice * (1 - e.cfg.StopLossBelowPct)
	}

	for _, bar := range klines {
		if stoppedOut {
			break
		}

		buysAllowed := !(e.cfg.PauseBuyAboveUpper && bar.Close > e.cfg.UpperPrice)

		newArmed := append([]bool(nil), armed...)
		newHolding := append([]bool(nil), holding...)
		newQty := append([]float64(nil), qty...)
		newCostBasis := append([]float64(nil), costBasis...)

		for i := 0; i < nSlots; i++ {
			lo, hi := levels[i], levels[i+1]

			switch {
			case holding[i]:
				// Sell-armed: fires when price rises through the top of this slot.
				if bar.High >= hi {
					q := qty[i]
					proceeds := hi * q
					fee := proceeds * e.cfg.FeeRate
					netProceeds := proceeds - fee
					pnl := netProceeds - costBasis[i]

					cash += netProceeds
					trades = append(trades, Trade{
						Timestamp: bar.OpenTime, Side: SideSell, Price: hi, Quantity: q,
						Fee: fee, PnL: pnl, SlotLow: lo, SlotHigh: hi,
					})
					newHolding[i] = false
					newQty[i] = 0
					newCostBasis[i] = 0
					newArmed[i] = true // re-arm the buy for the next dip
				}

			case armed[i]:
				if buysAllowed && bar.Low <= lo {
					q := e.cfg.USDTPerGrid / lo
					fee := lo * q * e.cfg.FeeRate
					cost := lo*q + fee

					cash -= cost
					trades = append(trades, Trade{
						Timestamp: bar.OpenTime, Side: SideBuy, Price: lo, Quantity: q,
						Fee: fee, PnL: 0, SlotLow: lo, SlotHigh: hi,
					})
					newHolding[i] = true
					newQty[i] = q
					newCostBasis[i] = cost
					newArmed[i] = false
				}

			default:
				// Neither armed nor holding: arm once price has traded above
				// this slot's floor, mirroring a real grid's resting buy
				// orders being (re)placed as price discovers new territory.
				// Applied via newArmed so it only takes effect next bar.
				if buysAllowed && bar.High >= lo {
					newArmed[i] = true
				}
			}
		}

		armed, holding, qty, costBasis = newArmed, newHolding, newQty, newCostBasis

		// Stop-loss: liquidate every open slot at this bar's close and halt.
		if stopLossPrice > 0 && bar.Close < stopLossPrice {
			for i := 0; i < nSlots; i++ {
				if !holding[i] {
					continue
				}
				q := qty[i]
				proceeds := bar.Close * q
				fee := proceeds * e.cfg.FeeRate
				netProceeds := proceeds - fee
				pnl := netProceeds - costBasis[i]

				cash += netProceeds
				trades = append(trades, Trade{
					Timestamp: bar.OpenTime, Side: SideForceClose, Price: bar.Close, Quantity: q,
					Fee: fee, PnL: pnl, SlotLow: levels[i], SlotHigh: levels[i+1],
				})
				holding[i] = false
				qty[i] = 0
				costBasis[i] = 0
				armed[i] = false
			}
			stoppedOut = true
			stopReason = fmt.Sprintf(
				"close %.2f fell below stop-loss %.2f (lower bound %.2f minus %.1f%%)",
				bar.Close, stopLossPrice, e.cfg.LowerPrice, e.cfg.StopLossBelowPct*100,
			)
		}

		// Mark-to-market equity for this bar.
		heldQty := 0.0
		concurrent := 0
		for i := 0; i < nSlots; i++ {
			if holding[i] {
				heldQty += qty[i]
				concurrent++
			}
		}
		if concurrent > maxConcurrent {
			maxConcurrent = concurrent
		}
		equity := cash + heldQty*bar.Close
		equityCurve = append(equityCurve, EquityPoint{Time: bar.OpenTime, Equity: equity})
		if equity > peakEquity {
			peakEquity = equity
		}
		if peakEquity > 0 {
			if dd := (peakEquity - equity) / peakEquity * 100; dd > maxDrawdown {
				maxDrawdown = dd
			}
		}
	}

	lastClose := klines[len(klines)-1].Close
	remainingQty := 0.0
	for i := 0; i < nSlots; i++ {
		if holding[i] {
			remainingQty += qty[i]
		}
	}

	realizedPnL, totalFees := 0.0, 0.0
	completedRoundTrips := 0
	for _, t := range trades {
		totalFees += t.Fee
		if t.Side == SideSell || t.Side == SideForceClose {
			realizedPnL += t.PnL
		}
		if t.Side == SideSell {
			completedRoundTrips++
		}
	}

	remainingValue := remainingQty * lastClose
	finalEquity := cash + remainingValue

	return &Result{
		Symbol:         e.cfg.Symbol,
		Interval:       e.cfg.Interval,
		StartTime:      klines[0].OpenTime,
		EndTime:        klines[len(klines)-1].OpenTime,
		InitialCash:    e.cfg.InitialCash,
		UpperPrice:     e.cfg.UpperPrice,
		LowerPrice:     e.cfg.LowerPrice,
		GridSpacingPct: e.cfg.GridSpacingPct,
		GridLevels:     levels,
		Trades:         trades,
		EquityCurve:    equityCurve,
		Metrics: Metrics{
			CompletedRoundTrips:   completedRoundTrips,
			RealizedPnL:           realizedPnL,
			TotalFees:             totalFees,
			RemainingQty:          remainingQty,
			RemainingValue:        remainingValue,
			FinalCash:             cash,
			FinalEquity:           finalEquity,
			TotalReturnPct:        (finalEquity - e.cfg.InitialCash) / e.cfg.InitialCash * 100,
			MaxDrawdownPct:        maxDrawdown,
			MaxConcurrentHoldings: maxConcurrent,
		},
		StoppedOut: stoppedOut,
		StopReason: stopReason,
	}, nil
}
