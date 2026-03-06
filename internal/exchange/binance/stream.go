package binance

import (
	"context"
	"fmt"
	"time"

	gobinance "github.com/adshao/go-binance/v2"
	"github.com/jayce/btc-trader/internal/exchange"
	"go.uber.org/zap"
)

// SubscribeKlines subscribes to kline WebSocket updates for a symbol/interval.
// Returns a channel that delivers kline events. The channel is closed when
// the context is canceled.
func (c *Client) SubscribeKlines(ctx context.Context, symbol, interval string) (<-chan exchange.Kline, error) {
	ch := make(chan exchange.Kline, 500)

	go func() {
		defer close(ch)
		c.runKlineStream(ctx, symbol, interval, ch)
	}()

	c.logger.Info("subscribed to klines",
		zap.String("symbol", symbol),
		zap.String("interval", interval),
	)
	return ch, nil
}

func (c *Client) runKlineStream(ctx context.Context, symbol, interval string, ch chan<- exchange.Kline) {
	backoff := newExponentialBackoff(time.Second, time.Minute)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		handler := func(event *gobinance.WsKlineEvent) {
			kline := convertWsKline(event)
			select {
			case ch <- kline:
			default:
				c.logger.Warn("kline channel full, dropping event",
					zap.String("symbol", symbol),
				)
			}
		}

		errHandler := func(err error) {
			c.logger.Error("kline ws error",
				zap.String("symbol", symbol),
				zap.String("interval", interval),
				zap.Error(err),
			)
		}

		doneC, stopC, err := gobinance.WsKlineServe(symbol, interval, handler, errHandler)
		if err != nil {
			c.logger.Error("failed to start kline ws",
				zap.Error(err),
				zap.Duration("backoff", backoff.Current()),
			)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff.Next()):
				continue
			}
		}

		backoff.Reset()
		c.logger.Info("kline ws connected",
			zap.String("symbol", symbol),
			zap.String("interval", interval),
		)

		// Wait for disconnection or context cancel
		select {
		case <-ctx.Done():
			close(stopC)
			return
		case <-doneC:
			c.logger.Warn("kline ws disconnected, reconnecting",
				zap.String("symbol", symbol),
				zap.Duration("backoff", backoff.Current()),
			)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff.Next()):
			}
		}
	}
}

// SubscribeDepth subscribes to depth (order book) WebSocket updates.
func (c *Client) SubscribeDepth(ctx context.Context, symbol string) (<-chan exchange.DepthUpdate, error) {
	ch := make(chan exchange.DepthUpdate, 100)

	go func() {
		defer close(ch)
		c.runDepthStream(ctx, symbol, ch)
	}()

	c.logger.Info("subscribed to depth", zap.String("symbol", symbol))
	return ch, nil
}

func (c *Client) runDepthStream(ctx context.Context, symbol string, ch chan<- exchange.DepthUpdate) {
	backoff := newExponentialBackoff(time.Second, time.Minute)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		handler := func(event *gobinance.WsDepthEvent) {
			update := exchange.DepthUpdate{Symbol: symbol}
			for _, b := range event.Bids {
				update.Bids = append(update.Bids, exchange.PriceLevel{
					Price:    parseFloat(b.Price),
					Quantity: parseFloat(b.Quantity),
				})
			}
			for _, a := range event.Asks {
				update.Asks = append(update.Asks, exchange.PriceLevel{
					Price:    parseFloat(a.Price),
					Quantity: parseFloat(a.Quantity),
				})
			}
			select {
			case ch <- update:
			default:
			}
		}

		errHandler := func(err error) {
			c.logger.Error("depth ws error", zap.String("symbol", symbol), zap.Error(err))
		}

		doneC, stopC, err := gobinance.WsDepthServe(symbol, handler, errHandler)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff.Next()):
				continue
			}
		}

		backoff.Reset()

		select {
		case <-ctx.Done():
			close(stopC)
			return
		case <-doneC:
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff.Next()):
			}
		}
	}
}

// SubscribeTrades subscribes to real-time trade (aggregate) events.
func (c *Client) SubscribeTrades(ctx context.Context, symbol string) (<-chan exchange.Trade, error) {
	ch := make(chan exchange.Trade, 500)

	go func() {
		defer close(ch)
		c.runTradeStream(ctx, symbol, ch)
	}()

	c.logger.Info("subscribed to trades", zap.String("symbol", symbol))
	return ch, nil
}

func (c *Client) runTradeStream(ctx context.Context, symbol string, ch chan<- exchange.Trade) {
	backoff := newExponentialBackoff(time.Second, time.Minute)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		handler := func(event *gobinance.WsAggTradeEvent) {
			side := exchange.OrderSideBuy
			if event.IsBuyerMaker {
				side = exchange.OrderSideSell
			}
			trade := exchange.Trade{
				ID:        event.AggTradeID,
				Symbol:    event.Symbol,
				Side:      side,
				Price:     parseFloat(event.Price),
				Quantity:  parseFloat(event.Quantity),
				Timestamp: msToTime(event.TradeTime),
			}
			select {
			case ch <- trade:
			default:
			}
		}

		errHandler := func(err error) {
			c.logger.Error("aggTrade ws error", zap.String("symbol", symbol), zap.Error(err))
		}

		doneC, stopC, err := gobinance.WsAggTradeServe(symbol, handler, errHandler)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff.Next()):
				continue
			}
		}

		backoff.Reset()

		select {
		case <-ctx.Done():
			close(stopC)
			return
		case <-doneC:
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff.Next()):
			}
		}
	}
}

// SubscribeUserData subscribes to the user data stream (order updates, balance updates).
func (c *Client) SubscribeUserData(ctx context.Context) (<-chan exchange.UserDataEvent, error) {
	ch := make(chan exchange.UserDataEvent, 100)

	// Start user data stream (get listen key)
	listenKey, err := c.api.NewStartUserStreamService().Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("start user data stream: %w", err)
	}

	go func() {
		defer close(ch)
		c.runUserDataStream(ctx, listenKey, ch)
	}()

	// Keep listen key alive (every 30 minutes)
	go func() {
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := c.api.NewKeepaliveUserStreamService().ListenKey(listenKey).Do(ctx); err != nil {
					c.logger.Error("keepalive user data stream failed", zap.Error(err))
				}
			}
		}
	}()

	c.logger.Info("subscribed to user data stream")
	return ch, nil
}

func (c *Client) runUserDataStream(ctx context.Context, listenKey string, ch chan<- exchange.UserDataEvent) {
	backoff := newExponentialBackoff(time.Second, time.Minute)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		handler := func(event *gobinance.WsUserDataEvent) {
			evt := exchange.UserDataEvent{}

			switch event.Event {
			case gobinance.UserDataEventTypeExecutionReport:
				order := &exchange.Order{
					ID:            event.OrderUpdate.Id,
					ClientOrderID: event.OrderUpdate.ClientOrderId,
					Symbol:        event.OrderUpdate.Symbol,
					Side:          convertSide(gobinance.SideType(event.OrderUpdate.Side)),
					Type:          convertOrderType(gobinance.OrderType(event.OrderUpdate.Type)),
					Status:        convertOrderStatus(gobinance.OrderStatusType(event.OrderUpdate.Status)),
					Price:         parseFloat(event.OrderUpdate.Price),
					Quantity:      parseFloat(event.OrderUpdate.Volume),
					FilledQty:     parseFloat(event.OrderUpdate.FilledVolume),
					UpdatedAt:     msToTime(event.OrderUpdate.TransactionTime),
				}
				evt.Type = "orderUpdate"
				evt.OrderUpdate = order

			case gobinance.UserDataEventTypeOutboundAccountPosition:
				for _, b := range event.AccountUpdate.WsAccountUpdates {
					bal := &exchange.Balance{
						Asset:  b.Asset,
						Free:   parseFloat(b.Free),
						Locked: parseFloat(b.Locked),
					}
					evt.Type = "balanceUpdate"
					evt.BalanceUpdate = bal
				}
			}

			if evt.Type != "" {
				select {
				case ch <- evt:
				default:
				}
			}
		}

		errHandler := func(err error) {
			c.logger.Error("user data ws error", zap.Error(err))
		}

		doneC, stopC, err := gobinance.WsUserDataServe(listenKey, handler, errHandler)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff.Next()):
				continue
			}
		}

		backoff.Reset()
		c.logger.Info("user data ws connected")

		select {
		case <-ctx.Done():
			close(stopC)
			return
		case <-doneC:
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff.Next()):
			}
		}
	}
}

// --- Exponential backoff ---

type exponentialBackoff struct {
	base    time.Duration
	max     time.Duration
	current time.Duration
}

func newExponentialBackoff(base, max time.Duration) *exponentialBackoff {
	return &exponentialBackoff{base: base, max: max, current: base}
}

func (b *exponentialBackoff) Next() time.Duration {
	d := b.current
	b.current *= 2
	if b.current > b.max {
		b.current = b.max
	}
	return d
}

func (b *exponentialBackoff) Current() time.Duration {
	return b.current
}

func (b *exponentialBackoff) Reset() {
	b.current = b.base
}
