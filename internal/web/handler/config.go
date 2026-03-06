package handler

import "github.com/gin-gonic/gin"

// GetConfig returns the sanitized system configuration.
func (h *Handler) GetConfig(c *gin.Context) {
	cfg := h.deps.Config

	// Mask sensitive fields
	apiKey := "****"
	if len(cfg.Exchange.APIKey) > 4 {
		apiKey = "****" + cfg.Exchange.APIKey[len(cfg.Exchange.APIKey)-4:]
	}

	ok(c, gin.H{
		"app": gin.H{
			"name":      cfg.App.Name,
			"mode":      cfg.App.Mode,
			"log_level": cfg.App.LogLevel,
			"testnet":   cfg.App.Testnet,
		},
		"exchange": gin.H{
			"name":            cfg.Exchange.Name,
			"market_type":     cfg.Exchange.MarketType,
			"symbols":         cfg.Exchange.Symbols,
			"kline_intervals": cfg.Exchange.KlineIntervals,
			"api_key":         apiKey,
		},
		"strategy": gin.H{
			"name":   cfg.Strategy.Name,
			"config": cfg.Strategy.Config,
		},
		"risk": cfg.Risk,
		"dashboard": gin.H{
			"enabled": cfg.Dashboard.Enabled,
			"addr":    cfg.Dashboard.Addr,
		},
		"snapshot": gin.H{
			"interval": cfg.Snapshot.Interval.String(),
		},
	})
}
