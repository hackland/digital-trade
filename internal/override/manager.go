// Package override implements conditional manual interventions.
// When price hits a user-defined trigger, it executes a chain of actions
// (force-close position, pause strategy, etc.) without modifying strategy logic.
package override

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ForceCloser 执行强制平仓（由 order.Manager 实现）
type ForceCloser interface {
	ForceClose(ctx context.Context, symbol, note string) error
}

// TradePauser 暂停/恢复交易（由 risk.Manager 实现）
type TradePauser interface {
	PauseWithDuration(reason string, d time.Duration)
	ResumeTrade()
}

// Manager 管理所有条件触发干预，线程安全。
// 在 order.Manager.Run() 的 kline 事件循环里调用 OnPriceUpdate()，
// 检查是否有 active 的干预条件被满足。
type Manager struct {
	mu        sync.RWMutex
	overrides map[string]*ConditionalOverride // id -> override

	forceCloser ForceCloser
	pauser      TradePauser
	logger      *zap.Logger
}

// New 创建 override.Manager
func New(fc ForceCloser, pauser TradePauser, logger *zap.Logger) *Manager {
	return &Manager{
		overrides:   make(map[string]*ConditionalOverride),
		forceCloser: fc,
		pauser:      pauser,
		logger:      logger,
	}
}

// Create 创建一条条件干预（同 symbol 若已有 active 记录会先取消旧的）
func (m *Manager) Create(req *CreateRequest) (*ConditionalOverride, error) {
	if len(req.Actions) == 0 {
		return nil, fmt.Errorf("actions must not be empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// 同 symbol 只允许一个 active override，自动取消旧的
	for _, ov := range m.overrides {
		if ov.Symbol == req.Symbol && ov.Status == StatusActive {
			ov.Status = StatusCancelled
			m.logger.Info("override auto-cancelled (replaced by new)",
				zap.String("id", ov.ID),
				zap.String("symbol", req.Symbol),
			)
		}
	}

	hours := req.PauseHours
	if hours <= 0 {
		hours = 24
	}

	ov := &ConditionalOverride{
		ID:           uuid.New().String(),
		Symbol:       req.Symbol,
		TriggerPrice: req.TriggerPrice,
		Direction:    req.Direction,
		Actions:      req.Actions,
		PauseHours:   hours,
		Note:         req.Note,
		Status:       StatusActive,
		CreatedAt:    time.Now(),
	}
	m.overrides[ov.ID] = ov

	m.logger.Info("conditional override created",
		zap.String("id", ov.ID),
		zap.String("symbol", ov.Symbol),
		zap.Float64("trigger_price", ov.TriggerPrice),
		zap.String("direction", string(ov.Direction)),
		zap.Any("actions", ov.Actions),
	)

	return ov, nil
}

// Cancel 取消一条 active 的干预
func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ov, ok := m.overrides[id]
	if !ok {
		return fmt.Errorf("override %s not found", id)
	}
	if ov.Status != StatusActive {
		return fmt.Errorf("override %s is not active (status: %s)", id, ov.Status)
	}
	ov.Status = StatusCancelled
	m.logger.Info("override cancelled", zap.String("id", id))
	return nil
}

// List 返回所有干预记录（快照），最近触发/取消的保留用于复盘
func (m *Manager) List() []*ConditionalOverride {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*ConditionalOverride, 0, len(m.overrides))
	for _, ov := range m.overrides {
		cp := *ov
		result = append(result, &cp)
	}
	return result
}

// OnPriceUpdate 由 order.Manager 的 kline 循环调用，检查触发条件。
// 已触发或取消的 override 会被忽略。
func (m *Manager) OnPriceUpdate(symbol string, price float64) {
	// 快速路径：先加读锁扫描是否有需要触发的
	m.mu.RLock()
	var toTrigger []*ConditionalOverride
	for _, ov := range m.overrides {
		if ov.Status != StatusActive || ov.Symbol != symbol {
			continue
		}
		if isTriggered(ov, price) {
			cp := *ov
			toTrigger = append(toTrigger, &cp)
		}
	}
	m.mu.RUnlock()

	if len(toTrigger) == 0 {
		return
	}

	// 升级为写锁，标记状态后执行动作
	m.mu.Lock()
	now := time.Now()
	for _, cp := range toTrigger {
		ov, ok := m.overrides[cp.ID]
		if !ok || ov.Status != StatusActive {
			continue // 并发触发保护
		}
		ov.Status = StatusTriggered
		ov.TriggeredAt = &now
		ov.ActualPrice = price
	}
	m.mu.Unlock()

	// 在锁外执行实际动作（可能耗时）
	for _, cp := range toTrigger {
		m.execute(cp, price)
	}
}

// isTriggered 判断价格是否满足触发条件
func isTriggered(ov *ConditionalOverride, price float64) bool {
	switch ov.Direction {
	case Above:
		return price >= ov.TriggerPrice
	case Below:
		return price <= ov.TriggerPrice
	}
	return false
}

// execute 执行干预动作链
func (m *Manager) execute(ov *ConditionalOverride, price float64) {
	m.logger.Info("conditional override triggered",
		zap.String("id", ov.ID),
		zap.String("symbol", ov.Symbol),
		zap.Float64("trigger_price", ov.TriggerPrice),
		zap.Float64("actual_price", price),
		zap.String("direction", string(ov.Direction)),
		zap.Any("actions", ov.Actions),
		zap.String("note", ov.Note),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, action := range ov.Actions {
		switch action {
		case ActionForceClose:
			note := fmt.Sprintf("条件触发平仓 @%.2f (设定=%.2f %s)", price, ov.TriggerPrice, ov.Direction)
			if ov.Note != "" {
				note += "，备注: " + ov.Note
			}
			if err := m.forceCloser.ForceClose(ctx, ov.Symbol, note); err != nil {
				m.logger.Error("conditional override: force close failed",
					zap.String("id", ov.ID),
					zap.Error(err),
				)
			} else {
				m.logger.Info("conditional override: force close executed",
					zap.String("symbol", ov.Symbol),
					zap.Float64("price", price),
				)
			}

		case ActionPauseStrategy:
			hours := ov.PauseHours
			if hours <= 0 {
				hours = 24
			}
			reason := fmt.Sprintf("条件干预触发 @%.2f (设定=%.2f %s)", price, ov.TriggerPrice, ov.Direction)
			if ov.Note != "" {
				reason += "，备注: " + ov.Note
			}
			m.pauser.PauseWithDuration(reason, time.Duration(hours)*time.Hour)
			m.logger.Info("conditional override: strategy paused",
				zap.String("symbol", ov.Symbol),
				zap.Int("hours", hours),
			)
		}
	}
}
