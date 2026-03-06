package position

import (
	"testing"

	"github.com/jayce/btc-trader/internal/exchange"
	"go.uber.org/zap"
)

func newTestManager() *Manager {
	logger, _ := zap.NewDevelopment()
	return NewManager(logger)
}

func TestManager_InitialState(t *testing.T) {
	m := newTestManager()
	pos := m.GetPosition("BTCUSDT")
	if pos.Side != "FLAT" {
		t.Errorf("initial side = %s, want FLAT", pos.Side)
	}
	if pos.Quantity != 0 {
		t.Errorf("initial quantity = %.4f, want 0", pos.Quantity)
	}
}

func TestManager_BuyTrade(t *testing.T) {
	m := newTestManager()

	m.OnTrade(&exchange.Trade{
		Symbol:   "BTCUSDT",
		Side:     exchange.OrderSideBuy,
		Price:    50000,
		Quantity: 0.1,
	})

	pos := m.GetPosition("BTCUSDT")
	if pos.Side != "LONG" {
		t.Errorf("side = %s, want LONG", pos.Side)
	}
	if pos.Quantity != 0.1 {
		t.Errorf("quantity = %.4f, want 0.1", pos.Quantity)
	}
	if pos.AvgEntryPrice != 50000 {
		t.Errorf("avg entry = %.2f, want 50000", pos.AvgEntryPrice)
	}
}

func TestManager_BuyAndSell(t *testing.T) {
	m := newTestManager()

	// Buy 0.1 @ 50000
	m.OnTrade(&exchange.Trade{
		Symbol:   "BTCUSDT",
		Side:     exchange.OrderSideBuy,
		Price:    50000,
		Quantity: 0.1,
	})

	// Sell 0.1 @ 51000 (profit = 100 USDT)
	m.OnTrade(&exchange.Trade{
		Symbol:   "BTCUSDT",
		Side:     exchange.OrderSideSell,
		Price:    51000,
		Quantity: 0.1,
	})

	pos := m.GetPosition("BTCUSDT")
	if pos.Side != "FLAT" {
		t.Errorf("side = %s, want FLAT", pos.Side)
	}
	if pos.Quantity != 0 {
		t.Errorf("quantity = %.4f, want 0", pos.Quantity)
	}

	expectedPnL := (51000 - 50000) * 0.1
	if pos.RealizedPnL != expectedPnL {
		t.Errorf("realized PnL = %.2f, want %.2f", pos.RealizedPnL, expectedPnL)
	}
}

func TestManager_AveragingUp(t *testing.T) {
	m := newTestManager()

	// Buy 0.1 @ 50000
	m.OnTrade(&exchange.Trade{
		Symbol:   "BTCUSDT",
		Side:     exchange.OrderSideBuy,
		Price:    50000,
		Quantity: 0.1,
	})

	// Buy 0.1 @ 52000
	m.OnTrade(&exchange.Trade{
		Symbol:   "BTCUSDT",
		Side:     exchange.OrderSideBuy,
		Price:    52000,
		Quantity: 0.1,
	})

	pos := m.GetPosition("BTCUSDT")
	if pos.Quantity != 0.2 {
		t.Errorf("quantity = %.4f, want 0.2", pos.Quantity)
	}

	expectedAvg := (50000*0.1 + 52000*0.1) / 0.2
	if pos.AvgEntryPrice != expectedAvg {
		t.Errorf("avg entry = %.2f, want %.2f", pos.AvgEntryPrice, expectedAvg)
	}
}

func TestManager_PartialSell(t *testing.T) {
	m := newTestManager()

	// Buy 0.2 @ 50000
	m.OnTrade(&exchange.Trade{
		Symbol:   "BTCUSDT",
		Side:     exchange.OrderSideBuy,
		Price:    50000,
		Quantity: 0.2,
	})

	// Sell 0.1 @ 51000
	m.OnTrade(&exchange.Trade{
		Symbol:   "BTCUSDT",
		Side:     exchange.OrderSideSell,
		Price:    51000,
		Quantity: 0.1,
	})

	pos := m.GetPosition("BTCUSDT")
	if pos.Side != "LONG" {
		t.Errorf("side = %s, want LONG", pos.Side)
	}
	if pos.Quantity != 0.1 {
		t.Errorf("remaining qty = %.4f, want 0.1", pos.Quantity)
	}
	if pos.AvgEntryPrice != 50000 {
		t.Errorf("avg entry should remain %.2f, got %.2f", 50000.0, pos.AvgEntryPrice)
	}

	expectedPnL := (51000 - 50000) * 0.1
	if pos.RealizedPnL != expectedPnL {
		t.Errorf("realized PnL = %.2f, want %.2f", pos.RealizedPnL, expectedPnL)
	}
}

func TestManager_UpdatePrice(t *testing.T) {
	m := newTestManager()

	// Open position
	m.OnTrade(&exchange.Trade{
		Symbol:   "BTCUSDT",
		Side:     exchange.OrderSideBuy,
		Price:    50000,
		Quantity: 0.1,
	})

	// Update price to 51000
	m.UpdatePrice("BTCUSDT", 51000)

	pos := m.GetPosition("BTCUSDT")
	expectedUnrealizedPnL := (51000 - 50000) * 0.1
	if pos.UnrealizedPnL != expectedUnrealizedPnL {
		t.Errorf("unrealized PnL = %.2f, want %.2f", pos.UnrealizedPnL, expectedUnrealizedPnL)
	}
}

func TestManager_MultiSymbol(t *testing.T) {
	m := newTestManager()

	m.OnTrade(&exchange.Trade{
		Symbol: "BTCUSDT", Side: exchange.OrderSideBuy, Price: 50000, Quantity: 0.1,
	})
	m.OnTrade(&exchange.Trade{
		Symbol: "ETHUSDT", Side: exchange.OrderSideBuy, Price: 3000, Quantity: 1.0,
	})

	all := m.GetAllPositions()
	if len(all) != 2 {
		t.Errorf("positions count = %d, want 2", len(all))
	}

	btc := m.GetPosition("BTCUSDT")
	eth := m.GetPosition("ETHUSDT")

	if btc.Quantity != 0.1 {
		t.Errorf("BTC qty = %.4f, want 0.1", btc.Quantity)
	}
	if eth.Quantity != 1.0 {
		t.Errorf("ETH qty = %.4f, want 1.0", eth.Quantity)
	}
}

func TestManager_TotalPnL(t *testing.T) {
	m := newTestManager()

	// Open BTC and ETH
	m.OnTrade(&exchange.Trade{
		Symbol: "BTCUSDT", Side: exchange.OrderSideBuy, Price: 50000, Quantity: 0.1,
	})
	m.OnTrade(&exchange.Trade{
		Symbol: "ETHUSDT", Side: exchange.OrderSideBuy, Price: 3000, Quantity: 1.0,
	})

	m.UpdatePrice("BTCUSDT", 51000)
	m.UpdatePrice("ETHUSDT", 3100)

	totalUnrealized := m.TotalUnrealizedPnL()
	expected := (51000-50000)*0.1 + (3100-3000)*1.0
	if totalUnrealized != expected {
		t.Errorf("total unrealized PnL = %.2f, want %.2f", totalUnrealized, expected)
	}
}
