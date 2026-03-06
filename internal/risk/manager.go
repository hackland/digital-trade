package risk

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jayce/btc-trader/internal/config"
	"github.com/jayce/btc-trader/internal/eventbus"
	"github.com/jayce/btc-trader/internal/exchange"
	"github.com/jayce/btc-trader/internal/strategy"
	"go.uber.org/zap"
)

// Decision represents the outcome of a risk check.
type Decision struct {
	Allowed bool
	Reason  string
}

// Status represents the current risk state.
type Status struct {
	DailyPnL        float64   `json:"daily_pnl"`
	DailyPnLPct     float64   `json:"daily_pnl_pct"`
	CurrentDrawdown float64   `json:"current_drawdown"`
	MaxDrawdown     float64   `json:"max_drawdown"`
	PeakEquity      float64   `json:"peak_equity"`
	CurrentEquity   float64   `json:"current_equity"`
	DailyTradeCount int       `json:"daily_trade_count"`
	IsTradingPaused bool      `json:"is_trading_paused"`
	PauseReason     string    `json:"pause_reason,omitempty"`
	PauseUntil      time.Time `json:"pause_until,omitempty"`
}

// Manager orchestrates pre-trade and post-trade risk checks.
type Manager struct {
	cfg    config.RiskConfig
	bus    *eventbus.Bus
	logger *zap.Logger

	mu              sync.RWMutex
	dailyPnL        float64
	dailyTradeCount int
	peakEquity      float64
	currentEquity   float64
	lastOrderTime   time.Time
	isPaused        bool
	pauseReason     string
	pauseUntil      time.Time
	dayStart        time.Time
}

// NewManager creates a new risk manager.
func NewManager(cfg config.RiskConfig, bus *eventbus.Bus, logger *zap.Logger) *Manager {
	now := time.Now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return &Manager{
		cfg:      cfg,
		bus:      bus,
		logger:   logger,
		dayStart: dayStart,
	}
}

// SetEquity sets the initial equity for risk calculations.
func (m *Manager) SetEquity(equity float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentEquity = equity
	if m.peakEquity == 0 {
		m.peakEquity = equity
	}
}

// PreTradeCheck validates whether a proposed order is allowed.
func (m *Manager) PreTradeCheck(ctx context.Context, req *exchange.OrderRequest, sig *strategy.Signal) (*Decision, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Check if trading is paused
	if m.isPaused {
		if time.Now().Before(m.pauseUntil) {
			return &Decision{Allowed: false, Reason: fmt.Sprintf("trading paused: %s", m.pauseReason)}, nil
		}
		// Auto-resume after cooldown
	}

	// Check daily loss limit
	if m.cfg.MaxDailyLossUSDT > 0 && m.dailyPnL < -m.cfg.MaxDailyLossUSDT {
		return &Decision{Allowed: false, Reason: fmt.Sprintf("daily loss limit reached: %.2f USDT", m.dailyPnL)}, nil
	}

	// Check daily loss percentage
	if m.cfg.MaxDailyLossPct > 0 && m.currentEquity > 0 {
		lossPct := -m.dailyPnL / m.currentEquity
		if lossPct > m.cfg.MaxDailyLossPct {
			return &Decision{Allowed: false, Reason: fmt.Sprintf("daily loss pct limit: %.2f%%", lossPct*100)}, nil
		}
	}

	// Check daily trade count
	if m.cfg.MaxDailyTrades > 0 && m.dailyTradeCount >= m.cfg.MaxDailyTrades {
		return &Decision{Allowed: false, Reason: "daily trade count limit reached"}, nil
	}

	// Check drawdown
	if m.cfg.MaxDrawdownPct > 0 && m.peakEquity > 0 {
		drawdown := (m.peakEquity - m.currentEquity) / m.peakEquity
		if drawdown > m.cfg.MaxDrawdownPct {
			return &Decision{Allowed: false, Reason: fmt.Sprintf("max drawdown %.2f%% exceeded", drawdown*100)}, nil
		}
	}

	// Check minimum time between orders
	if m.cfg.MinTimeBetweenOrders > 0 && !m.lastOrderTime.IsZero() {
		if time.Since(m.lastOrderTime) < m.cfg.MinTimeBetweenOrders {
			return &Decision{Allowed: false, Reason: "too soon since last order"}, nil
		}
	}

	// Check minimum order size
	orderValue := req.Quantity * req.Price
	if req.Type == exchange.OrderTypeMarket {
		// For market orders, we don't know the exact price, skip value check
		orderValue = 0
	}
	if m.cfg.MinOrderSizeUSDT > 0 && orderValue > 0 && orderValue < m.cfg.MinOrderSizeUSDT {
		return &Decision{Allowed: false, Reason: fmt.Sprintf("order value %.2f below minimum %.2f USDT", orderValue, m.cfg.MinOrderSizeUSDT)}, nil
	}

	return &Decision{Allowed: true}, nil
}

// PostTradeCheck runs after a trade executes.
func (m *Manager) PostTradeCheck(ctx context.Context, trade *exchange.Trade) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.dailyTradeCount++
	m.lastOrderTime = time.Now()

	// Update PnL tracking would happen here based on position manager data

	return nil
}

// ContinuousMonitor runs in a loop, checking for breached thresholds.
func (m *Manager) ContinuousMonitor(ctx context.Context) error {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			m.checkAndReset()
			m.checkDrawdown()
		}
	}
}

// PauseTrade pauses all trading with a reason.
func (m *Manager) PauseTrade(reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.isPaused = true
	m.pauseReason = reason
	if m.cfg.DrawdownCooldownMins > 0 {
		m.pauseUntil = time.Now().Add(time.Duration(m.cfg.DrawdownCooldownMins) * time.Minute)
	}
	m.logger.Warn("trading paused", zap.String("reason", reason))
	m.bus.Publish(eventbus.Event{
		Type:      eventbus.EventRiskAlert,
		Timestamp: time.Now(),
		Payload: eventbus.RiskAlertEvent{
			Rule:    "pause",
			Message: reason,
			Level:   "critical",
		},
	})
}

// ResumeTrade resumes trading.
func (m *Manager) ResumeTrade() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.isPaused = false
	m.pauseReason = ""
	m.logger.Info("trading resumed")
}

// GetStatus returns the current risk state.
func (m *Manager) GetStatus() *Status {
	m.mu.RLock()
	defer m.mu.RUnlock()

	drawdown := 0.0
	if m.peakEquity > 0 {
		drawdown = (m.peakEquity - m.currentEquity) / m.peakEquity
	}

	return &Status{
		DailyPnL:        m.dailyPnL,
		CurrentDrawdown: drawdown,
		PeakEquity:      m.peakEquity,
		CurrentEquity:   m.currentEquity,
		DailyTradeCount: m.dailyTradeCount,
		IsTradingPaused: m.isPaused,
		PauseReason:     m.pauseReason,
		PauseUntil:      m.pauseUntil,
	}
}

// checkAndReset resets daily counters at midnight UTC.
func (m *Manager) checkAndReset() {
	now := time.Now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	m.mu.Lock()
	defer m.mu.Unlock()

	if dayStart.After(m.dayStart) {
		m.logger.Info("resetting daily risk counters")
		m.dailyPnL = 0
		m.dailyTradeCount = 0
		m.dayStart = dayStart
	}
}

func (m *Manager) checkDrawdown() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.peakEquity > 0 && m.cfg.MaxDrawdownPct > 0 {
		drawdown := (m.peakEquity - m.currentEquity) / m.peakEquity
		if drawdown > m.cfg.MaxDrawdownPct && !m.isPaused {
			go m.PauseTrade(fmt.Sprintf("max drawdown %.2f%% exceeded", drawdown*100))
		}
	}
}
