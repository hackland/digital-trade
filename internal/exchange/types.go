package exchange

import "time"

// MarketType distinguishes spot from futures.
type MarketType int

const (
	MarketSpot MarketType = iota
	MarketUSDTMFutures
	MarketCoinMFutures
)

// OrderSide represents buy or sell.
type OrderSide int

const (
	OrderSideBuy OrderSide = iota
	OrderSideSell
)

func (s OrderSide) String() string {
	if s == OrderSideBuy {
		return "BUY"
	}
	return "SELL"
}

// OrderType represents the type of order.
type OrderType int

const (
	OrderTypeMarket OrderType = iota
	OrderTypeLimit
	OrderTypeStopLoss
	OrderTypeTakeProfit
)

func (t OrderType) String() string {
	switch t {
	case OrderTypeMarket:
		return "MARKET"
	case OrderTypeLimit:
		return "LIMIT"
	case OrderTypeStopLoss:
		return "STOP_LOSS"
	case OrderTypeTakeProfit:
		return "TAKE_PROFIT"
	default:
		return "UNKNOWN"
	}
}

// OrderStatus represents the current state of an order.
type OrderStatus int

const (
	OrderStatusNew OrderStatus = iota
	OrderStatusPartiallyFilled
	OrderStatusFilled
	OrderStatusCanceled
	OrderStatusRejected
	OrderStatusExpired
)

func (s OrderStatus) String() string {
	switch s {
	case OrderStatusNew:
		return "NEW"
	case OrderStatusPartiallyFilled:
		return "PARTIALLY_FILLED"
	case OrderStatusFilled:
		return "FILLED"
	case OrderStatusCanceled:
		return "CANCELED"
	case OrderStatusRejected:
		return "REJECTED"
	case OrderStatusExpired:
		return "EXPIRED"
	default:
		return "UNKNOWN"
	}
}

// Kline represents a candlestick bar.
type Kline struct {
	Symbol      string    `json:"symbol"`
	Interval    string    `json:"interval"`
	OpenTime    time.Time `json:"open_time"`
	CloseTime   time.Time `json:"close_time"`
	Open        float64   `json:"open"`
	High        float64   `json:"high"`
	Low         float64   `json:"low"`
	Close       float64   `json:"close"`
	Volume      float64   `json:"volume"`
	QuoteVolume float64   `json:"quote_volume"`
	Trades      int64     `json:"trades"`
	IsFinal     bool      `json:"is_final"`
}

// KlineRequest specifies parameters for historical kline queries.
type KlineRequest struct {
	Symbol    string
	Interval  string
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
}

// OrderRequest specifies parameters for placing an order.
type OrderRequest struct {
	Symbol        string
	Side          OrderSide
	Type          OrderType
	Quantity      float64
	Price         float64 // For limit orders
	StopPrice     float64 // For stop orders
	TimeInForce   string  // GTC, IOC, FOK
	ClientOrderID string  // Optional idempotency key
}

// Order represents an order on the exchange.
type Order struct {
	ID            int64
	ClientOrderID string
	Symbol        string
	Side          OrderSide
	Type          OrderType
	Status        OrderStatus
	Price         float64
	Quantity      float64
	FilledQty     float64
	AvgPrice      float64
	StopPrice     float64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Trade represents a filled trade.
type Trade struct {
	ID        int64
	OrderID   int64
	Symbol    string
	Side      OrderSide
	Price     float64
	Quantity  float64
	Fee       float64
	FeeAsset  string
	Timestamp time.Time
}

// Balance represents an asset balance.
type Balance struct {
	Asset  string
	Free   float64
	Locked float64
}

// Account represents the account state.
type Account struct {
	Balances []Balance
}

// DepthUpdate represents an order book update.
type DepthUpdate struct {
	Symbol string
	Bids   []PriceLevel
	Asks   []PriceLevel
}

// PriceLevel represents a price-quantity pair in the order book.
type PriceLevel struct {
	Price    float64
	Quantity float64
}

// OrderBook represents a full order book snapshot.
type OrderBook struct {
	Symbol string
	Bids   []PriceLevel
	Asks   []PriceLevel
}

// Ticker represents a real-time ticker.
type Ticker struct {
	Symbol    string
	BidPrice  float64
	AskPrice  float64
	LastPrice float64
	Volume24h float64
}

// UserDataEvent represents events from the user data stream.
type UserDataEvent struct {
	Type          string // "orderUpdate", "tradeUpdate", "balanceUpdate"
	OrderUpdate   *Order
	TradeUpdate   *Trade
	BalanceUpdate *Balance
}

// ExchangeInfo contains exchange trading rules and symbol information.
type ExchangeInfo struct {
	Symbols []SymbolInfo
}

// SymbolInfo contains trading rules for a symbol.
type SymbolInfo struct {
	Symbol            string
	BaseAsset         string
	QuoteAsset        string
	MinQty            float64
	MaxQty            float64
	StepSize          float64
	MinNotional       float64
	PricePrecision    int
	QuantityPrecision int
}
