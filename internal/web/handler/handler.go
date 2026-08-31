package handler

import (
	"github.com/jayce/btc-trader/internal/config"
	"github.com/jayce/btc-trader/internal/eventbus"
	"github.com/jayce/btc-trader/internal/exchange"
	"github.com/jayce/btc-trader/internal/order"
	"github.com/jayce/btc-trader/internal/override"
	"github.com/jayce/btc-trader/internal/position"
	"github.com/jayce/btc-trader/internal/risk"
	"github.com/jayce/btc-trader/internal/storage"
	"github.com/jayce/btc-trader/internal/strategy"
	"go.uber.org/zap"
)

// Deps holds all dependencies the dashboard needs from the trader.
type Deps struct {
	Config        *config.Config
	Bus           *eventbus.Bus
	Store         storage.Store
	Exchange      exchange.Exchange
	Position      *position.Manager
	Risk          *risk.Manager
	Order         *order.Manager
	Override      *override.Manager
	Strategy      strategy.Strategy
	VirtualLedger VirtualLedgerReader
}

// VirtualLedgerReader is the minimal read interface the dashboard needs from
// internal/app.VirtualLedger, kept here (rather than importing internal/app
// directly) to avoid a web -> app dependency cycle (app already imports web).
type VirtualLedgerReader interface {
	Snapshot(symbol string) map[string]VirtualLedgerSnapshotView
}

// VirtualLedgerSnapshotView mirrors app.VirtualLedgerSnapshot's shape.
type VirtualLedgerSnapshotView struct {
	Equity     float64 `json:"equity"`
	InPosition bool    `json:"in_position"`
	EntryPrice float64 `json:"entry_price,omitempty"`
	Quantity   float64 `json:"quantity,omitempty"`
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
