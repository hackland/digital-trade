# btc-trader

币安 BTC 现货自动交易系统。Go 1.24 后端 + Vue 3 前端 + TimescaleDB。

## 常用命令

```bash
# 后端
make build          # 编译所有二进制
make run            # 编译并启动交易引擎
make test           # 运行测试（含 -race）
make migrate        # 数据库迁移
make docker-up      # 启动 TimescaleDB

# 前端 (Vue3 Dashboard)
cd web/dashboard && pnpm dev    # 开发服务器 :5199
make frontend-build             # 构建

# 工具命令
make backtest       # 回测
make klinebackfill  # 拉取1年K线到DB

# 回测/优化工具 (各有独立 cmd)
go run ./cmd/shorttrend   # 做空趋势分析
go run ./cmd/shortopt     # 做空参数优化
go run ./cmd/btcompare    # 策略对比
```

## 架构要点

- **端口**: 后端 API `:9090`，前端 `:5199`
- **事件总线**: 组件间通信全走 `internal/eventbus`，不直接调用
- **存储**: K线用 TimescaleDB COPY 批量写入，非逐行 INSERT
- **策略**: 实现 `Strategy` 接口注册到 `internal/strategy/registry`，不改引擎主逻辑
- **WebSocket 重连**: 指数退避（base 1s, max 1min），已内置，不要重复实现

## 关键模块

| 路径 | 职责 |
|------|------|
| `internal/app/trader.go` | 主编排器，启动所有协程 |
| `internal/exchange/binance/` | Binance REST + WebSocket |
| `internal/strategy/` | 策略接口 + 注册表 |
| `internal/risk/manager.go` | 风控（日亏损/回撤/频率） |
| `internal/storage/timescale/` | TimescaleDB 5个 Repository |
| `internal/web/` | Dashboard REST + WS API |
| `web/dashboard/` | Vue3 前端 |
| `cmd/` | 各独立工具入口 |

## 做空策略

默认权重 **MACD 60% + EMA 20% + MFI 20%**，做空阈值 -0.25 / 平空 0.15。
已回测确认此组合最优，**不要建议替换 MFI**。

## 注意事项

- 实盘模式修改前确认 `config.yaml` 中 `app.mode`
- API Key 走环境变量 `BINANCE_API_KEY` / `BINANCE_SECRET_KEY`，不写入配置文件
- 风控参数有业务含义，不随意调整默认值
