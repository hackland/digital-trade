package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type overviewResponse struct {
	TotalEquity     float64            `json:"total_equity"`
	FreeCash        float64            `json:"free_cash"`
	PositionValue   float64            `json:"position_value"`
	UnrealizedPnL   float64            `json:"unrealized_pnl"`
	RealizedPnL     float64            `json:"realized_pnl"`
	DailyPnL        float64            `json:"daily_pnl"`
	DrawdownPct     float64            `json:"drawdown_pct"`
	IsTradingPaused bool               `json:"is_trading_paused"`
	Positions       []positionResponse `json:"positions"`
}

// GetOverview returns aggregated account overview.
func (h *Handler) GetOverview(c *gin.Context) {
	ctx := c.Request.Context()

	// Account balance
	acc, err := h.deps.Exchange.GetAccount(ctx)
	if err != nil {
		errResp(c, http.StatusInternalServerError, "failed to get account")
		return
	}

	var freeCash float64
	for _, b := range acc.Balances {
		if b.Asset == "USDT" {
			freeCash = b.Free
			break
		}
	}

	// Positions
	positions := h.deps.Position.GetAllPositions()
	posValue := 0.0
	posList := make([]positionResponse, 0, len(positions))
	for _, pos := range positions {
		posValue += pos.Quantity * pos.CurrentPrice
		if pos.Quantity != 0 {
			posList = append(posList, positionResponse{
				Symbol:        pos.Symbol,
				Quantity:      pos.Quantity,
				AvgEntryPrice: pos.AvgEntryPrice,
				CurrentPrice:  pos.CurrentPrice,
				UnrealizedPnL: pos.UnrealizedPnL,
				RealizedPnL:   pos.RealizedPnL,
				Side:          pos.Side,
			})
		}
	}

	// Risk
	riskStatus := h.deps.Risk.GetStatus()

	ok(c, overviewResponse{
		TotalEquity:     freeCash + posValue,
		FreeCash:        freeCash,
		PositionValue:   posValue,
		UnrealizedPnL:   h.deps.Position.TotalUnrealizedPnL(),
		RealizedPnL:     h.deps.Position.TotalRealizedPnL(),
		DailyPnL:        riskStatus.DailyPnL,
		DrawdownPct:     riskStatus.CurrentDrawdown,
		IsTradingPaused: riskStatus.IsTradingPaused,
		Positions:       posList,
	})
}
