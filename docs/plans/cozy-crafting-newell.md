# BTC-Trader Dashboard 实施计划 (Phase 5)

## Context

btc-trader 项目 Phase 1-3 已完成（交易引擎、Binance集成、策略、风控、仓位管理、TimescaleDB存储）。现在实施 Phase 5：搭建 Dashboard 后端 API (Gin) + Vue3 前端，实现交易系统的可视化监控。

---

## 技术选型

| 模块 | 选择 |
|------|------|
| Web 框架 | Gin + gin-contrib/cors |
| WebSocket | gorilla/websocket (已有间接依赖) |
| 前端框架 | Vue 3 + TypeScript + Vite |
| UI 库 | Element Plus |
| K线图 | TradingView Lightweight Charts |
| 状态管理 | Pinia |
| HTTP客户端 | Axios |
| 包管理 | pnpm |

---

## 实施步骤

### Step 1: Go 后端 — Dashboard Server 骨架

**新建文件：**
- `internal/web/deps.go` — DashboardDeps 依赖结构体（聚合 Store/Bus/Position/Risk/Exchange/Order/Strategy/Config）
- `internal/web/server.go` — Gin 引擎初始化、CORS、路由注册、HTTP server 生命周期管理
- `internal/web/handler/handler.go` — Handler 结构体，持有 Deps 引用
- `internal/web/handler/response.go` — 通用 JSON 响应（OK/Paginated/Error）
- `internal/web/handler/middleware.go` — zap 日志中间件 + Recovery
- `internal/web/embed.go` — go:embed 前端静态文件（构建时复制 dist/ 到此）
- `internal/web/embed_dev.go` — dev 模式空实现

**修改文件：**
- `internal/app/trader.go` — 新增 getter 方法暴露私有组件，Run() 中条件启动 Dashboard
- `go.mod` — 新增 `gin-gonic/gin`, `gin-contrib/cors`

### Step 2: Go 后端 — REST API Handlers

**新建 handler 文件（每个 handler 一个文件）：**

| 文件 | 路由 | 数据来源 |
|------|------|----------|
| `handler/overview.go` | `GET /api/v1/overview` | Exchange + Position + Risk |
| `handler/position.go` | `GET /api/v1/positions`, `GET /api/v1/positions/:symbol` | Position Manager |
| `handler/order.go` | `GET /api/v1/orders` (分页), `GET /api/v1/orders/active`, `GET /api/v1/orders/:id` | Store + Order Manager |
| `handler/trade.go` | `GET /api/v1/trades` (分页) | Store |
| `handler/signal.go` | `GET /api/v1/signals` (分页) | Store |
| `handler/snapshot.go` | `GET /api/v1/snapshots` (时间范围) | Store |
| `handler/kline.go` | `GET /api/v1/klines` | Store |
| `handler/risk.go` | `GET /api/v1/risk/status` | Risk Manager |
| `handler/strategy.go` | `GET /api/v1/strategy/status` | Strategy |
| `handler/config.go` | `GET /api/v1/config` (脱敏) | Config |
| `handler/ticker.go` | `GET /api/v1/ticker/:symbol` | Exchange |

### Step 3: Go 后端 — WebSocket 实时推送

**新建文件：**
- `internal/web/ws/message.go` — WS 消息类型定义 + 订阅请求结构
- `internal/web/ws/hub.go` — Hub 管理所有连接，按频道广播消息
- `internal/web/ws/client.go` — 单连接读写循环，维护频道订阅集合
- `internal/web/ws/bridge.go` — EventBus → WebSocket 桥接，订阅 EventBus 5 种事件转为 WS 推送

**频道设计：**
- `kline:{symbol}:{interval}` — K线实时更新
- `ticker:{symbol}` — 行情Ticker
- `signal` — 策略信号
- `order` — 订单状态变更
- `position` — 持仓变动
- `risk` — 风控告警

### Step 4: Vue3 前端 — 项目脚手架

```bash
cd web/dashboard
pnpm create vite . --template vue-ts
pnpm add vue-router@4 pinia element-plus @element-plus/icons-vue axios lightweight-charts
```

**项目结构：**
```
web/dashboard/src/
├── main.ts                    # 入口
├── App.vue                    # 根组件
├── router/index.ts            # 路由 (7 页面)
├── api/http.ts                # Axios 实例 + 拦截器
├── api/{overview,positions,orders,trades,signals,snapshots,klines,risk,config}.ts
├── stores/{overview,positions,orders,market,risk,websocket}.ts
├── composables/
│   ├── useWebSocket.ts        # WS 连接管理 + 自动重连
│   ├── usePagination.ts       # 分页复用逻辑
│   └── useChart.ts            # Lightweight Charts 封装
├── components/
│   ├── layout/{AppLayout,Sidebar,Header}.vue
│   ├── charts/{KlineChart,EquityChart,PnlChart}.vue
│   ├── common/{PaginatedTable,StatusBadge,TimeRange}.vue
│   ├── dashboard/{AccountCard,PositionCard,RecentTrades,RecentSignals}.vue
│   └── risk/{RiskGauge,RiskAlerts}.vue
├── views/
│   ├── DashboardView.vue      # 首页总览
│   ├── MarketView.vue         # K线行情
│   ├── OrdersView.vue         # 订单列表
│   ├── TradesView.vue         # 成交记录
│   ├── SignalsView.vue        # 信号历史
│   ├── RiskView.vue           # 风控面板
│   └── SettingsView.vue       # 系统配置
├── types/{api,models,ws}.ts   # 类型定义
└── utils/{format,constants}.ts
```

**vite.config.ts** 配置 proxy `/api` → `localhost:8080`

### Step 5: Vue3 前端 — Dashboard 首页 + Layout

- `AppLayout.vue` — Element Plus 侧边栏 + 顶栏 + router-view
- `Sidebar.vue` — 7 个导航菜单项
- `Header.vue` — 连接状态指示灯 + 实时 BTC 价格
- `DashboardView.vue` — 组合 AccountCard + PositionCard + RecentTrades + RecentSignals + EquityChart

### Step 6: Vue3 前端 — Market K线页

- `KlineChart.vue` — TradingView Lightweight Charts 封装
  - REST 获取历史 K 线 → `createChart()` + `addCandlestickSeries()`
  - WebSocket 订阅 `kline:BTCUSDT:5m` → `series.update()` 实时更新
  - 标注买卖信号点
- `MarketView.vue` — 品种/周期选择器 + KlineChart + Ticker 信息

### Step 7: Vue3 前端 — 列表页 (Orders/Trades/Signals)

三个页面使用统一的 `PaginatedTable` 组件：
- 筛选栏：品种、状态、时间范围
- `el-table` + `el-pagination`
- 调用分页 API，WebSocket 推送新记录时自动插入

### Step 8: Vue3 前端 — Risk 面板 + Settings

- `RiskView.vue` — 风控状态仪表 + 权益曲线 + 告警列表
- `SettingsView.vue` — 只读脱敏配置展示

### Step 9: 构建集成

**更新 Makefile：**
- `dashboard-build`: `pnpm build` → 复制 dist/ → `go build`

**更新 Dockerfile（三阶段构建）：**
- Stage 1: Node 构建前端
- Stage 2: Go 编译（嵌入前端 dist）
- Stage 3: Alpine 运行时

---

## 关键修改文件

| 文件 | 操作 | 说明 |
|------|------|------|
| `internal/app/trader.go` | 修改 | 新增 getter 方法 + Dashboard 启动逻辑 |
| `go.mod` | 修改 | 新增 gin, cors 依赖 |
| `Makefile` | 修改 | 新增 dashboard-build target |
| `deployments/docker/Dockerfile` | 修改 | 新增 Node 前端构建阶段 |
| `internal/web/**` | 新建 | 全部 Dashboard 后端代码 |
| `web/dashboard/**` | 新建 | 全部 Vue3 前端代码 |

## 验证方式

1. `go build ./cmd/trader` 编译通过
2. 启动 trader（配置 dashboard.enabled=true），访问 `http://localhost:8080/api/v1/overview` 返回 JSON
3. WebSocket 连接 `ws://localhost:8080/api/v1/ws`，订阅频道后收到推送
4. `cd web/dashboard && pnpm dev` 启动前端，通过 proxy 对接后端 API
5. 浏览器访问 `http://localhost:5173` 查看完整 Dashboard
6. `go test -race ./internal/web/...` 测试通过
