#!/usr/bin/env python3
"""Backtest comparison script - runs multiple configs and prints a summary table."""
import json
import urllib.request

BASE = "http://localhost:9090/api/v1/backtest"
COMMON = {
    "symbol": "BTCUSDT",
    "interval": "1h",
    "strategy": "custom_weighted",
    "days": 365,
    "cash": 10000,
    "alloc": 1.0,
    "fee": 0,
}
SIGNAL = {
    "buy_threshold": 0.15,
    "sell_threshold": -0.5,
    "confirm_bars": 1,
    "cooldown_bars": 2,
    "min_hold_bars": 6,
    "atr_stop_mult": 3.0,
    "trend_filter": True,
    "trend_period": 50,
}

CONFIGS = [
    ("默认6模块", [
        {"name": "ema_cross", "weight": 0.25, "params": {"fast_period": 9, "slow_period": 21}},
        {"name": "macd", "weight": 0.20, "params": {"fast": 12, "slow": 26, "signal": 9}},
        {"name": "rsi", "weight": 0.15, "params": {"period": 14}},
        {"name": "mfi", "weight": 0.10, "params": {"period": 14}},
        {"name": "volume_ratio", "weight": 0.15, "params": {"period": 20}},
        {"name": "cmf", "weight": 0.15, "params": {"period": 20}},
    ]),
    ("精简2模块(EMA+MACD)", [
        {"name": "ema_cross", "weight": 0.60, "params": {"fast_period": 9, "slow_period": 21}},
        {"name": "macd", "weight": 0.40, "params": {"fast": 12, "slow": 26, "signal": 9}},
    ]),
    ("趋势3(EMA+MACD+SMA)", [
        {"name": "ema_cross", "weight": 0.40, "params": {"fast_period": 9, "slow_period": 21}},
        {"name": "macd", "weight": 0.35, "params": {"fast": 12, "slow": 26, "signal": 9}},
        {"name": "sma_trend", "weight": 0.25, "params": {"period": 50}},
    ]),
    ("趋势+动量(EMA+MACD+RSI)", [
        {"name": "ema_cross", "weight": 0.40, "params": {"fast_period": 9, "slow_period": 21}},
        {"name": "macd", "weight": 0.30, "params": {"fast": 12, "slow": 26, "signal": 9}},
        {"name": "rsi", "weight": 0.30, "params": {"period": 14}},
    ]),
    ("趋势+量能(EMA+MACD+VOL)", [
        {"name": "ema_cross", "weight": 0.40, "params": {"fast_period": 9, "slow_period": 21}},
        {"name": "macd", "weight": 0.35, "params": {"fast": 12, "slow": 26, "signal": 9}},
        {"name": "volume_ratio", "weight": 0.25, "params": {"period": 20}},
    ]),
    ("趋势+KDJ(EMA+MACD+KDJ)", [
        {"name": "ema_cross", "weight": 0.35, "params": {"fast_period": 9, "slow_period": 21}},
        {"name": "macd", "weight": 0.35, "params": {"fast": 12, "slow": 26, "signal": 9}},
        {"name": "kdj", "weight": 0.30, "params": {"period": 9, "k_smooth": 3, "d_smooth": 3}},
    ]),
    ("BB反转(EMA+BB+RSI)", [
        {"name": "ema_cross", "weight": 0.35, "params": {"fast_period": 9, "slow_period": 21}},
        {"name": "bb_position", "weight": 0.35, "params": {"period": 20, "mult": 2}},
        {"name": "rsi", "weight": 0.30, "params": {"period": 14}},
    ]),
    ("资金流(EMA+MFI+CMF)", [
        {"name": "ema_cross", "weight": 0.40, "params": {"fast_period": 9, "slow_period": 21}},
        {"name": "mfi", "weight": 0.30, "params": {"period": 14}},
        {"name": "cmf", "weight": 0.30, "params": {"period": 20}},
    ]),
]


def run_bt(label, modules):
    cfg = {**COMMON, "strategy_config": {**SIGNAL, "modules": modules}}
    data = json.dumps(cfg).encode()
    req = urllib.request.Request(BASE, data=data, headers={"Content-Type": "application/json"})
    try:
        resp = urllib.request.urlopen(req, timeout=30)
        r = json.loads(resp.read())
    except Exception as e:
        return f"{label:30s} | ERROR: {e}"

    d = r.get("data", r)
    m = d["metrics"]
    return (
        label,
        m["total_return"],
        m["total_return_pct"],
        m["total_trades"],
        m["win_rate"] * 100,
        m["max_drawdown_pct"],
        m["sharpe_ratio"],
        m["profit_factor"],
        m["annualized_return"],
    )


print("=" * 120)
print(f"{'策略配置':30s} | {'收益':>10s} | {'收益%':>8s} | {'交易':>5s} | {'胜率':>6s} | {'最大回撤':>8s} | {'Sharpe':>7s} | {'PF':>6s} | {'年化%':>7s}")
print("-" * 120)

results = []
for label, modules in CONFIGS:
    r = run_bt(label, modules)
    if isinstance(r, str):
        print(r)
    else:
        results.append(r)
        label, ret, ret_pct, trades, wr, dd, sharpe, pf, annual = r
        print(f"{label:30s} | {ret:>+10.0f} | {ret_pct:>+7.1f}% | {trades:>5d} | {wr:>5.0f}% | {dd:>7.1f}% | {sharpe:>7.2f} | {pf:>6.2f} | {annual:>+6.1f}%")

print("=" * 120)

# Find best by Sharpe
if results:
    best = max(results, key=lambda x: x[6])
    print(f"\n最佳 Sharpe: {best[0]} (Sharpe={best[6]:.2f}, Return={best[2]:+.1f}%, MaxDD={best[5]:.1f}%)")
    best_pf = max(results, key=lambda x: x[7])
    print(f"最佳 PF:     {best_pf[0]} (PF={best_pf[7]:.2f}, Return={best_pf[2]:+.1f}%)")
