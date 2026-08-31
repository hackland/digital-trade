package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jayce/btc-trader/internal/storage"
)

// GetVirtualLedger returns the current virtual equity / open-position state
// per direction (LONG/SHORT), plus recent virtual trade legs, for a symbol.
// This tracks hypothetical P&L for alert-only signals (short Short/Cover,
// or a Buy blocked by app.signal_only) that never place a real order.
// GET /api/v1/virtual-ledger?symbol=BTCUSDT&limit=20
func (h *Handler) GetVirtualLedger(c *gin.Context) {
	symbol := c.Query("symbol")
	if symbol == "" {
		errResp(c, http.StatusBadRequest, "symbol is required")
		return
	}

	var state map[string]VirtualLedgerSnapshotView
	if h.deps.VirtualLedger != nil {
		state = h.deps.VirtualLedger.Snapshot(symbol)
	}

	ctx := c.Request.Context()
	limit := parseIntDefault(c.Query("limit"), 20)
	trades, err := h.deps.Store.GetVirtualTrades(ctx, storage.VirtualTradeFilter{
		Symbol: symbol,
		Limit:  limit,
	})
	if err != nil {
		errResp(c, http.StatusInternalServerError, "failed to query virtual trades")
		return
	}

	ok(c, gin.H{
		"symbol":     symbol,
		"state":      state,
		"trades":     trades,
		"queried_at": time.Now(),
	})
}
