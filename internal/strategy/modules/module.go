package modules

import (
	"github.com/jayce/btc-trader/internal/strategy"
)

// ScoringModule is the interface for a pluggable indicator scoring module.
// Each module computes a score from -1.0 (strong sell) to +1.0 (strong buy).
type ScoringModule interface {
	// Name returns the unique identifier (e.g. "rsi", "macd").
	Name() string
	// Category returns the group: "trend", "momentum", "money_flow", "volume".
	Category() string
	// Label returns the display name (Chinese).
	Label() string
	// Description returns a short description.
	Description() string
	// DefaultWeight returns the suggested weight for this module.
	DefaultWeight() float64
	// ParamSchema returns configurable parameters metadata for frontend rendering.
	ParamSchema() []ParamSchema
	// Init initializes the module with user-provided parameters.
	Init(params map[string]interface{})
	// RequiredIndicators returns which indicators this module needs.
	RequiredIndicators() []strategy.IndicatorRequirement
	// RequiredHistory returns the minimum kline count needed.
	RequiredHistory() int
	// Score computes a score from -1.0 to +1.0 given the market snapshot.
	Score(snap *strategy.MarketSnapshot) float64
}

// ParamSchema describes a configurable parameter for frontend rendering.
type ParamSchema struct {
	Key     string  `json:"key"`
	Label   string  `json:"label"`
	Type    string  `json:"type"` // "int", "float", "bool", "string"
	Default any     `json:"default"`
	Min     float64 `json:"min"`
	Max     float64 `json:"max"`
	Step    float64 `json:"step"`
	Group   string  `json:"group,omitempty"` // UI group: "signal", "position", "stoploss", "trend"
	Desc    string  `json:"desc,omitempty"`  // Help text shown as tooltip
}

// ModuleMeta is the API response for a single module's metadata.
type ModuleMeta struct {
	Name          string        `json:"name"`
	Label         string        `json:"label"`
	Category      string        `json:"category"`
	Description   string        `json:"description"`
	DefaultWeight float64       `json:"default_weight"`
	Params        []ParamSchema `json:"params"`
}

// Meta returns the ModuleMeta for a given module.
func Meta(m ScoringModule) ModuleMeta {
	return ModuleMeta{
		Name:          m.Name(),
		Label:         m.Label(),
		Category:      m.Category(),
		Description:   m.Description(),
		DefaultWeight: m.DefaultWeight(),
		Params:        m.ParamSchema(),
	}
}
