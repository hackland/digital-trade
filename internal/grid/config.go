// Package grid implements a spot grid-trading backtest: a ladder of buy/sell
// levels within a price range, independent of the signal-driven Strategy
// interface used by internal/strategy — a grid position is many concurrent
// partial fills across price levels, not a single flat/long position.
package grid

import "fmt"

// Config configures one grid backtest run.
type Config struct {
	Symbol   string
	Interval string // kline interval used for simulation, e.g. "1m"

	UpperPrice float64
	LowerPrice float64

	// GridSpacingPct is the equal-ratio spacing between adjacent grid levels,
	// e.g. 0.01 = 1%. Levels are generated geometrically from LowerPrice up to
	// UpperPrice: p, p*(1+spacing), p*(1+spacing)^2, ... — equal-ratio spacing
	// suits crypto's percentage-scale moves better than equal-dollar spacing.
	GridSpacingPct float64

	USDTPerGrid float64 // fixed USDT amount bought/sold at each grid level (equal-amount grid)
	FeeRate     float64 // e.g. 0.001 = 0.1%, mirrors typical Binance spot taker fee
	InitialCash float64

	// StopLossBelowPct: if the close price falls this far below LowerPrice,
	// liquidate all remaining inventory at the close price and halt the grid
	// entirely. 0 disables the stop-loss (not recommended).
	StopLossBelowPct float64

	// PauseBuyAboveUpper: when true, no new buy fills or re-arms happen while
	// the close price is above UpperPrice — existing inventory still sells
	// normally as price climbs through remaining levels. The pause is not
	// sticky: normal buying resumes once price closes back at/below
	// UpperPrice. Defaults to true via NewConfig.
	PauseBuyAboveUpper bool
}

// NewConfig returns a Config with recommended defaults for fields not
// meaningfully defaultable to zero (PauseBuyAboveUpper).
func NewConfig() Config {
	return Config{
		PauseBuyAboveUpper: true,
	}
}

func (c *Config) Validate() error {
	if c.Symbol == "" {
		return fmt.Errorf("symbol is required")
	}
	if c.UpperPrice <= 0 || c.LowerPrice <= 0 {
		return fmt.Errorf("upper_price and lower_price must be > 0")
	}
	if c.UpperPrice <= c.LowerPrice {
		return fmt.Errorf("upper_price (%.2f) must be > lower_price (%.2f)", c.UpperPrice, c.LowerPrice)
	}
	if c.GridSpacingPct <= 0 {
		return fmt.Errorf("grid_spacing_pct must be > 0")
	}
	if c.USDTPerGrid <= 0 {
		return fmt.Errorf("usdt_per_grid must be > 0")
	}
	if c.FeeRate < 0 {
		return fmt.Errorf("fee_rate must be >= 0")
	}
	if c.InitialCash <= 0 {
		return fmt.Errorf("initial_cash must be > 0")
	}
	if c.StopLossBelowPct < 0 {
		return fmt.Errorf("stop_loss_below_pct must be >= 0")
	}
	return nil
}

// BuildLevels generates the grid price levels from LowerPrice to UpperPrice
// (inclusive) using equal-ratio spacing. Returns at least 2 levels.
func (c *Config) BuildLevels() []float64 {
	levels := []float64{c.LowerPrice}
	p := c.LowerPrice
	for {
		next := p * (1 + c.GridSpacingPct)
		if next >= c.UpperPrice {
			break
		}
		levels = append(levels, next)
		p = next
	}
	levels = append(levels, c.UpperPrice)
	return levels
}
