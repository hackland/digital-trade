package exchange

import "context"

// Exchange defines the interface for interacting with a crypto exchange.
// Implementations: binance.Client (live), simulated.Exchange (backtest).
type Exchange interface {
	// Account
	GetAccount(ctx context.Context) (*Account, error)
	GetBalance(ctx context.Context, asset string) (*Balance, error)

	// Market Data (REST)
	GetKlines(ctx context.Context, req KlineRequest) ([]Kline, error)
	GetOrderBook(ctx context.Context, symbol string, limit int) (*OrderBook, error)
	GetTicker(ctx context.Context, symbol string) (*Ticker, error)
	GetExchangeInfo(ctx context.Context) (*ExchangeInfo, error)

	// Orders
	PlaceOrder(ctx context.Context, req OrderRequest) (*Order, error)
	CancelOrder(ctx context.Context, symbol string, orderID int64) error
	GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error)
	GetOpenOrders(ctx context.Context, symbol string) ([]Order, error)

	// Streams
	SubscribeKlines(ctx context.Context, symbol, interval string) (<-chan Kline, error)
	SubscribeDepth(ctx context.Context, symbol string) (<-chan DepthUpdate, error)
	SubscribeTrades(ctx context.Context, symbol string) (<-chan Trade, error)
	SubscribeUserData(ctx context.Context) (<-chan UserDataEvent, error)

	// Info
	Name() string
}
