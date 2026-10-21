package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/jayce/btc-trader/internal/ict"
)

const ictSymbol = "BTCUSDT"

// RunICTBacktest replays the ICT model over `days` of history.
// POST /api/v1/ict/backtest  {"days":90, ...ict.Params overrides}
func (h *Handler) RunICTBacktest(c *gin.Context) {
	req := struct {
		Days int `json:"days"`
		ict.Params
	}{Days: 90, Params: ict.DefaultParams()}
	_ = c.ShouldBindJSON(&req) // an empty body just means "defaults"

	res, err := ict.Analyze(c.Request.Context(), h.deps.Exchange, ictSymbol, req.Days, req.Params)
	if err != nil {
		errResp(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, res)
}

// GetICTScan analyses the most recent window with default parameters: current bias,
// armed/open setups and their zones, for the live chart.
// GET /api/v1/ict/scan?days=14
func (h *Handler) GetICTScan(c *gin.Context) {
	days := 14
	if v, err := strconv.Atoi(c.Query("days")); err == nil {
		days = v
	}
	res, err := ict.Analyze(c.Request.Context(), h.deps.Exchange, ictSymbol, days, ict.DefaultParams())
	if err != nil {
		errResp(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, res)
}
