# BTC 趋势跟踪交易系统 - 实现方案

## Context

构建一个基于币安的现货趋势跟踪交易程序。Go 为主语言，TimescaleDB 存储时序数据。系统需包含：实时行情 WebSocket、风控系统、回测引擎、Web 管理面板。支持 **BTCUSDT + ETHUSDT** 现货交易，架构预留合约扩展及更多币种能力。

---

## 项目结构

```
btc-trader/
├── cmd/
│   ├── trader/main.go           # 实盘交易入口
│   ├── backtester/main.go       # 回测 CLI
│   └── dashboard/main.go        # 可选独立 Dashboard
├── internal/
│   ├── app/                     # 应用编排与生命周期
│   ├── config/                  # 配置加载 (YAML + env)
│   ├── exchange/                # 交易所抽象层
│   │   ├── exchange.go          # Exchange 接口定义
│   │   ├── types.go             # Kline, Order, Trade 等核心类型
│   │   ├── binance/             # 币安实现 (REST + WS)
│   │   └── simulated/           # 回测模拟交易所
│   ├── market/                  # 行情服务 + 指标计算
│   ├── strategy/                # 策略引擎
│   │   ├── strategy.go          # Strategy 接口
│   │   └── trend/               # EMA交叉、MACD+RSI、布林突破
│   ├── order/                   # 订单管理 & 执行
│   ├── position/                # 仓位管理 & 盈亏计算
│   ├── risk/                    # 风控 (盘前检查 + 盘后监控)
│   ├── backtest/                # 回测引擎
│   ├── storage/                 # 数据库层 (TimescaleDB)
│   ├── eventbus/                # 内部事件系统 (typed channels)
│   └── web/                     # Dashboard HTTP API + WS 推送
├── web/dashboard/               # Vue 3 + Element Plus 管理系统 (go:embed 嵌入)
├── configs/config.example.yaml
├── deployments/docker/
│   ├── Dockerfile
│   └── docker-compose.yml
├── go.mod
└── Makefile
```

---

## 核心架构

**事件驱动 + Go channels**，每个组件独立 goroutine 通过 EventBus 通信。

```
Binance WS → Exchange Client → EventBus → Market Data Service (指标计算)
                                              ↓
                                         Strategy Engine (信号生成)
                                              ↓
                                         Risk Manager (盘前检查)
                                              ↓
                                         Order Manager (下单执行)
                                              ↓
                                         Position Manager (仓位/PnL)
                                              ↓
                                         Web Dashboard (实时推送)
```

**数据流**: 行情 → 指标 → 策略信号 → 风控过滤 → 下单 → 成交 → 仓位更新 → 盘后风控

---

## 关键接口设计

### 1. Exchange 接口
- `GetAccount`, `GetBalance` - 账户信息
- `GetKlines`, `GetOrderBook` - 行情 REST
- `PlaceOrder`, `CancelOrder`, `GetOpenOrders` - 订单操作
- `SubscribeKlines`, `SubscribeDepth`, `SubscribeUserData` - WebSocket 流
- 实现: `binance.Client` (实盘) / `simulated.Exchange` (回测)

### 2. Strategy 接口
- `Name()` - 策略名
- `RequiredIndicators()` - 声明需要的指标
- `RequiredHistory()` - 最少历史K线数
- `Evaluate(ctx, snapshot) → Signal` - 核心决策（同时用于实盘和回测）
- `OnTradeExecuted(trade)` - 成交回调

### 3. RiskManager 接口
- `PreTradeCheck(req, signal) → RiskDecision` - 下单前校验
- `PostTradeCheck(trade)` - 成交后监控
- `ContinuousMonitor(ctx)` - 持续风控循环

### 风控参数
- 最大仓位 (BTC / USDT / 账户百分比)
- 止损止盈 (固定% / 跟踪止损)
- 日亏损上限、日交易次数上限
- 最大回撤触发冷却期

---

## 技术选型

| 模块 | 选择 | 原因 |
|------|------|------|
| 币安 SDK | `adshao/go-binance` v2 | MIT, Spot+Futures, 成熟稳定 |
| 数据库 | TimescaleDB (PG 扩展) | 时序优化、连续聚合、自动压缩 |
| SQL 层 | `pgx` v5 + `sqlx` | 性能优先，无 ORM 开销 |
| Web 框架 | Gin | 性能/生态平衡 |
| 配置 | Viper (YAML + env) | 标准方案 |
| 日志 | `uber-go/zap` | 结构化、低延迟 |
| 技术指标 | 自研 9个: SMA/EMA/MACD/RSI/BB/ATR + OBV/MFI/VWAP | 避免 AGPL 许可证问题 |
| 精度 | `shopspring/decimal` (订单) / float64 (指标) | 关键场景精度 |
| 前端 | Vue 3 + Element Plus + TradingView Lightweight Charts | 管理系统 + 专业K线图 |
| 迁移 | golang-migrate | 文件式 SQL 迁移 |

---

## 数据库 Schema (TimescaleDB)

- **klines** - hypertable, 7天自动压缩, 90天保留策略
- **klines_1h** - 连续聚合物化视图 (从1m自动聚合)
- **orders** - 订单历史
- **trades** - hypertable, 成交记录
- **account_snapshots** - hypertable, 权益快照 (每5分钟)
- **signals** - hypertable, 策略信号审计

---

## 实现分阶段

### Phase 1: 基础骨架
1. 项目脚手架 (go mod, 目录, Makefile, Docker)
2. 配置模块 (Viper YAML + env)
3. 核心类型定义 (`exchange/types.go`)
4. Exchange 接口 + 币安 REST/WS 实现
5. TimescaleDB 迁移 + KlineRepository
6. WebSocket 行情接入 + K线入库

**里程碑**: docker-compose up 后 BTCUSDT + ETHUSDT 1m K线持续入库

### Phase 2: 指标 + 策略
7. 9 个技术指标实现 + 单元测试 (价格类: SMA/EMA/MACD/RSI/BB/ATR, 量价类: OBV/MFI/VWAP)
8. Market Data Service (指标计算管道)
9. Strategy 接口 + EMA交叉策略
10. EventBus (typed channels)
11. Signal 持久化

**里程碑**: 策略产出信号并持久化，与 TradingView 对比验证

### Phase 3: 订单 + 仓位 + 风控
12. Position Manager
13. Risk Manager (盘前+盘后)
14. Order Manager + UserDataStream 跟踪
15. 账户权益快照

**里程碑**: Testnet 上完整交易闭环

### Phase 4: 回测引擎
16. 历史数据加载器
17. 模拟交易所实现
18. 回测引擎主循环
19. 绩效指标 (Sharpe/MaxDD/WinRate/ProfitFactor)
20. 回测 CLI

**里程碑**: 1年 BTCUSDT 5m 数据回测报告

### Phase 5: Web 管理系统
21. Gin HTTP API (status/PnL/trades/orders/risk/strategy/settings)
22. WebSocket 实时推送 (行情/成交/PnL/风控告警)
23. Vue 3 + Element Plus 管理系统前端:
    - 仪表盘: 账户总览、当日PnL、持仓概览、运行状态
    - 实时行情: TradingView Lightweight Charts K线 + 指标叠加 + 成交标记
    - 交易记录: 历史成交表格 (筛选/分页/导出)
    - 订单管理: 当前挂单、历史订单、手动撤单
    - 仓位管理: 当前持仓、未实现盈亏、手动平仓
    - 策略管理: 策略列表、启停控制、参数配置、信号日志
    - 风控中心: 风控参数配置、状态监控、触发历史
    - 回测报告: 权益曲线、绩效指标对比
    - 系统设置: 交易所配置、通知设置、系统日志
24. go:embed 嵌入编译后的前端资源，单二进制部署

**里程碑**: 浏览器访问完整管理系统，查看实时行情、交易记录、策略管理、手动干预

### Phase 6: 加固
25. 更多策略 (MACD+RSI, 布林突破, 多时间框架)
26. Telegram/Discord 告警
27. Prometheus 监控指标
28. 合约支持扩展

---

## 关键运维考虑

- **API 限流**: `golang.org/x/time/rate` 实现，按 endpoint weight 控制
- **WS 重连**: 指数退避 (1s→60s)，23h 主动重连 (避免24h断连)
- **优雅关机**: context 取消 → 停止策略 → 撤销挂单 → 关WS → flush DB
- **测试流程**: Testnet → Paper Trading (真行情+模拟下单) → 小仓实盘 → 逐步加仓

---

## 验证方式

1. `docker-compose up` 启动 TimescaleDB + trader
2. 检查日志确认 K线入库
3. 检查 signals 表确认策略信号产出
4. Testnet 下单验证完整交易闭环
5. 回测 CLI 输出绩效报告
6. 浏览器访问 :8080 查看 Dashboard
7. `go test -race ./...` 全量测试
