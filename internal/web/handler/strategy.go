package handler

import "github.com/gin-gonic/gin"

type strategyResponse struct {
	Name      string `json:"name"`
	Interval  string `json:"interval"`
	RSIFilter bool   `json:"rsi_filter"`
}

// GetStrategyStatus returns the current strategy configuration and state.
func (h *Handler) GetStrategyStatus(c *gin.Context) {
	strat := h.deps.Strategy
	cfg := h.deps.Config.Strategy

	interval := "5m"
	rsiFilter := false
	if v, exists := cfg.Config["interval"]; exists {
		if s, valid := v.(string); valid {
			interval = s
		}
	}
	if v, exists := cfg.Config["rsi_filter"]; exists {
		if b, valid := v.(bool); valid {
			rsiFilter = b
		}
	}

	ok(c, gin.H{
		"name":                strat.Name(),
		"interval":            interval,
		"rsi_filter":          rsiFilter,
		"required_history":    strat.RequiredHistory(),
		"required_indicators": strat.RequiredIndicators(),
		"config":              cfg.Config,
	})
}
