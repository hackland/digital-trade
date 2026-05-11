package override

import "time"

// TriggerDir 触发方向
type TriggerDir string

const (
	// Above 价格 >= 触发价时触发（适合"涨到目标价后平仓"）
	Above TriggerDir = "above"
	// Below 价格 <= 触发价时触发（适合"跌破止损线后平仓"）
	Below TriggerDir = "below"
)

// Action 触发后执行的操作
type Action string

const (
	// ActionForceClose 市价平仓全部持仓
	ActionForceClose Action = "force_close"
	// ActionPauseStrategy 暂停策略开新仓
	ActionPauseStrategy Action = "pause_strategy"
)

// Status 条件干预状态
type Status string

const (
	StatusActive    Status = "active"    // 等待触发
	StatusTriggered Status = "triggered" // 已触发执行
	StatusCancelled Status = "cancelled" // 用户手动取消
)

// ConditionalOverride 条件触发干预
type ConditionalOverride struct {
	ID           string     `json:"id"`
	Symbol       string     `json:"symbol"`
	TriggerPrice float64    `json:"trigger_price"`
	Direction    TriggerDir `json:"direction"`
	Actions      []Action   `json:"actions"`
	// PauseHours 暂停策略的小时数（ActionPauseStrategy 时生效，0 表示用默认 24h）
	PauseHours  int        `json:"pause_hours,omitempty"`
	Note        string     `json:"note,omitempty"`
	Status      Status     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	TriggeredAt *time.Time `json:"triggered_at,omitempty"`
	// TriggerPrice 触发时的实际价格（事后复盘用）
	ActualPrice float64 `json:"actual_price,omitempty"`
}

// CreateRequest 创建干预的请求体
type CreateRequest struct {
	Symbol       string     `json:"symbol" binding:"required"`
	TriggerPrice float64    `json:"trigger_price" binding:"required,gt=0"`
	Direction    TriggerDir `json:"direction" binding:"required,oneof=above below"`
	Actions      []Action   `json:"actions" binding:"required,min=1"`
	PauseHours   int        `json:"pause_hours"`
	Note         string     `json:"note"`
}
