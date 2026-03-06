package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type positionResponse struct {
	Symbol        string  `json:"symbol"`
	Quantity      float64 `json:"quantity"`
	AvgEntryPrice float64 `json:"avg_entry_price"`
	CurrentPrice  float64 `json:"current_price"`
	UnrealizedPnL float64 `json:"unrealized_pnl"`
	RealizedPnL   float64 `json:"realized_pnl"`
	Side          string  `json:"side"`
}

// GetPositions returns all current positions.
func (h *Handler) GetPositions(c *gin.Context) {
	positions := h.deps.Position.GetAllPositions()
	result := make([]positionResponse, 0, len(positions))
	for _, pos := range positions {
		result = append(result, positionResponse{
			Symbol:        pos.Symbol,
			Quantity:      pos.Quantity,
			AvgEntryPrice: pos.AvgEntryPrice,
			CurrentPrice:  pos.CurrentPrice,
			UnrealizedPnL: pos.UnrealizedPnL,
			RealizedPnL:   pos.RealizedPnL,
			Side:          pos.Side,
		})
	}
	ok(c, result)
}

// GetPosition returns position for a specific symbol.
func (h *Handler) GetPosition(c *gin.Context) {
	symbol := c.Param("symbol")
	if symbol == "" {
		errResp(c, http.StatusBadRequest, "symbol is required")
		return
	}
	pos := h.deps.Position.GetPosition(symbol)
	ok(c, positionResponse{
		Symbol:        pos.Symbol,
		Quantity:      pos.Quantity,
		AvgEntryPrice: pos.AvgEntryPrice,
		CurrentPrice:  pos.CurrentPrice,
		UnrealizedPnL: pos.UnrealizedPnL,
		RealizedPnL:   pos.RealizedPnL,
		Side:          pos.Side,
	})
}
