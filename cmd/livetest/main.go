package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/jayce/btc-trader/internal/config"
	"github.com/jayce/btc-trader/internal/exchange"
	"github.com/jayce/btc-trader/internal/exchange/binance"
	"go.uber.org/zap"
)

func main() {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	cfg, err := config.Load("configs/config.yaml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := binance.NewClient(cfg.Exchange.APIKey, cfg.Exchange.SecretKey, cfg.App.Testnet, logger)

	// If "buybnb" argument is passed, buy $50 of BNB and exit
	if len(os.Args) > 1 && os.Args[1] == "buybnb" {
		buyBNB(ctx, client)
		return
	}

	// Step 1: 获取账户余额
	fmt.Println("=== Step 1: 查询账户余额 ===")
	bal, err := client.GetBalance(ctx, "USDT")
	if err != nil {
		fmt.Fprintf(os.Stderr, "get balance: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("USDT: Free=%.2f, Locked=%.2f\n", bal.Free, bal.Locked)

	if bal.Free < 100 {
		fmt.Fprintf(os.Stderr, "余额不足 100 USDT (当前 %.2f)，无法测试\n", bal.Free)
		os.Exit(1)
	}

	// Step 2: 获取当前价格
	fmt.Println("\n=== Step 2: 获取 BTCUSDT 价格 ===")
	ticker, err := client.GetTicker(ctx, "BTCUSDT")
	if err != nil {
		fmt.Fprintf(os.Stderr, "get ticker: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("BTC Price: Bid=%.2f, Ask=%.2f, Last=%.2f\n", ticker.BidPrice, ticker.AskPrice, ticker.LastPrice)

	// Step 3: 获取交易规则
	fmt.Println("\n=== Step 3: 获取交易规则 ===")
	info, err := client.GetExchangeInfo(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "get exchange info: %v\n", err)
		os.Exit(1)
	}

	var si exchange.SymbolInfo
	for _, s := range info.Symbols {
		if s.Symbol == "BTCUSDT" {
			si = s
			break
		}
	}
	fmt.Printf("StepSize=%.8f, MinQty=%.8f, MinNotional=%.2f\n", si.StepSize, si.MinQty, si.MinNotional)

	// Step 4: 计算下单数量 ($100)
	fmt.Println("\n=== Step 4: 计算下单数量 ===")
	allocUSDT := 100.0
	feeReserve := 1.001 // 预留 0.1% 手续费
	rawQty := allocUSDT / (ticker.AskPrice * feeReserve)

	// 按 stepSize 截断
	steps := math.Floor(rawQty / si.StepSize)
	qty := steps * si.StepSize

	fmt.Printf("分配 USDT: %.2f\n", allocUSDT)
	fmt.Printf("原始数量: %.10f\n", rawQty)
	fmt.Printf("截断后数量: %.8f (步进=%.8f)\n", qty, si.StepSize)
	fmt.Printf("订单价值: %.2f USDT\n", qty*ticker.AskPrice)

	if qty < si.MinQty {
		fmt.Fprintf(os.Stderr, "数量 %.8f 低于最小值 %.8f\n", qty, si.MinQty)
		os.Exit(1)
	}
	if si.MinNotional > 0 && qty*ticker.AskPrice < si.MinNotional {
		fmt.Fprintf(os.Stderr, "订单价值 %.2f 低于最小名义值 %.2f\n", qty*ticker.AskPrice, si.MinNotional)
		os.Exit(1)
	}

	// Check BTC balance first
	btcBal, _ := client.GetBalance(ctx, "BTC")
	fmt.Printf("\nBTC余额: Free=%.8f, Locked=%.8f\n", btcBal.Free, btcBal.Locked)

	// If we already have BTC from a previous failed test, just sell it
	if btcBal.Free > si.MinQty {
		fmt.Println("\n=== 检测到已有 BTC 持仓，直接卖出 ===")
		sellQty := math.Floor(btcBal.Free/si.StepSize) * si.StepSize
		fmt.Printf("卖出数量: %.8f BTC\n", sellQty)
		sellOrder, sellErr := client.PlaceOrder(ctx, exchange.OrderRequest{
			Symbol:   "BTCUSDT",
			Side:     exchange.OrderSideSell,
			Type:     exchange.OrderTypeMarket,
			Quantity: sellQty,
		})
		if sellErr != nil {
			fmt.Fprintf(os.Stderr, "清仓卖出失败: %v\n", sellErr)
		} else {
			fmt.Printf("清仓成功! OrderID=%d Status=%s\n", sellOrder.ID, sellOrder.Status.String())
		}
		// Continue with the buy test
	}

	// Refresh balance
	time.Sleep(time.Second)
	bal, _ = client.GetBalance(ctx, "USDT")
	fmt.Printf("刷新 USDT余额: %.2f\n", bal.Free)

	// Step 5: 下市价买单
	fmt.Println("\n=== Step 5: 下市价买单 ===")
	buyOrder, err := client.PlaceOrder(ctx, exchange.OrderRequest{
		Symbol:   "BTCUSDT",
		Side:     exchange.OrderSideBuy,
		Type:     exchange.OrderTypeMarket,
		Quantity: qty,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "买单失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("买单成功!\n")
	fmt.Printf("  OrderID:  %d\n", buyOrder.ID)
	fmt.Printf("  Status:   %s\n", buyOrder.Status.String())
	fmt.Printf("  成交数量: %.8f BTC\n", buyOrder.FilledQty)
	fmt.Printf("  成交均价: %.2f\n", buyOrder.AvgPrice)
	fmt.Printf("  成交金额: %.2f USDT\n", buyOrder.FilledQty*buyOrder.AvgPrice)

	if buyOrder.Status != exchange.OrderStatusFilled {
		fmt.Println("买单未完全成交，跳过卖出")
		os.Exit(0)
	}

	// Step 6: 等2秒，然后卖出
	fmt.Println("\n=== Step 6: 等待2秒后卖出 ===")
	time.Sleep(2 * time.Second)

	// 查询实际 BTC 余额（手续费会从BTC扣，所以实际到账 < FilledQty）
	btcBal2, _ := client.GetBalance(ctx, "BTC")
	fmt.Printf("实际 BTC 余额: %.8f (买入成交 %.8f, 差额=手续费)\n", btcBal2.Free, buyOrder.FilledQty)

	// 用实际余额来卖，按 stepSize 截断
	sellQty := math.Floor(btcBal2.Free/si.StepSize) * si.StepSize

	sellOrder, err := client.PlaceOrder(ctx, exchange.OrderRequest{
		Symbol:   "BTCUSDT",
		Side:     exchange.OrderSideSell,
		Type:     exchange.OrderTypeMarket,
		Quantity: sellQty,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "卖单失败: %v\n", err)
		fmt.Println("⚠️ 买入的 BTC 仍在账户中，请手动处理")
		os.Exit(1)
	}

	fmt.Printf("卖单成功!\n")
	fmt.Printf("  OrderID:  %d\n", sellOrder.ID)
	fmt.Printf("  Status:   %s\n", sellOrder.Status.String())
	fmt.Printf("  成交数量: %.8f BTC\n", sellOrder.FilledQty)
	fmt.Printf("  成交均价: %.2f\n", sellOrder.AvgPrice)
	fmt.Printf("  成交金额: %.2f USDT\n", sellOrder.FilledQty*sellOrder.AvgPrice)

	// Step 7: 总结
	fmt.Println("\n=== 测试结果 ===")
	buyCost := buyOrder.FilledQty * buyOrder.AvgPrice
	sellRevenue := sellOrder.FilledQty * sellOrder.AvgPrice
	pnl := sellRevenue - buyCost
	fmt.Printf("买入花费: %.4f USDT\n", buyCost)
	fmt.Printf("卖出收入: %.4f USDT\n", sellRevenue)
	fmt.Printf("差额(含手续费): %.4f USDT\n", pnl)
	fmt.Println("\n✅ 实盘下单测试通过！代码可以正常与币安交互。")
}

func buyBNB(ctx context.Context, client *binance.Client) {
	fmt.Println("=== 买入 $50 BNB ===")

	// 查余额
	bal, err := client.GetBalance(ctx, "USDT")
	if err != nil {
		fmt.Fprintf(os.Stderr, "get balance: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("USDT余额: %.2f\n", bal.Free)

	// 获取 BNBUSDT 价格
	ticker, err := client.GetTicker(ctx, "BNBUSDT")
	if err != nil {
		fmt.Fprintf(os.Stderr, "get ticker: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("BNB价格: %.2f\n", ticker.AskPrice)

	// 获取交易规则
	info, err := client.GetExchangeInfo(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "get exchange info: %v\n", err)
		os.Exit(1)
	}
	var si exchange.SymbolInfo
	for _, s := range info.Symbols {
		if s.Symbol == "BNBUSDT" {
			si = s
			break
		}
	}
	fmt.Printf("StepSize=%.8f, MinQty=%.8f\n", si.StepSize, si.MinQty)

	// 计算数量
	allocUSDT := 50.0
	rawQty := allocUSDT / (ticker.AskPrice * 1.001)
	qty := math.Floor(rawQty/si.StepSize) * si.StepSize
	fmt.Printf("下单数量: %.4f BNB (≈$%.2f)\n", qty, qty*ticker.AskPrice)

	// 下单
	order, err := client.PlaceOrder(ctx, exchange.OrderRequest{
		Symbol:   "BNBUSDT",
		Side:     exchange.OrderSideBuy,
		Type:     exchange.OrderTypeMarket,
		Quantity: qty,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "买入BNB失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n✅ BNB买入成功!\n")
	fmt.Printf("  OrderID: %d\n", order.ID)
	fmt.Printf("  Status:  %s\n", order.Status.String())
	fmt.Printf("  数量:    %.4f BNB\n", order.FilledQty)
	fmt.Printf("  均价:    %.2f USDT\n", order.AvgPrice)
	fmt.Printf("  花费:    %.2f USDT\n", order.FilledQty*order.AvgPrice)

	// 查看最终BNB余额
	bnbBal, _ := client.GetBalance(ctx, "BNB")
	fmt.Printf("\nBNB余额: %.4f\n", bnbBal.Free)
	fmt.Println("\n请到币安App开启「使用BNB支付手续费」")
}
