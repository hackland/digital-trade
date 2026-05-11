package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jayce/btc-trader/internal/override"
	"go.uber.org/zap"
)

// ListOverrides GET /api/v1/overrides
// 返回所有干预记录（active + 历史），前端用于显示当前设置和触发日志。
func (h *Handler) ListOverrides(c *gin.Context) {
	if h.deps.Override == nil {
		ok(c, []*override.ConditionalOverride{})
		return
	}
	ok(c, h.deps.Override.List())
}

// CreateOverride POST /api/v1/overrides
// 创建条件触发干预。同 symbol 只允许一个 active，新建时自动取消旧的。
func (h *Handler) CreateOverride(c *gin.Context) {
	var req override.CreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errResp(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	if h.deps.Override == nil {
		errResp(c, http.StatusServiceUnavailable, "override manager not initialized")
		return
	}

	ov, err := h.deps.Override.Create(&req)
	if err != nil {
		errResp(c, http.StatusBadRequest, err.Error())
		return
	}

	h.logger.Info("override created via API",
		zap.String("id", ov.ID),
		zap.String("symbol", ov.Symbol),
		zap.Float64("trigger_price", ov.TriggerPrice),
		zap.String("direction", string(ov.Direction)),
	)
	c.JSON(http.StatusCreated, response{Code: 0, Message: "ok", Data: ov})
}

// CancelOverride DELETE /api/v1/overrides/:id
// 取消一条 active 的干预。
func (h *Handler) CancelOverride(c *gin.Context) {
	id := c.Param("id")
	if h.deps.Override == nil {
		errResp(c, http.StatusServiceUnavailable, "override manager not initialized")
		return
	}

	if err := h.deps.Override.Cancel(id); err != nil {
		errResp(c, http.StatusBadRequest, err.Error())
		return
	}

	h.logger.Info("override cancelled via API", zap.String("id", id))
	ok(c, gin.H{"id": id, "status": "cancelled"})
}

// ForceClosePosition POST /api/v1/positions/:symbol/force-close
// 立即市价平仓（无需等待价格触发），适合用户手动干预。
func (h *Handler) ForceClosePosition(c *gin.Context) {
	symbol := c.Param("symbol")

	var body struct {
		Note string `json:"note"`
	}
	_ = c.ShouldBindJSON(&body)
	if body.Note == "" {
		body.Note = "手动平仓"
	}

	// 超时设置为 60s：ForceClose 会轮询余额最长 15s 等取消挂单生效，
	// 加上 3 次下单重试 (≤3s)，最坏情况 ~20s，留足缓冲。
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	if err := h.deps.Order.ForceClose(ctx, symbol, body.Note); err != nil {
		h.logger.Error("force close via API failed",
			zap.String("symbol", symbol), zap.Error(err))
		errResp(c, http.StatusInternalServerError, err.Error())
		return
	}

	h.logger.Info("force close executed via API",
		zap.String("symbol", symbol),
		zap.String("note", body.Note),
	)
	ok(c, gin.H{"symbol": symbol, "action": "force_close", "note": body.Note})
}

// PauseStrategy POST /api/v1/strategy/pause
// 立即暂停策略（不平仓），hours 默认 24。
func (h *Handler) PauseStrategy(c *gin.Context) {
	var body struct {
		Hours  int    `json:"hours"`
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&body)
	if body.Hours <= 0 {
		body.Hours = 24
	}
	if body.Reason == "" {
		body.Reason = "手动暂停"
	}

	h.deps.Risk.PauseWithDuration(body.Reason, time.Duration(body.Hours)*time.Hour)

	h.logger.Info("strategy paused via API",
		zap.Int("hours", body.Hours),
		zap.String("reason", body.Reason),
	)
	ok(c, gin.H{"paused": true, "hours": body.Hours, "reason": body.Reason})
}

// ResumeStrategy POST /api/v1/strategy/resume
// 立即恢复策略（提前结束暂停）。
func (h *Handler) ResumeStrategy(c *gin.Context) {
	h.deps.Risk.ResumeTrade()
	h.logger.Info("strategy resumed via API")
	ok(c, gin.H{"paused": false})
}
