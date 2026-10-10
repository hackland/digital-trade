package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jayce/btc-trader/internal/mtfmacd"
)

// GetMTFMACDStatus returns the live 4h/1h/15m state and the current evaluation.
func (h *Handler) GetMTFMACDStatus(c *gin.Context) {
	w := h.deps.MTFMACD
	if w == nil {
		errResp(c, http.StatusServiceUnavailable, "mtf_macd monitor not enabled")
		return
	}
	ev, updated, lastErr := w.Status()
	p := w.Params()
	ok(c, gin.H{
		"symbol":                  w.Symbol(),
		"eval":                    ev,
		"updated_at":              updated,
		"error":                   lastErr,
		"require_15m_beyond_zero": p.Require15mBeyondZero,
		"macd":                    gin.H{"fast": p.Fast, "slow": p.Slow, "signal": p.Signal},
	})
}

// GetMTFMACDSignals returns fired signals (newest first).
func (h *Handler) GetMTFMACDSignals(c *gin.Context) {
	w := h.deps.MTFMACD
	if w == nil {
		errResp(c, http.StatusServiceUnavailable, "mtf_macd monitor not enabled")
		return
	}
	ok(c, w.History())
}

// SetMTFMACDConfig toggles runtime options.
func (h *Handler) SetMTFMACDConfig(c *gin.Context) {
	w := h.deps.MTFMACD
	if w == nil {
		errResp(c, http.StatusServiceUnavailable, "mtf_macd monitor not enabled")
		return
	}
	var req struct {
		Require15mBeyondZero bool `json:"require_15m_beyond_zero"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errResp(c, http.StatusBadRequest, "invalid body")
		return
	}
	w.SetRequire15mBeyondZero(req.Require15mBeyondZero)
	ok(c, gin.H{"require_15m_beyond_zero": req.Require15mBeyondZero})
}

// RunMTFMACDBacktest replays the rule on historical exchange klines.
func (h *Handler) RunMTFMACDBacktest(c *gin.Context) {
	w := h.deps.MTFMACD
	if w == nil {
		errResp(c, http.StatusServiceUnavailable, "mtf_macd monitor not enabled")
		return
	}
	var req struct {
		Days        int     `json:"days"`
		Reversal    string  `json:"reversal"`
		Expanding4h bool    `json:"expanding_4h"`
		Min1hDifPct float64 `json:"min_1h_dif_pct"`
		LongOnly    bool    `json:"long_only"`
		Divergence  string  `json:"divergence"` // "" | regular | near | hidden | dif_up
	}
	_ = c.ShouldBindJSON(&req)
	p := w.Params()
	p.Reversal, p.Expanding4h, p.Min1hDifPct = req.Reversal, req.Expanding4h, req.Min1hDifPct
	p.LongOnly = req.LongOnly
	p.Divergence, p.DivMode = req.Divergence != "", req.Divergence
	if req.Days <= 0 {
		req.Days = 90
	}
	if req.Days > 365 {
		req.Days = 365
	}
	res, err := mtfmacd.Backtest(c.Request.Context(), h.deps.Exchange, w.Symbol(), req.Days, p)
	if err != nil {
		errResp(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, res)
}

// GetMTFMACDWindow returns 4h/1h/15m bars with MACD around one signal time.
func (h *Handler) GetMTFMACDWindow(c *gin.Context) {
	w := h.deps.MTFMACD
	if w == nil {
		errResp(c, http.StatusServiceUnavailable, "mtf_macd monitor not enabled")
		return
	}
	t, err := time.Parse(time.RFC3339, c.Query("time"))
	if err != nil {
		errResp(c, http.StatusBadRequest, "invalid time")
		return
	}
	win, err := mtfmacd.SignalWindow(c.Request.Context(), h.deps.Exchange, w.Symbol(), t, w.Params())
	if err != nil {
		errResp(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, win)
}
