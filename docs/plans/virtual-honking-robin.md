# BTC-Trader 后续功能实施计划

## Context

Phase 1-3（交易引擎）和 Phase 5（Dashboard）已完成。现在按优先级实施剩余功能：更多策略 → 止损止盈自动管理 → 回测引擎。

---

## Step 1: 提取策略公共 helpers

**新建** `internal/strategy/trend/helpers.go` (~50 行)

从 `ema_cross.go` 提取 `toInt`、`toFloat`、`toBool`、`clamp` 四个包级函数，三个策略共用，避免重复。

---

## Step 2: MACD+RSI 组合策略

**新建** `internal/strategy/trend/macd_rsi.go` (~200 行)

- 买入：MACD 柱状图从负转正（零轴上穿）+ RSI 在 30-70 之间
- 卖出：MACD 柱状图从正转负（零轴下穿）+ RSI 在 30-70 之间
- 参数：fast_period(12), slow_period(26), signal_period(9), rsi_period(14)
- 复用已有 `indicator.ComputeMACD()` 和 `ComputeRSI()`

**新建** `internal/strategy/trend/macd_rsi_test.go` (~100 行)

---

## Step 3: 布林带突破策略

**新建** `internal/strategy/trend/bb_breakout.go` (~200 行)

- 买入：收盘价突破上轨 + 成交量 > 20 周期均量
- 卖出：收盘价跌破下轨
- 参数：bb_period(20), bb_mult(2), rsi_period(14), volume_period(20)
- 复用已有 `indicator.ComputeBollingerBands()`

**新建** `internal/strategy/trend/bb_breakout_test.go` (~100 行)

---

## Step 4: 策略注册表接入

**修改** `internal/app/trader.go` `createStrategy()` 函数

把 switch-case 改为使用已有的 `strategy.Registry`：
```go
reg.Register("ema_crossover", ...)
reg.Register("macd_rsi", ...)
reg.Register("bb_breakout", ...)
return reg.Create(cfg.Name, cfg.Config)
```

---

## Step 5: 止损/止盈自动管理

**修改** `internal/order/manager.go` (~150 行新增)

- `NewManager` 接收 `config.RiskConfig` 参数
- 买单成交后自动挂 SL/TP 单（`placeProtectiveOrders`）
- 仓位平掉后自动撤销 SL/TP（`cancelProtectiveOrders`）
- SL 触发时自动撤 TP，反之亦然

**新增追踪止损** `UpdateTrailingStop(ctx, symbol, price)`：
- 价格上行时动态抬高止损价
- 撤旧 SL 挂新 SL

**修改** `internal/app/trader.go` K线循环中调用 `UpdateTrailingStop`

---

## Step 6: 模拟交易所

**新建** `internal/exchange/simulated/exchange.go` (~400 行)

实现 `exchange.Exchange` 接口，用于回测：
- 内存维护余额、订单簿
- `OnKline(k)` 推进时间，检查 pending 限价/止损单是否触发
- 市价单按当前 K 线 Close ± slippage 立即成交
- 手续费扣减、余额增减

---

## Step 7: 回测结果与绩效指标

**新建** `internal/backtest/result.go` (~200 行)

- `Result` 结构体：总收益、Sharpe、最大回撤、胜率、盈亏比、ProfitFactor
- `EquitySnapshot` / `TradeResult` 序列数据
- `PrintSummary(w)` 格式化输出

---

## Step 8: 回测引擎

**新建** `internal/backtest/engine.go` (~300 行)

核心流程：
1. 从 DB 加载历史 K 线
2. 逐根回放 → simulated.Exchange.OnKline()
3. 检查订单成交 → position.Manager 更新
4. 积累足够历史后调用 strategy.Evaluate()
5. 信号 → simulated.Exchange.PlaceOrder()
6. 记录权益快照 → 计算绩效

---

## Step 9: 回测 CLI

**改写** `cmd/backtester/main.go` (~100 行)

```bash
./bin/backtester -config configs/config.yaml \
    -symbol BTCUSDT -interval 5m \
    -start 2025-01-01 -end 2025-12-31 \
    -capital 10000 -strategy ema_crossover
```

连接 DB 读 K 线 → 创建策略 → 运行引擎 → 输出报告

---

## 文件清单

| 操作 | 文件 | 预估行数 |
|------|------|---------|
| 新建 | `internal/strategy/trend/helpers.go` | ~50 |
| 新建 | `internal/strategy/trend/macd_rsi.go` | ~200 |
| 新建 | `internal/strategy/trend/macd_rsi_test.go` | ~100 |
| 新建 | `internal/strategy/trend/bb_breakout.go` | ~200 |
| 新建 | `internal/strategy/trend/bb_breakout_test.go` | ~100 |
| 修改 | `internal/app/trader.go` | ~30 行变更 |
| 修改 | `internal/order/manager.go` | ~150 行新增 |
| 新建 | `internal/exchange/simulated/exchange.go` | ~400 |
| 新建 | `internal/backtest/result.go` | ~200 |
| 新建 | `internal/backtest/engine.go` | ~300 |
| 改写 | `cmd/backtester/main.go` | ~100 |

**总计新增约 ~1800 行**

---

## 验证方式

1. `go build ./...` 编译通过
2. `go test -race ./internal/strategy/...` 策略单元测试通过
3. 切换 `config.yaml` 的 `strategy.name` 为 `macd_rsi` 或 `bb_breakout`，启动 trader 确认信号生成
4. `./bin/backtester -symbol BTCUSDT -interval 5m -start 2025-06-01 -end 2025-12-31` 输出回测报告
5. 实盘 paper 模式下买入后确认自动挂 SL/TP 单（日志可见）
