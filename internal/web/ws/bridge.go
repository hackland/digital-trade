package ws

import (
	"context"
	"fmt"

	"github.com/jayce/btc-trader/internal/eventbus"
	"go.uber.org/zap"
)

// Bridge forwards EventBus events to WebSocket clients.
type Bridge struct {
	bus    *eventbus.Bus
	hub    *Hub
	logger *zap.Logger
}

// NewBridge creates a new EventBus-to-WebSocket bridge.
func NewBridge(bus *eventbus.Bus, hub *Hub, logger *zap.Logger) *Bridge {
	return &Bridge{bus: bus, hub: hub, logger: logger}
}

// Run subscribes to EventBus events and broadcasts them via WebSocket.
func (b *Bridge) Run(ctx context.Context) {
	klineCh := b.bus.Subscribe(eventbus.EventKlineUpdate, 500)
	signalCh := b.bus.Subscribe(eventbus.EventSignal, 100)
	orderCh := b.bus.Subscribe(eventbus.EventOrderUpdate, 100)
	positionCh := b.bus.Subscribe(eventbus.EventPositionUpdate, 100)
	riskCh := b.bus.Subscribe(eventbus.EventRiskAlert, 100)

	b.logger.Info("ws bridge started")

	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-klineCh:
			if !ok {
				return
			}
			ke, ok := evt.Payload.(eventbus.KlineEvent)
			if !ok {
				continue
			}
			channel := fmt.Sprintf("kline:%s:%s", ke.Symbol, ke.Interval)
			b.hub.BroadcastToChannel(channel, &Message{Type: "kline", Data: ke})
			// Also broadcast 1m klines to "ticker" channel for Header price display
			if ke.Interval == "1m" {
				b.hub.BroadcastToChannel("ticker", &Message{
					Type: "ticker",
					Data: map[string]interface{}{
						"symbol": ke.Symbol,
						"price":  ke.Kline.Close,
					},
				})
			}
		case evt, ok := <-signalCh:
			if !ok {
				return
			}
			b.hub.BroadcastToChannel("signal", &Message{Type: "signal", Data: evt.Payload})
		case evt, ok := <-orderCh:
			if !ok {
				return
			}
			b.hub.BroadcastToChannel("order", &Message{Type: "order", Data: evt.Payload})
		case evt, ok := <-positionCh:
			if !ok {
				return
			}
			b.hub.BroadcastToChannel("position", &Message{Type: "position", Data: evt.Payload})
		case evt, ok := <-riskCh:
			if !ok {
				return
			}
			b.hub.BroadcastToChannel("risk", &Message{Type: "risk_alert", Data: evt.Payload})
		}
	}
}
