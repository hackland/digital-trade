package binance

import (
	"strconv"
	"time"

	gobinance "github.com/adshao/go-binance/v2"
	"github.com/jayce/btc-trader/internal/exchange"
)

// --- Kline conversion ---

func convertKline(symbol, interval string, bk *gobinance.Kline) exchange.Kline {
	return exchange.Kline{
		Symbol:      symbol,
		Interval:    interval,
		OpenTime:    msToTime(bk.OpenTime),
		CloseTime:   msToTime(bk.CloseTime),
		Open:        parseFloat(bk.Open),
		High:        parseFloat(bk.High),
		Low:         parseFloat(bk.Low),
		Close:       parseFloat(bk.Close),
		Volume:      parseFloat(bk.Volume),
		QuoteVolume: parseFloat(bk.QuoteAssetVolume),
		Trades:      int64(bk.TradeNum),
		IsFinal:     true,
	}
}

func convertWsKline(e *gobinance.WsKlineEvent) exchange.Kline {
	return exchange.Kline{
		Symbol:      e.Symbol,
		Interval:    e.Kline.Interval,
		OpenTime:    msToTime(e.Kline.StartTime),
		CloseTime:   msToTime(e.Kline.EndTime),
		Open:        parseFloat(e.Kline.Open),
		High:        parseFloat(e.Kline.High),
		Low:         parseFloat(e.Kline.Low),
		Close:       parseFloat(e.Kline.Close),
		Volume:      parseFloat(e.Kline.Volume),
		QuoteVolume: parseFloat(e.Kline.QuoteVolume),
		Trades:      int64(e.Kline.TradeNum),
		IsFinal:     e.Kline.IsFinal,
	}
}

// --- Order conversion ---

func convertOrder(o *gobinance.CreateOrderResponse) *exchange.Order {
	filledQty := parseFloat(o.ExecutedQuantity)
	cumulativeQuote := parseFloat(o.CummulativeQuoteQuantity)

	// Calculate average fill price from cumulative quote / filled quantity
	var avgPrice float64
	if filledQty > 0 && cumulativeQuote > 0 {
		avgPrice = cumulativeQuote / filledQty
	}

	return &exchange.Order{
		ID:            o.OrderID,
		ClientOrderID: o.ClientOrderID,
		Symbol:        o.Symbol,
		Side:          convertSide(o.Side),
		Type:          convertOrderType(o.Type),
		Status:        convertOrderStatus(o.Status),
		Price:         parseFloat(o.Price),
		Quantity:      parseFloat(o.OrigQuantity),
		FilledQty:     filledQty,
		AvgPrice:      avgPrice,
		CreatedAt:     msToTime(o.TransactTime),
		UpdatedAt:     msToTime(o.TransactTime),
	}
}

func convertQueryOrder(o *gobinance.Order) *exchange.Order {
	filledQty := parseFloat(o.ExecutedQuantity)
	cumulativeQuote := parseFloat(o.CummulativeQuoteQuantity)

	var avgPrice float64
	if filledQty > 0 && cumulativeQuote > 0 {
		avgPrice = cumulativeQuote / filledQty
	}

	return &exchange.Order{
		ID:            o.OrderID,
		ClientOrderID: o.ClientOrderID,
		Symbol:        o.Symbol,
		Side:          convertSide(o.Side),
		Type:          convertOrderType(o.Type),
		Status:        convertOrderStatus(o.Status),
		Price:         parseFloat(o.Price),
		Quantity:      parseFloat(o.OrigQuantity),
		FilledQty:     filledQty,
		AvgPrice:      avgPrice,
		StopPrice:     parseFloat(o.StopPrice),
		CreatedAt:     msToTime(o.Time),
		UpdatedAt:     msToTime(o.UpdateTime),
	}
}

// --- Balance / Account ---

func convertBalance(b gobinance.Balance) exchange.Balance {
	return exchange.Balance{
		Asset:  b.Asset,
		Free:   parseFloat(b.Free),
		Locked: parseFloat(b.Locked),
	}
}

// --- Enum conversions ---

func convertSide(s gobinance.SideType) exchange.OrderSide {
	if s == gobinance.SideTypeBuy {
		return exchange.OrderSideBuy
	}
	return exchange.OrderSideSell
}

func toBinanceSide(s exchange.OrderSide) gobinance.SideType {
	if s == exchange.OrderSideBuy {
		return gobinance.SideTypeBuy
	}
	return gobinance.SideTypeSell
}

func convertOrderType(t gobinance.OrderType) exchange.OrderType {
	switch t {
	case gobinance.OrderTypeMarket:
		return exchange.OrderTypeMarket
	case gobinance.OrderTypeLimit:
		return exchange.OrderTypeLimit
	case gobinance.OrderTypeStopLoss:
		return exchange.OrderTypeStopLoss
	case gobinance.OrderTypeTakeProfit:
		return exchange.OrderTypeTakeProfit
	default:
		return exchange.OrderTypeMarket
	}
}

func toBinanceOrderType(t exchange.OrderType) gobinance.OrderType {
	switch t {
	case exchange.OrderTypeMarket:
		return gobinance.OrderTypeMarket
	case exchange.OrderTypeLimit:
		return gobinance.OrderTypeLimit
	case exchange.OrderTypeStopLoss:
		return gobinance.OrderTypeStopLoss
	case exchange.OrderTypeTakeProfit:
		return gobinance.OrderTypeTakeProfit
	default:
		return gobinance.OrderTypeMarket
	}
}

func convertOrderStatus(s gobinance.OrderStatusType) exchange.OrderStatus {
	switch s {
	case gobinance.OrderStatusTypeNew:
		return exchange.OrderStatusNew
	case gobinance.OrderStatusTypePartiallyFilled:
		return exchange.OrderStatusPartiallyFilled
	case gobinance.OrderStatusTypeFilled:
		return exchange.OrderStatusFilled
	case gobinance.OrderStatusTypeCanceled:
		return exchange.OrderStatusCanceled
	case gobinance.OrderStatusTypeRejected:
		return exchange.OrderStatusRejected
	case gobinance.OrderStatusTypeExpired:
		return exchange.OrderStatusExpired
	default:
		return exchange.OrderStatusNew
	}
}

// --- Helpers ---

func parseFloat(s string) float64 {
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

func msToTime(ms int64) time.Time {
	return time.Unix(0, ms*int64(time.Millisecond))
}
