#!/usr/bin/env python3
"""Backtest comparison - test signal param variations on best module combo."""
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

# Best combo from round 1: EMA+MACD+KDJ
MODULES = [
    {"name": "ema_cross", "weight": 0.35, "params": {"fast_period": 9, "slow_period": 21}},
    {"name": "macd", "weight": 0.35, "params": {"fast": 12, "slow": 26, "signal": 9}},
    {"name": "kdj", "weight": 0.30, "params": {"period": 9, "k_smooth": 3, "d_smooth": 3}},
]

PARAM_SETS = [
    ("标准(0.15/-0.5/hold6/atr3)", {"buy_threshold": 0.15, "sell_threshold": -0.5, "confirm_bars": 1, "cooldown_bars": 2, "min_hold_bars": 6, "atr_stop_mult": 3.0, "trend_filter": True, "trend_period": 50}),
    ("保守(0.25/-0.6/hold8/atr3.5)", {"buy_threshold": 0.25, "sell_threshold": -0.6, "confirm_bars": 2, "cooldown_bars": 3, "min_hold_bars": 8, "atr_stop_mult": 3.5, "trend_filter": True, "trend_period": 50}),
    ("激进(0.10/-0.3/hold3/atr2)", {"buy_threshold": 0.10, "sell_threshold": -0.3, "confirm_bars": 1, "cooldown_bars": 1, "min_hold_bars": 3, "atr_stop_mult": 2.0, "trend_filter": True, "trend_period": 50}),
    ("宽松买+紧止损(0.10/-0.5/hold6/atr2)", {"buy_threshold": 0.10, "sell_threshold": -0.5, "confirm_bars": 1, "cooldown_bars": 2, "min_hold_bars": 6, "atr_stop_mult": 2.0, "trend_filter": True, "trend_period": 50}),
    ("严格买+宽止损(0.30/-0.5/hold6/atr4)", {"buy_threshold": 0.30, "sell_threshold": -0.5, "confirm_bars": 2, "cooldown_bars": 2, "min_hold_bars": 6, "atr_stop_mult": 4.0, "trend_filter": True, "trend_period": 50}),
    ("无趋势过滤(0.15/-0.5/hold6/atr3)", {"buy_threshold": 0.15, "sell_threshold": -0.5, "confirm_bars": 1, "cooldown_bars": 2, "min_hold_bars": 6, "atr_stop_mult": 3.0, "trend_filter": False, "trend_period": 50}),
    ("长趋势EMA100(0.15/-0.5/hold6/atr3)", {"buy_threshold": 0.15, "sell_threshold": -0.5, "confirm_bars": 1, "cooldown_bars": 2, "min_hold_bars": 6, "atr_stop_mult": 3.0, "trend_filter": True, "trend_period": 100}),
    ("长持仓(0.20/-0.4/hold12/atr3.5)", {"buy_threshold": 0.20, "sell_threshold": -0.4, "confirm_bars": 1, "cooldown_bars": 3, "min_hold_bars": 12, "atr_stop_mult": 3.5, "trend_filter": True, "trend_period": 50}),
]

# Also test different intervals
INTERVAL_TESTS = [
    ("4h周期(EMA+MACD+KDJ标准)", "4h", {"buy_threshold": 0.15, "sell_threshold": -0.5, "confirm_bars": 1, "cooldown_bars": 2, "min_hold_bars": 6, "atr_stop_mult": 3.0, "trend_filter": True, "trend_period": 50}),
    ("4h保守", "4h", {"buy_threshold": 0.25, "sell_threshold": -0.6, "confirm_bars": 2, "cooldown_bars": 2, "min_hold_bars": 6, "atr_stop_mult": 3.5, "trend_filter": True, "trend_period": 50}),
]


def run_bt(label, modules, signal_cfg, interval="1h"):
    cfg = {**COMMON, "interval": interval, "strategy_config": {**signal_cfg, "modules": modules}}
    data = json.dumps(cfg).encode()
    req = urllib.request.Request(BASE, data=data, headers={"Content-Type": "application/json"})
    try:
        resp = urllib.request.urlopen(req, timeout=60)
        r = json.loads(resp.read())
    except Exception as e:
        return f"{label:38s} | ERROR: {e}"

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
    )


def print_header():
    print(f"{'策略配置':38s} | {'收益':>10s} | {'收益%':>8s} | {'交易':>5s} | {'胜率':>6s} | {'最大回撤':>8s} | {'Sharpe':>7s} | {'PF':>6s}")
    print("-" * 110)


def print_row(r):
    if isinstance(r, str):
        print(r)
    else:
        label, ret, ret_pct, trades, wr, dd, sharpe, pf = r
        print(f"{label:38s} | {ret:>+10.0f} | {ret_pct:>+7.1f}% | {trades:>5d} | {wr:>5.0f}% | {dd:>7.1f}% | {sharpe:>7.2f} | {pf:>6.2f}")


print("=" * 110)
print("Part 1: EMA+MACD+KDJ 不同信号参数 (1h)")
print("=" * 110)
print_header()
results = []
for label, params in PARAM_SETS:
    r = run_bt(label, MODULES, params)
    results.append(r)
    print_row(r)

print("\n" + "=" * 110)
print("Part 2: 4h 周期测试")
print("=" * 110)
print_header()
for label, interval, params in INTERVAL_TESTS:
    r = run_bt(label, MODULES, params, interval)
    results.append(r)
    print_row(r)

print("=" * 110)

valid = [r for r in results if isinstance(r, tuple)]
if valid:
    best = max(valid, key=lambda x: x[6])
    print(f"\n最佳 Sharpe: {best[0]} (Sharpe={best[6]:.2f}, Return={best[2]:+.1f}%, MaxDD={best[5]:.1f}%)")
