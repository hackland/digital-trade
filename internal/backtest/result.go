package backtest

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jayce/btc-trader/internal/exchange"
)

// Result contains the complete output of a backtest run.
type Result struct {
	// Config
	Symbol      string        `json:"symbol"`
	Strategy    string        `json:"strategy"`
	Interval    string        `json:"interval"`
	StartTime   time.Time     `json:"start_time"`
	EndTime     time.Time     `json:"end_time"`
	Duration    time.Duration `json:"duration"`
	InitialCash float64       `json:"initial_cash"`

	// Trade history
	Trades []TradeRecord `json:"trades"`

	// Performance metrics
	Metrics Metrics `json:"metrics"`

	// Equity curve (time → equity value)
	EquityCurve []EquityPoint `json:"equity_curve"`
}

// TradeRecord represents a single trade in the backtest.
type TradeRecord struct {
	Timestamp time.Time `json:"timestamp"`
	Side      string    `json:"side"` // BUY, SELL
	Price     float64   `json:"price"`
	Quantity  float64   `json:"quantity"`
	Fee       float64   `json:"fee"`
	PnL       float64   `json:"pnl"` // realized PnL for sells
	Reason    string    `json:"reason"`
}

// EquityPoint is a single point on the equity curve.
type EquityPoint struct {
	Time   time.Time `json:"time"`
	Equity float64   `json:"equity"`
}

// Metrics holds all computed performance metrics.
type Metrics struct {
	// Returns
	FinalEquity    float64 `json:"final_equity"`
	TotalReturn    float64 `json:"total_return"`     // absolute USDT profit
	TotalReturnPct float64 `json:"total_return_pct"` // percentage return

	// Trade statistics
	TotalTrades int     `json:"total_trades"`
	WinTrades   int     `json:"win_trades"`
	LossTrades  int     `json:"lose_trades"`
	WinRate     float64 `json:"win_rate"` // 0.0 to 1.0

	// PnL
	AvgWin       float64 `json:"avg_win"`
	AvgLoss      float64 `json:"avg_loss"`
	LargestWin   float64 `json:"largest_win"`
	LargestLoss  float64 `json:"largest_loss"`
	ProfitFactor float64 `json:"profit_factor"` // total wins / total losses

	// Risk
	MaxDrawdown    float64 `json:"max_drawdown"`     // max peak-to-trough in USDT
	MaxDrawdownPct float64 `json:"max_drawdown_pct"` // max peak-to-trough in %
	SharpeRatio    float64 `json:"sharpe_ratio"`
	SortinoRatio   float64 `json:"sortino_ratio"`

	// Fees
	TotalFees float64 `json:"total_fees"`

	// Annualized
	AnnualizedReturn float64 `json:"annualized_return"`
}

// ComputeMetrics calculates all performance metrics from trades and equity curve.
func ComputeMetrics(trades []*exchange.Trade, equityCurve []EquityPoint, initialCash float64, duration time.Duration) Metrics {
	m := Metrics{}

	if len(equityCurve) == 0 {
		m.FinalEquity = initialCash
		return m
	}

	m.FinalEquity = equityCurve[len(equityCurve)-1].Equity
	m.TotalReturn = m.FinalEquity - initialCash
	if initialCash > 0 {
		m.TotalReturnPct = m.TotalReturn / initialCash * 100
	}

	// Compute fees
	for _, t := range trades {
		m.TotalFees += t.Fee
	}

	// Compute trade-pair PnL (buy→sell rounds)
	// Group trades into round trips
	roundTrips := computeRoundTrips(trades)
	m.TotalTrades = len(roundTrips)

	var totalWins, totalLosses float64
	for _, rt := range roundTrips {
		if rt > 0 {
			m.WinTrades++
			totalWins += rt
			if rt > m.LargestWin {
				m.LargestWin = rt
			}
		} else if rt < 0 {
			m.LossTrades++
			totalLosses += math.Abs(rt)
			if rt < m.LargestLoss {
				m.LargestLoss = rt
			}
		}
	}

	if m.TotalTrades > 0 {
		m.WinRate = float64(m.WinTrades) / float64(m.TotalTrades)
	}
	if m.WinTrades > 0 {
		m.AvgWin = totalWins / float64(m.WinTrades)
	}
	if m.LossTrades > 0 {
		m.AvgLoss = totalLosses / float64(m.LossTrades)
	}
	if totalLosses > 0 {
		m.ProfitFactor = totalWins / totalLosses
	}

	// Drawdown from equity curve
	peakEquity := equityCurve[0].Equity
	for _, ep := range equityCurve {
		if ep.Equity > peakEquity {
			peakEquity = ep.Equity
		}
		dd := peakEquity - ep.Equity
		ddPct := 0.0
		if peakEquity > 0 {
			ddPct = dd / peakEquity * 100
		}
		if dd > m.MaxDrawdown {
			m.MaxDrawdown = dd
		}
		if ddPct > m.MaxDrawdownPct {
			m.MaxDrawdownPct = ddPct
		}
	}

	// Sharpe & Sortino from equity curve returns
	if len(equityCurve) > 1 {
		returns := make([]float64, 0, len(equityCurve)-1)
		for i := 1; i < len(equityCurve); i++ {
			prev := equityCurve[i-1].Equity
			if prev > 0 {
				returns = append(returns, (equityCurve[i].Equity-prev)/prev)
			}
		}

		if len(returns) > 1 {
			avgReturn := mean(returns)
			stdDev := stddev(returns, avgReturn)
			downDev := downDeviation(returns, avgReturn)

			// Annualize: assume 365 periods per year for daily, adjust as needed
			periodsPerYear := 365.0
			if duration.Hours() > 0 {
				totalPeriods := float64(len(equityCurve))
				daysTotal := duration.Hours() / 24
				if daysTotal > 0 {
					periodsPerYear = totalPeriods / daysTotal * 365
				}
			}

			sqrtPeriods := math.Sqrt(periodsPerYear)
			if stdDev > 0 {
				m.SharpeRatio = avgReturn / stdDev * sqrtPeriods
			}
			if downDev > 0 {
				m.SortinoRatio = avgReturn / downDev * sqrtPeriods
			}
		}
	}

	// Annualized return
	years := duration.Hours() / (24 * 365)
	if years > 0 && initialCash > 0 {
		m.AnnualizedReturn = (math.Pow(m.FinalEquity/initialCash, 1.0/years) - 1) * 100
	}

	return m
}

// computeRoundTrips groups buy→sell trades into round trips and returns PnL list.
func computeRoundTrips(trades []*exchange.Trade) []float64 {
	var roundTrips []float64
	var buyAvg float64
	var buyQty float64

	for _, t := range trades {
		if t.Side == exchange.OrderSideBuy {
			// Weighted average entry price
			totalCost := buyAvg*buyQty + t.Price*t.Quantity
			buyQty += t.Quantity
			if buyQty > 0 {
				buyAvg = totalCost / buyQty
			}
		} else if t.Side == exchange.OrderSideSell {
			sellQty := t.Quantity
			if sellQty > buyQty {
				sellQty = buyQty
			}
			if sellQty > 0 {
				pnl := (t.Price - buyAvg) * sellQty
				roundTrips = append(roundTrips, pnl)
				buyQty -= sellQty
				if buyQty <= 0 {
					buyQty = 0
					buyAvg = 0
				}
			}
		}
	}

	return roundTrips
}

// PrintSummary prints a human-readable backtest summary.
func (r *Result) PrintSummary() string {
	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString("═══════════════════════════════════════════\n")
	sb.WriteString("           BACKTEST RESULTS\n")
	sb.WriteString("═══════════════════════════════════════════\n")
	sb.WriteString(fmt.Sprintf("  Symbol:     %s\n", r.Symbol))
	sb.WriteString(fmt.Sprintf("  Strategy:   %s\n", r.Strategy))
	sb.WriteString(fmt.Sprintf("  Interval:   %s\n", r.Interval))
	sb.WriteString(fmt.Sprintf("  Period:     %s ~ %s\n",
		r.StartTime.Format("2006-01-02"), r.EndTime.Format("2006-01-02")))
	sb.WriteString(fmt.Sprintf("  Duration:   %d days\n", int(r.Duration.Hours()/24)))
	sb.WriteString("───────────────────────────────────────────\n")
	sb.WriteString("  PERFORMANCE\n")
	sb.WriteString("───────────────────────────────────────────\n")
	sb.WriteString(fmt.Sprintf("  Initial:    $%.2f\n", r.InitialCash))
	sb.WriteString(fmt.Sprintf("  Final:      $%.2f\n", r.Metrics.FinalEquity))
	sb.WriteString(fmt.Sprintf("  Return:     $%.2f (%.2f%%)\n", r.Metrics.TotalReturn, r.Metrics.TotalReturnPct))
	sb.WriteString(fmt.Sprintf("  Annual:     %.2f%%\n", r.Metrics.AnnualizedReturn))
	sb.WriteString(fmt.Sprintf("  Fees:       $%.2f\n", r.Metrics.TotalFees))
	sb.WriteString("───────────────────────────────────────────\n")
	sb.WriteString("  TRADES\n")
	sb.WriteString("───────────────────────────────────────────\n")
	sb.WriteString(fmt.Sprintf("  Total:      %d\n", r.Metrics.TotalTrades))
	sb.WriteString(fmt.Sprintf("  Wins:       %d (%.1f%%)\n", r.Metrics.WinTrades, r.Metrics.WinRate*100))
	sb.WriteString(fmt.Sprintf("  Losses:     %d\n", r.Metrics.LossTrades))
	sb.WriteString(fmt.Sprintf("  Avg Win:    $%.2f\n", r.Metrics.AvgWin))
	sb.WriteString(fmt.Sprintf("  Avg Loss:   $%.2f\n", r.Metrics.AvgLoss))
	sb.WriteString(fmt.Sprintf("  Largest W:  $%.2f\n", r.Metrics.LargestWin))
	sb.WriteString(fmt.Sprintf("  Largest L:  $%.2f\n", r.Metrics.LargestLoss))
	sb.WriteString(fmt.Sprintf("  Profit Fac: %.2f\n", r.Metrics.ProfitFactor))
	sb.WriteString("───────────────────────────────────────────\n")
	sb.WriteString("  RISK\n")
	sb.WriteString("───────────────────────────────────────────\n")
	sb.WriteString(fmt.Sprintf("  Max DD:     $%.2f (%.2f%%)\n", r.Metrics.MaxDrawdown, r.Metrics.MaxDrawdownPct))
	sb.WriteString(fmt.Sprintf("  Sharpe:     %.2f\n", r.Metrics.SharpeRatio))
	sb.WriteString(fmt.Sprintf("  Sortino:    %.2f\n", r.Metrics.SortinoRatio))
	sb.WriteString("═══════════════════════════════════════════\n")
	return sb.String()
}

// --- math helpers ---

func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func stddev(values []float64, avg float64) float64 {
	if len(values) < 2 {
		return 0
	}
	sumSq := 0.0
	for _, v := range values {
		d := v - avg
		sumSq += d * d
	}
	return math.Sqrt(sumSq / float64(len(values)-1))
}

func downDeviation(values []float64, avg float64) float64 {
	if len(values) < 2 {
		return 0
	}
	sumSq := 0.0
	count := 0
	for _, v := range values {
		if v < avg {
			d := v - avg
			sumSq += d * d
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return math.Sqrt(sumSq / float64(count))
}
