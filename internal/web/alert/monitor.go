// Package alert provides a background monitor that periodically evaluates
// open positions and pushes WebSocket alerts when risk is elevated.
package alert

import (
	"context"
	"sync"
	"time"

	"github.com/jayce/btc-trader/internal/market"
	"github.com/jayce/btc-trader/internal/position"
	"github.com/jayce/btc-trader/internal/strategy/trend"
	"github.com/jayce/btc-trader/internal/web/handler"
	"github.com/jayce/btc-trader/internal/web/ws"
	"go.uber.org/zap"
)

// Monitor periodically checks open positions and emits position_alert WS messages.
type Monitor struct {
	deps     *handler.Deps
	hub      *ws.Hub
	symbols  []string
	interval time.Duration
	logger   *zap.Logger

	mu       sync.Mutex
	cooldown map[string]time.Time // last alert time per symbol
}

// NewMonitor creates a Monitor. interval is how often to run (e.g. 5 * time.Minute).
func NewMonitor(deps *handler.Deps, hub *ws.Hub, symbols []string, interval time.Duration, logger *zap.Logger) *Monitor {
	return &Monitor{
		deps:     deps,
		hub:      hub,
		symbols:  symbols,
		interval: interval,
		logger:   logger,
		cooldown: make(map[string]time.Time),
	}
}

// Run starts the monitor loop; blocks until ctx is cancelled.
func (m *Monitor) Run(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	m.logger.Info("position alert monitor started",
		zap.Strings("symbols", m.symbols),
		zap.Duration("interval", m.interval),
	)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.checkAll(ctx)
		}
	}
}

func (m *Monitor) checkAll(ctx context.Context) {
	for _, symbol := range m.symbols {
		pos := m.deps.Position.GetPosition(symbol)
		if pos == nil || pos.Quantity <= 0 {
			continue
		}
		m.checkPosition(ctx, pos)
	}
}

func (m *Monitor) checkPosition(ctx context.Context, pos *position.Position) {
	symbol := pos.Symbol

	// ── Build analysis ────────────────────────────────────────────────────────
	ticker, err := m.deps.Exchange.GetTicker(ctx, symbol)
	if err != nil {
		return
	}
	currentPrice := ticker.LastPrice

	// Strategy diagnostics
	var compositeScore, sellThreshold, stopPrice float64
	var barsSinceEntry int
	var holdReason string
	var htfBullish, htfBlocked bool
	if cw, ok := m.deps.Strategy.(*trend.CustomWeightedStrategy); ok {
		if diag := cw.GetDiagnostics(); diag != nil {
			compositeScore = diag.CompositeScore
			sellThreshold = diag.SellThreshold
			stopPrice = diag.StopPrice
			barsSinceEntry = diag.BarsSinceEntry
			holdReason = diag.HoldReason
			htfBullish = diag.HTFBullish
			htfBlocked = diag.HTFBlocked
		}
	}

	// Daily EMA
	ic := market.NewIndicatorComputer()
	dailyStart := time.Now().Add(-210 * 24 * time.Hour)
	dailyKlines, _ := m.deps.Store.GetKlines(ctx, symbol, "1d", dailyStart, time.Now(), 210)
	var ema50, ema200 float64
	if len(dailyKlines) >= 50 {
		closes := make([]float64, len(dailyKlines))
		for i, k := range dailyKlines {
			closes[i] = k.Close
		}
		ema50 = ic.ComputeEMA(closes, 50)
		ema200 = ic.ComputeEMA(closes, 200)
	}

	regime, regimeLabel := computeRegime(currentPrice, ema200)

	analysis := handler.BuildAnalysis(handler.PositionAnalysis{
		Symbol:         symbol,
		EntryPrice:     pos.AvgEntryPrice,
		CurrentPrice:   currentPrice,
		Quantity:       pos.Quantity,
		StopPrice:      stopPrice,
		DailyEMA50:     ema50,
		DailyEMA200:    ema200,
		CompositeScore: compositeScore,
		SellThreshold:  sellThreshold,
		BarsSinceEntry: barsSinceEntry,
		HoldReason:     holdReason,
		Regime:         regime,
		RegimeLabel:    regimeLabel,
		HTFBullish:     htfBullish,
		HTFBlocked:     htfBlocked,
	})

	// ── Only alert on high/critical ───────────────────────────────────────────
	if analysis.RiskLevel != "high" && analysis.RiskLevel != "critical" {
		return
	}

	// ── Cooldown: 30 min between alerts per symbol ────────────────────────────
	const cooldownDur = 30 * time.Minute
	m.mu.Lock()
	last, exists := m.cooldown[symbol]
	if exists && time.Since(last) < cooldownDur {
		m.mu.Unlock()
		return
	}
	m.cooldown[symbol] = time.Now()
	m.mu.Unlock()

	m.logger.Info("position alert triggered",
		zap.String("symbol", symbol),
		zap.String("risk_level", analysis.RiskLevel),
		zap.String("recommendation", analysis.Recommendation),
	)

	m.hub.BroadcastToChannel("position_alert", &ws.Message{
		Type: "position_alert",
		Data: analysis,
	})
}

func computeRegime(price, ema200 float64) (string, string) {
	if ema200 == 0 {
		return "unknown", "未知"
	}
	if price > ema200 {
		return "strong_bull", "强牛市"
	}
	return "mid_bear", "中期熊市"
}
