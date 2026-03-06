package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jayce/btc-trader/internal/storage"
)

// GetOrders returns paginated order history.
func (h *Handler) GetOrders(c *gin.Context) {
	ctx := c.Request.Context()
	filter := storage.OrderFilter{
		Symbol: c.Query("symbol"),
		Status: c.Query("status"),
		Limit:  parseIntDefault(c.Query("limit"), 20),
		Offset: parseIntDefault(c.Query("offset"), 0),
	}
	if s := c.Query("start"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			filter.StartTime = &t
		}
	}
	if s := c.Query("end"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			filter.EndTime = &t
		}
	}

	orders, err := h.deps.Store.GetOrders(ctx, filter)
	if err != nil {
		errResp(c, http.StatusInternalServerError, "failed to query orders")
		return
	}
	paginated(c, orders, len(orders), filter.Limit, filter.Offset)
}

// GetActiveOrders returns currently active orders from memory.
func (h *Handler) GetActiveOrders(c *gin.Context) {
	orders := h.deps.Order.GetActiveOrders()
	ok(c, orders)
}

// GetOrder returns a single order by ID.
func (h *Handler) GetOrder(c *gin.Context) {
	ctx := c.Request.Context()
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		errResp(c, http.StatusBadRequest, "invalid order id")
		return
	}

	order, err := h.deps.Store.GetOrder(ctx, id)
	if err != nil {
		errResp(c, http.StatusNotFound, "order not found")
		return
	}
	ok(c, order)
}

func parseIntDefault(s string, def int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}
