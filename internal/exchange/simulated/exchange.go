package simulated

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jayce/btc-trader/internal/exchange"
)

// Exchange simulates an exchange for backtesting.
// It replays historical klines and simulates order fills at market prices.
type Exchange struct {
	mu sync.RWMutex

	// Balances
	balances map[string]*exchange.Balance

	// Orders
	orders map[int64]*exchange.Order
	nextID atomic.Int64

	// Current market price per symbol (set by backtest engine on each bar)
	prices map[string]float64

	// Pending stop orders to check on each price update
	pendingStops map[int64]*exchange.Order

	// Fee rate (e.g., 0.001 = 0.1%)
	feeRate float64

	// Trade history
	trades []*exchange.Trade

	// Filled orders channel for backtest engine to consume
	fillCh chan *exchange.Order
}

// NewExchange creates a simulated exchange with initial USDT balance.
func NewExchange(initialUSDT float64, feeRate float64) *Exchange {
	e := &Exchange{
		balances: map[string]*exchange.Balance{
			"USDT": {Asset: "USDT", Free: initialUSDT, Locked: 0},
			"BTC":  {Asset: "BTC", Free: 0, Locked: 0},
			"ETH":  {Asset: "ETH", Free: 0, Locked: 0},
		},
		orders:       make(map[int64]*exchange.Order),
		prices:       make(map[string]float64),
		pendingStops: make(map[int64]*exchange.Order),
		feeRate:      feeRate,
		fillCh:       make(chan *exchange.Order, 1000),
	}
	return e
}

// Name returns "simulated".
func (e *Exchange) Name() string {
	return "simulated"
}

// SetPrice updates the current market price for a symbol and checks pending stops.
func (e *Exchange) SetPrice(symbol string, price float64) []*exchange.Order {
	e.mu.Lock()
	e.prices[symbol] = price
	e.mu.Unlock()

	return e.checkPendingStops(symbol, price)
}

// FillChannel returns the channel of filled orders (for backtest engine).
func (e *Exchange) FillChannel() <-chan *exchange.Order {
	return e.fillCh
}

// GetTrades returns all executed trades.
func (e *Exchange) GetTrades() []*exchange.Trade {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := make([]*exchange.Trade, len(e.trades))
	copy(result, e.trades)
	return result
}

// --- Exchange Interface Implementation ---

func (e *Exchange) GetAccount(_ context.Context) (*exchange.Account, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	acc := &exchange.Account{
		Balances: make([]exchange.Balance, 0, len(e.balances)),
	}
	for _, b := range e.balances {
		acc.Balances = append(acc.Balances, *b)
	}
	return acc, nil
}

func (e *Exchange) GetBalance(_ context.Context, asset string) (*exchange.Balance, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	b, ok := e.balances[asset]
	if !ok {
		return &exchange.Balance{Asset: asset}, nil
	}
	cp := *b
	return &cp, nil
}

func (e *Exchange) GetKlines(_ context.Context, _ exchange.KlineRequest) ([]exchange.Kline, error) {
	// In backtesting, klines are fed by the engine, not fetched
	return nil, nil
}

func (e *Exchange) GetOrderBook(_ context.Context, _ string, _ int) (*exchange.OrderBook, error) {
	return &exchange.OrderBook{}, nil
}

func (e *Exchange) GetTicker(_ context.Context, symbol string) (*exchange.Ticker, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	price := e.prices[symbol]
	return &exchange.Ticker{
		Symbol:    symbol,
		BidPrice:  price,
		AskPrice:  price,
		LastPrice: price,
	}, nil
}

func (e *Exchange) GetExchangeInfo(_ context.Context) (*exchange.ExchangeInfo, error) {
	return &exchange.ExchangeInfo{
		Symbols: []exchange.SymbolInfo{
			{
				Symbol: "BTCUSDT", BaseAsset: "BTC", QuoteAsset: "USDT",
				MinQty: 0.00001, MaxQty: 1000, StepSize: 0.00001,
				MinNotional: 10, PricePrecision: 2, QuantityPrecision: 5,
			},
			{
				Symbol: "ETHUSDT", BaseAsset: "ETH", QuoteAsset: "USDT",
				MinQty: 0.0001, MaxQty: 10000, StepSize: 0.0001,
				MinNotional: 10, PricePrecision: 2, QuantityPrecision: 4,
			},
		},
	}, nil
}

func (e *Exchange) PlaceOrder(_ context.Context, req exchange.OrderRequest) (*exchange.Order, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	id := e.nextID.Add(1)
	now := time.Now()

	order := &exchange.Order{
		ID:        id,
		Symbol:    req.Symbol,
		Side:      req.Side,
		Type:      req.Type,
		Status:    exchange.OrderStatusNew,
		Price:     req.Price,
		Quantity:  req.Quantity,
		StopPrice: req.StopPrice,
		CreatedAt: now,
		UpdatedAt: now,
	}

	switch req.Type {
	case exchange.OrderTypeMarket:
		// Fill immediately at current price
		fillPrice := e.prices[req.Symbol]
		if fillPrice == 0 {
			return nil, fmt.Errorf("no market price for %s", req.Symbol)
		}
		if err := e.fillOrder(order, fillPrice); err != nil {
			return nil, err
		}

	case exchange.OrderTypeLimit:
		// In simulated exchange, fill immediately for simplicity
		fillPrice := req.Price
		if fillPrice == 0 {
			fillPrice = e.prices[req.Symbol]
		}
		if err := e.fillOrder(order, fillPrice); err != nil {
			return nil, err
		}

	case exchange.OrderTypeStopLoss, exchange.OrderTypeTakeProfit:
		// Add to pending stops, check on each price update
		e.pendingStops[id] = order
		e.orders[id] = order
		return order, nil

	default:
		return nil, fmt.Errorf("unsupported order type: %s", req.Type)
	}

	e.orders[id] = order
	return order, nil
}

func (e *Exchange) CancelOrder(_ context.Context, _ string, orderID int64) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	order, ok := e.orders[orderID]
	if !ok {
		return fmt.Errorf("order %d not found", orderID)
	}
	if order.Status == exchange.OrderStatusFilled {
		return fmt.Errorf("cannot cancel filled order %d", orderID)
	}

	order.Status = exchange.OrderStatusCanceled
	order.UpdatedAt = time.Now()
	delete(e.pendingStops, orderID)
	return nil
}

func (e *Exchange) GetOrder(_ context.Context, _ string, orderID int64) (*exchange.Order, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	order, ok := e.orders[orderID]
	if !ok {
		return nil, fmt.Errorf("order %d not found", orderID)
	}
	cp := *order
	return &cp, nil
}

func (e *Exchange) GetOpenOrders(_ context.Context, symbol string) ([]exchange.Order, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var result []exchange.Order
	for _, o := range e.orders {
		if o.Symbol == symbol && o.Status == exchange.OrderStatusNew {
			result = append(result, *o)
		}
	}
	return result, nil
}

// Streaming methods are not used in backtesting — return closed channels.

func (e *Exchange) SubscribeKlines(_ context.Context, _, _ string) (<-chan exchange.Kline, error) {
	ch := make(chan exchange.Kline)
	close(ch)
	return ch, nil
}

func (e *Exchange) SubscribeDepth(_ context.Context, _ string) (<-chan exchange.DepthUpdate, error) {
	ch := make(chan exchange.DepthUpdate)
	close(ch)
	return ch, nil
}

func (e *Exchange) SubscribeTrades(_ context.Context, _ string) (<-chan exchange.Trade, error) {
	ch := make(chan exchange.Trade)
	close(ch)
	return ch, nil
}

func (e *Exchange) SubscribeUserData(_ context.Context) (<-chan exchange.UserDataEvent, error) {
	ch := make(chan exchange.UserDataEvent)
	close(ch)
	return ch, nil
}

// --- Internal ---

// fillOrder fills an order at the given price, updating balances.
// Must be called with e.mu held.
func (e *Exchange) fillOrder(order *exchange.Order, fillPrice float64) error {
	qty := order.Quantity
	cost := fillPrice * qty
	fee := cost * e.feeRate

	if order.Side == exchange.OrderSideBuy {
		// Check USDT balance
		usdtBal := e.balances["USDT"]
		if usdtBal.Free < cost+fee {
			return fmt.Errorf("insufficient USDT: need %.2f, have %.2f", cost+fee, usdtBal.Free)
		}
		usdtBal.Free -= cost + fee

		// Add to base asset
		baseAsset := symbolToBase(order.Symbol)
		if _, ok := e.balances[baseAsset]; !ok {
			e.balances[baseAsset] = &exchange.Balance{Asset: baseAsset}
		}
		e.balances[baseAsset].Free += qty

	} else {
		// Sell
		baseAsset := symbolToBase(order.Symbol)
		baseBal, ok := e.balances[baseAsset]
		if !ok || baseBal.Free < qty {
			freeQty := 0.0
			if ok {
				freeQty = baseBal.Free
			}
			return fmt.Errorf("insufficient %s: need %.8f, have %.8f", baseAsset, qty, freeQty)
		}
		baseBal.Free -= qty

		// Add USDT proceeds minus fee
		e.balances["USDT"].Free += cost - fee
	}

	order.Status = exchange.OrderStatusFilled
	order.FilledQty = qty
	order.AvgPrice = fillPrice
	order.UpdatedAt = time.Now()

	// Record trade
	trade := &exchange.Trade{
		ID:        e.nextID.Add(1),
		OrderID:   order.ID,
		Symbol:    order.Symbol,
		Side:      order.Side,
		Price:     fillPrice,
		Quantity:  qty,
		Fee:       fee,
		FeeAsset:  "USDT",
		Timestamp: order.UpdatedAt,
	}
	e.trades = append(e.trades, trade)

	// Send to fill channel (non-blocking)
	select {
	case e.fillCh <- order:
	default:
	}

	return nil
}

// checkPendingStops checks if any pending stop orders should trigger.
func (e *Exchange) checkPendingStops(symbol string, price float64) []*exchange.Order {
	e.mu.Lock()
	defer e.mu.Unlock()

	var filled []*exchange.Order
	for id, order := range e.pendingStops {
		if order.Symbol != symbol {
			continue
		}

		triggered := false

		switch order.Type {
		case exchange.OrderTypeStopLoss:
			// SL triggers when price drops to or below stop price
			if price <= order.StopPrice {
				triggered = true
			}
		case exchange.OrderTypeTakeProfit:
			// TP triggers when price rises to or above stop price
			if price >= order.StopPrice {
				triggered = true
			}
		}

		if triggered {
			if err := e.fillOrder(order, price); err == nil {
				filled = append(filled, order)
			}
			delete(e.pendingStops, id)
		}
	}

	return filled
}

// symbolToBase extracts the base asset from a symbol like "BTCUSDT".
func symbolToBase(symbol string) string {
	// Handle common suffixes
	for _, quote := range []string{"USDT", "BUSD", "USDC"} {
		if len(symbol) > len(quote) {
			suffix := symbol[len(symbol)-len(quote):]
			if suffix == quote {
				return symbol[:len(symbol)-len(quote)]
			}
		}
	}
	return symbol
}

// Compile-time interface check.
var _ exchange.Exchange = (*Exchange)(nil)
