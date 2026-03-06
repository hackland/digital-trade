package handler

import (
	"github.com/jayce/btc-trader/internal/config"
	"github.com/jayce/btc-trader/internal/eventbus"
	"github.com/jayce/btc-trader/internal/exchange"
	"github.com/jayce/btc-trader/internal/order"
	"github.com/jayce/btc-trader/internal/position"
	"github.com/jayce/btc-trader/internal/risk"
	"github.com/jayce/btc-trader/internal/storage"
	"github.com/jayce/btc-trader/internal/strategy"
	"go.uber.org/zap"
)

// Deps holds all dependencies the dashboard needs from the trader.
type Deps struct {
	Config   *config.Config
	Bus      *eventbus.Bus
	Store    storage.Store
	Exchange exchange.Exchange
	Position *position.Manager
	Risk     *risk.Manager
	Order    *order.Manager
	Strategy strategy.Strategy
}

// Handler holds all REST API handler methods.
type Handler struct {
	deps   *Deps
	logger *zap.Logger
}

// New creates a new Handler.
func New(deps *Deps, logger *zap.Logger) *Handler {
	return &Handler{deps: deps, logger: logger}
}
