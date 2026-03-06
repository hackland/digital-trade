package binance

import (
	"context"
	"fmt"
	"strconv"

	gobinance "github.com/adshao/go-binance/v2"
	"github.com/jayce/btc-trader/internal/exchange"
	"go.uber.org/zap"
)

// Client implements exchange.Exchange for Binance Spot.
type Client struct {
	api    *gobinance.Client
	logger *zap.Logger
}

// NewClient creates a new Binance client.
func NewClient(apiKey, secretKey string, testnet bool, logger *zap.Logger) *Client {
	if testnet {
		gobinance.UseTestnet = true
	}
	api := gobinance.NewClient(apiKey, secretKey)

	return &Client{
		api:    api,
		logger: logger,
	}
}

func (c *Client) Name() string { return "binance" }

// --- Account ---

func (c *Client) GetAccount(ctx context.Context) (*exchange.Account, error) {
	acc, err := c.api.NewGetAccountService().Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("get account: %w", err)
	}

	balances := make([]exchange.Balance, 0, len(acc.Balances))
	for _, b := range acc.Balances {
		bal := convertBalance(b)
		if bal.Free > 0 || bal.Locked > 0 {
			balances = append(balances, bal)
		}
	}

	return &exchange.Account{Balances: balances}, nil
}

func (c *Client) GetBalance(ctx context.Context, asset string) (*exchange.Balance, error) {
	acc, err := c.GetAccount(ctx)
	if err != nil {
		return nil, err
	}

	for _, b := range acc.Balances {
		if b.Asset == asset {
			return &b, nil
		}
	}
	return &exchange.Balance{Asset: asset}, nil
}

// --- Market Data (REST) ---

func (c *Client) GetKlines(ctx context.Context, req exchange.KlineRequest) ([]exchange.Kline, error) {
	svc := c.api.NewKlinesService().
		Symbol(req.Symbol).
		Interval(req.Interval)

	if req.Limit > 0 {
		svc.Limit(req.Limit)
	}
	if req.StartTime != nil {
		svc.StartTime(req.StartTime.UnixMilli())
	}
	if req.EndTime != nil {
		svc.EndTime(req.EndTime.UnixMilli())
	}

	bklines, err := svc.Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("get klines: %w", err)
	}

	klines := make([]exchange.Kline, 0, len(bklines))
	for _, bk := range bklines {
		klines = append(klines, convertKline(req.Symbol, req.Interval, bk))
	}
	return klines, nil
}

func (c *Client) GetOrderBook(ctx context.Context, symbol string, limit int) (*exchange.OrderBook, error) {
	svc := c.api.NewDepthService().Symbol(symbol)
	if limit > 0 {
		svc.Limit(limit)
	}

	depth, err := svc.Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("get depth: %w", err)
	}

	book := &exchange.OrderBook{Symbol: symbol}
	for _, bid := range depth.Bids {
		book.Bids = append(book.Bids, exchange.PriceLevel{
			Price:    parseFloat(bid.Price),
			Quantity: parseFloat(bid.Quantity),
		})
	}
	for _, ask := range depth.Asks {
		book.Asks = append(book.Asks, exchange.PriceLevel{
			Price:    parseFloat(ask.Price),
			Quantity: parseFloat(ask.Quantity),
		})
	}
	return book, nil
}

func (c *Client) GetTicker(ctx context.Context, symbol string) (*exchange.Ticker, error) {
	prices, err := c.api.NewListBookTickersService().Symbol(symbol).Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("get ticker: %w", err)
	}
	if len(prices) == 0 {
		return nil, fmt.Errorf("no ticker for %s", symbol)
	}

	p := prices[0]
	return &exchange.Ticker{
		Symbol:   p.Symbol,
		BidPrice: parseFloat(p.BidPrice),
		AskPrice: parseFloat(p.AskPrice),
	}, nil
}

func (c *Client) GetExchangeInfo(ctx context.Context) (*exchange.ExchangeInfo, error) {
	info, err := c.api.NewExchangeInfoService().Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("get exchange info: %w", err)
	}

	symbols := make([]exchange.SymbolInfo, 0, len(info.Symbols))
	for _, s := range info.Symbols {
		si := exchange.SymbolInfo{
			Symbol:     s.Symbol,
			BaseAsset:  s.BaseAsset,
			QuoteAsset: s.QuoteAsset,
		}
		for _, f := range s.Filters {
			switch f["filterType"] {
			case "LOT_SIZE":
				si.MinQty = parseFloat(f["minQty"].(string))
				si.MaxQty = parseFloat(f["maxQty"].(string))
				si.StepSize = parseFloat(f["stepSize"].(string))
			case "NOTIONAL", "MIN_NOTIONAL":
				if v, ok := f["minNotional"]; ok {
					si.MinNotional = parseFloat(v.(string))
				}
			}
		}
		si.PricePrecision = s.QuotePrecision
		si.QuantityPrecision = s.BaseAssetPrecision
		symbols = append(symbols, si)
	}

	return &exchange.ExchangeInfo{Symbols: symbols}, nil
}

// --- Orders ---

func (c *Client) PlaceOrder(ctx context.Context, req exchange.OrderRequest) (*exchange.Order, error) {
	svc := c.api.NewCreateOrderService().
		Symbol(req.Symbol).
		Side(toBinanceSide(req.Side)).
		Type(toBinanceOrderType(req.Type)).
		Quantity(strconv.FormatFloat(req.Quantity, 'f', -1, 64))

	if req.Type == exchange.OrderTypeLimit {
		svc.Price(strconv.FormatFloat(req.Price, 'f', -1, 64))
		tif := req.TimeInForce
		if tif == "" {
			tif = "GTC"
		}
		svc.TimeInForce(gobinance.TimeInForceType(tif))
	}

	if req.StopPrice > 0 {
		svc.StopPrice(strconv.FormatFloat(req.StopPrice, 'f', -1, 64))
	}
	if req.ClientOrderID != "" {
		svc.NewClientOrderID(req.ClientOrderID)
	}

	c.logger.Info("placing order",
		zap.String("symbol", req.Symbol),
		zap.String("side", req.Side.String()),
		zap.String("type", req.Type.String()),
		zap.Float64("qty", req.Quantity),
		zap.Float64("price", req.Price),
	)

	resp, err := svc.Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("place order: %w", err)
	}

	order := convertOrder(resp)
	c.logger.Info("order placed",
		zap.Int64("order_id", order.ID),
		zap.String("status", order.Status.String()),
	)

	return order, nil
}

func (c *Client) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	_, err := c.api.NewCancelOrderService().
		Symbol(symbol).
		OrderID(orderID).
		Do(ctx)
	if err != nil {
		return fmt.Errorf("cancel order %d: %w", orderID, err)
	}

	c.logger.Info("order canceled",
		zap.String("symbol", symbol),
		zap.Int64("order_id", orderID),
	)
	return nil
}

func (c *Client) GetOrder(ctx context.Context, symbol string, orderID int64) (*exchange.Order, error) {
	o, err := c.api.NewGetOrderService().
		Symbol(symbol).
		OrderID(orderID).
		Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("get order %d: %w", orderID, err)
	}
	return convertQueryOrder(o), nil
}

func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]exchange.Order, error) {
	orders, err := c.api.NewListOpenOrdersService().
		Symbol(symbol).
		Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("get open orders: %w", err)
	}

	result := make([]exchange.Order, 0, len(orders))
	for _, o := range orders {
		result = append(result, *convertQueryOrder(o))
	}
	return result, nil
}

// Ensure compile-time interface compliance.
var _ exchange.Exchange = (*Client)(nil)
