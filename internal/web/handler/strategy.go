package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jayce/btc-trader/internal/strategy/modules"
	"github.com/jayce/btc-trader/internal/strategy/trend"
	"go.uber.org/zap"
)

type strategyResponse struct {
	Name      string `json:"name"`
	Interval  string `json:"interval"`
	RSIFilter bool   `json:"rsi_filter"`
}

// GetStrategyStatus returns the current strategy configuration and state.
// If strategy supports GetConfig(), returns live running config (reflects deploy changes).
func (h *Handler) GetStrategyStatus(c *gin.Context) {
	strat := h.deps.Strategy

	// Prefer live config from strategy instance (reflects hot-reload)
	var liveConfig map[string]interface{}
	if cw, ok := strat.(*trend.CustomWeightedStrategy); ok {
		liveConfig = cw.GetConfig()
	} else {
		liveConfig = h.deps.Config.Strategy.Config
	}

	ok(c, gin.H{
		"name":                strat.Name(),
		"required_history":    strat.RequiredHistory(),
		"required_indicators": strat.RequiredIndicators(),
		"config":              liveConfig,
	})
}

// GetIndicatorModules returns all available scoring modules with metadata.
// Frontend uses this to render the indicator selection + weight configuration UI.
func (h *Handler) GetIndicatorModules(c *gin.Context) {
	metas := modules.AllMeta()

	// Group by category for frontend convenience
	grouped := make(map[string][]modules.ModuleMeta)
	for _, m := range metas {
		grouped[m.Category] = append(grouped[m.Category], m)
	}

	ok(c, gin.H{
		"modules": metas,
		"grouped": grouped,
		"signal_params": []modules.ParamSchema{
			// 买卖信号
			{Key: "buy_threshold", Label: "买入阈值", Type: "float", Default: 0.20, Min: 0.05, Max: 0.8, Step: 0.05, Group: "signal", Desc: "综合评分超过此值才触发买入，越高越严格"},
			{Key: "sell_threshold", Label: "卖出阈值", Type: "float", Default: -0.30, Min: -1.0, Max: -0.1, Step: 0.05, Group: "signal", Desc: "综合评分低于此值触发卖出，越低越宽松"},
			{Key: "confirm_bars", Label: "确认K线数", Type: "int", Default: 1, Min: 1, Max: 5, Step: 1, Group: "signal", Desc: "连续N根K线评分达标才买入，防止假信号"},
			// 持仓控制
			{Key: "cooldown_bars", Label: "冷却期", Type: "int", Default: 12, Min: 0, Max: 48, Step: 1, Group: "position", Desc: "卖出后等待N根K线才允许再次买入"},
			{Key: "min_hold_bars", Label: "最短持仓", Type: "int", Default: 6, Min: 0, Max: 30, Step: 1, Group: "position", Desc: "买入后至少持有N根K线，避免频繁交易"},
			// 止损
			{Key: "atr_stop_mult", Label: "ATR止损倍数", Type: "float", Default: 3.0, Min: 1.0, Max: 6.0, Step: 0.1, Group: "stoploss", Desc: "追踪止损距离 = ATR × 倍数，越大越宽松"},
			// 趋势过滤
			{Key: "trend_filter", Label: "EMA趋势过滤", Type: "bool", Default: false, Min: 0, Max: 1, Step: 1, Group: "trend", Desc: "开启后只在价格高于EMA均线时买入，过滤下跌趋势"},
			{Key: "trend_period", Label: "EMA周期", Type: "int", Default: 50, Min: 20, Max: 200, Step: 5, Group: "trend", Desc: "趋势判断用的均线周期，50表示看50根K线趋势"},
			{Key: "htf_enabled", Label: "大周期过滤", Type: "bool", Default: true, Min: 0, Max: 1, Step: 1, Group: "trend", Desc: "开启后用更大时间周期确认趋势方向"},
			{Key: "htf_interval", Label: "大周期", Type: "string", Default: "1d", Group: "trend", Desc: "用于趋势确认的大时间框架"},
			{Key: "htf_period", Label: "大周期EMA", Type: "int", Default: 10, Min: 5, Max: 100, Step: 5, Group: "trend", Desc: "大周期上的EMA均线周期"},
			// 做空参数
			{Key: "short_enabled", Label: "启用做空信号", Type: "bool", Default: false, Min: 0, Max: 1, Step: 1, Group: "short", Desc: "开启后生成做空提醒（仅通知，不自动交易）"},
			{Key: "short_threshold", Label: "做空阈值", Type: "float", Default: -0.25, Min: -0.8, Max: -0.05, Step: 0.05, Group: "short", Desc: "综合评分低于此值触发做空信号"},
			{Key: "cover_threshold", Label: "平空阈值", Type: "float", Default: 0.15, Min: 0.0, Max: 0.5, Step: 0.05, Group: "short", Desc: "综合评分高于此值触发平空信号"},
			{Key: "short_confirm_bars", Label: "做空确认K线", Type: "int", Default: 1, Min: 1, Max: 5, Step: 1, Group: "short", Desc: "连续N根K线评分达标才做空"},
			{Key: "short_min_hold_bars", Label: "做空最短持仓", Type: "int", Default: 12, Min: 0, Max: 60, Step: 1, Group: "short", Desc: "做空后至少持有N根K线"},
			{Key: "short_atr_stop_mult", Label: "做空ATR止损", Type: "float", Default: 3.0, Min: 1.0, Max: 8.0, Step: 0.1, Group: "short", Desc: "做空追踪止损距离 = ATR × 倍数"},
			{Key: "short_cooldown_bars", Label: "做空冷却期", Type: "int", Default: 4, Min: 0, Max: 48, Step: 1, Group: "short", Desc: "平空后等待N根K线才允许再次做空"},
		},
		"signal_presets": map[string]map[string]any{
			"conservative": {
				"label":          "保守",
				"desc":           "低频交易，高确认，严格过滤",
				"buy_threshold":  0.30,
				"sell_threshold": -0.30,
				"confirm_bars":   2,
				"cooldown_bars":  24,
				"min_hold_bars":  30,
				"atr_stop_mult":  4.5,
				"trend_filter":   false,
				"trend_period":   50,
				"htf_enabled":    true,
				"htf_interval":   "1d",
				"htf_period":     10,
			},
			"standard": {
				"label":          "标准",
				"desc":           "推荐: EMA+MACD+MFI, 日线过滤",
				"buy_threshold":  0.20,
				"sell_threshold": -0.30,
				"confirm_bars":   1,
				"cooldown_bars":  12,
				"min_hold_bars":  24,
				"atr_stop_mult":  4.5,
				"trend_filter":   false,
				"trend_period":   50,
				"htf_enabled":    true,
				"htf_interval":   "1d",
				"htf_period":     10,
			},
			"aggressive": {
				"label":          "激进",
				"desc":           "低阈值、快进快出，交易频率高",
				"buy_threshold":  0.10,
				"sell_threshold": -0.15,
				"confirm_bars":   1,
				"cooldown_bars":  2,
				"min_hold_bars":  6,
				"atr_stop_mult":  2.5,
				"trend_filter":   false,
				"trend_period":   30,
				"htf_enabled":    true,
				"htf_interval":   "1d",
				"htf_period":     10,
			},
		},
	})
}

// GetStrategyDiagnostics returns the latest evaluation diagnostics (why no signal).
// GET /api/v1/strategy/diagnostics
func (h *Handler) GetStrategyDiagnostics(c *gin.Context) {
	cw, isCW := h.deps.Strategy.(*trend.CustomWeightedStrategy)
	if !isCW {
		c.JSON(http.StatusBadRequest, gin.H{"error": "strategy does not support diagnostics"})
		return
	}

	diag := cw.GetDiagnostics()
	if diag == nil {
		ok(c, gin.H{
			"message": "策略尚未执行过评估，等待下一根K线收线...",
		})
		return
	}

	ok(c, diag)
}

// DeployStrategy hot-reloads the running strategy with new configuration.
// POST /api/v1/strategy/deploy
func (h *Handler) DeployStrategy(c *gin.Context) {
	var req struct {
		Modules []struct {
			Name   string  `json:"name"`
			Weight float64 `json:"weight"`
		} `json:"modules"`
		SignalParams map[string]interface{} `json:"signal_params"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Build config map matching strategy.Init() format
	cfg := make(map[string]interface{})

	// Modules
	modList := make([]interface{}, 0, len(req.Modules))
	for _, m := range req.Modules {
		modList = append(modList, map[string]interface{}{
			"name":   m.Name,
			"weight": m.Weight,
		})
	}
	cfg["modules"] = modList

	// Signal params (buy_threshold, sell_threshold, cooldown_bars, etc.)
	for k, v := range req.SignalParams {
		cfg[k] = v
	}

	// Type-assert to CustomWeightedStrategy for Reconfigure
	cw, isCW := h.deps.Strategy.(*trend.CustomWeightedStrategy)
	if !isCW {
		c.JSON(http.StatusBadRequest, gin.H{"error": "strategy does not support hot-reload"})
		return
	}

	if err := cw.Reconfigure(cfg); err != nil {
		h.logger.Error("strategy deploy failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Return new running config as confirmation
	h.logger.Info("strategy deployed via API")
	ok(c, gin.H{
		"message": "策略已部署",
		"config":  cw.GetConfig(),
	})
}
