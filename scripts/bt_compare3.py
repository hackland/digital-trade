#!/usr/bin/env python3
"""Round 3: variations around EMA50%+RSI30%+VOL20%+4h filter baseline."""
import json
import urllib.request

BASE = "http://localhost:9090/api/v1/backtest"

def run_bt(label, cfg):
    data = json.dumps(cfg).encode()
    req = urllib.request.Request(BASE, data=data, headers={"Content-Type": "application/json"})
    try:
        resp = urllib.request.urlopen(req, timeout=60)
        r = json.loads(resp.read())
    except Exception as e:
        return label, None, str(e)
    d = r.get("data", r)
    m = d["metrics"]
    return (label, m, None)

def make_cfg(modules, signal, interval="1h", days=365):
    return {
        "symbol": "BTCUSDT", "interval": interval, "strategy": "custom_weighted",
        "days": days, "cash": 10000, "alloc": 1.0, "fee": 0,
        "strategy_config": {**signal, "modules": modules},
    }

# Base modules
EMA_RSI_VOL = [
    {"name": "ema_cross", "weight": 0.50, "params": {"fast_period": 9, "slow_period": 21}},
    {"name": "rsi", "weight": 0.30, "params": {"period": 14}},
    {"name": "volume_ratio", "weight": 0.20, "params": {"period": 20}},
]
BASE_SIG = {
    "buy_threshold": 0.15, "sell_threshold": -0.5, "confirm_bars": 1,
    "cooldown_bars": 2, "min_hold_bars": 6, "atr_stop_mult": 3.0,
    "trend_filter": True, "trend_period": 50,
    "htf_enabled": True, "htf_interval": "4h", "htf_period": 20,
}

TESTS = []

# === Part 1: 权重微调 ===
TESTS.append(("--- 权重变化 ---", None))
TESTS.append(("基准 EMA50/RSI30/VOL20", make_cfg(EMA_RSI_VOL, BASE_SIG)))
for ew, rw, vw in [(0.60, 0.25, 0.15), (0.40, 0.35, 0.25), (0.45, 0.35, 0.20), (0.55, 0.25, 0.20), (0.50, 0.20, 0.30)]:
    mods = [
        {"name": "ema_cross", "weight": ew, "params": {"fast_period": 9, "slow_period": 21}},
        {"name": "rsi", "weight": rw, "params": {"period": 14}},
        {"name": "volume_ratio", "weight": vw, "params": {"period": 20}},
    ]
    TESTS.append((f"EMA{int(ew*100)}/RSI{int(rw*100)}/VOL{int(vw*100)}", make_cfg(mods, BASE_SIG)))

# === Part 2: 信号参数变化 ===
TESTS.append(("--- 信号参数 ---", None))
for bt, st, cb, cd, mh, atr, label in [
    (0.20, -0.5, 1, 2, 6, 3.0, "买入0.20"),
    (0.10, -0.5, 1, 2, 6, 3.0, "买入0.10"),
    (0.15, -0.4, 1, 2, 6, 3.0, "卖出-0.4"),
    (0.15, -0.6, 1, 2, 6, 3.0, "卖出-0.6"),
    (0.15, -0.5, 2, 2, 6, 3.0, "确认2根"),
    (0.15, -0.5, 1, 2, 8, 3.0, "持仓8根"),
    (0.15, -0.5, 1, 2, 4, 3.0, "持仓4根"),
    (0.15, -0.5, 1, 2, 6, 2.5, "ATR2.5"),
    (0.15, -0.5, 1, 2, 6, 3.5, "ATR3.5"),
    (0.15, -0.5, 1, 2, 6, 4.0, "ATR4.0"),
    (0.15, -0.5, 1, 1, 6, 3.0, "冷却1根"),
    (0.15, -0.5, 1, 3, 6, 3.0, "冷却3根"),
]:
    sig = {**BASE_SIG, "buy_threshold": bt, "sell_threshold": st, "confirm_bars": cb,
           "cooldown_bars": cd, "min_hold_bars": mh, "atr_stop_mult": atr}
    TESTS.append((label, make_cfg(EMA_RSI_VOL, sig)))

# === Part 3: EMA参数变化 ===
TESTS.append(("--- EMA周期 ---", None))
for fp, sp in [(5, 13), (7, 21), (9, 26), (12, 26), (9, 50), (20, 50)]:
    mods = [
        {"name": "ema_cross", "weight": 0.50, "params": {"fast_period": fp, "slow_period": sp}},
        {"name": "rsi", "weight": 0.30, "params": {"period": 14}},
        {"name": "volume_ratio", "weight": 0.20, "params": {"period": 20}},
    ]
    TESTS.append((f"EMA({fp}/{sp})", make_cfg(mods, BASE_SIG)))

# === Part 4: 趋势过滤周期 ===
TESTS.append(("--- 趋势过滤EMA ---", None))
for tp in [30, 40, 60, 80, 100]:
    sig = {**BASE_SIG, "trend_period": tp}
    TESTS.append((f"趋势EMA{tp}", make_cfg(EMA_RSI_VOL, sig)))

# === Part 5: HTF参数 ===
TESTS.append(("--- HTF参数 ---", None))
for hi, hp in [("4h", 10), ("4h", 30), ("4h", 50), ("1d", 20), ("1d", 10)]:
    sig = {**BASE_SIG, "htf_interval": hi, "htf_period": hp}
    TESTS.append((f"HTF {hi}/EMA{hp}", make_cfg(EMA_RSI_VOL, sig)))

# === Part 6: 加第4个模块 ===
TESTS.append(("--- +第4模块 ---", None))
for extra_name, extra_w, extra_p, lbl in [
    ("macd", 0.15, {"fast": 12, "slow": 26, "signal": 9}, "+MACD"),
    ("kdj", 0.15, {"period": 9, "k_smooth": 3, "d_smooth": 3}, "+KDJ"),
    ("mfi", 0.15, {"period": 14}, "+MFI"),
    ("bb_position", 0.15, {"period": 20, "mult": 2}, "+BB"),
    ("cmf", 0.15, {"period": 20}, "+CMF"),
]:
    mods = [
        {"name": "ema_cross", "weight": 0.40, "params": {"fast_period": 9, "slow_period": 21}},
        {"name": "rsi", "weight": 0.25, "params": {"period": 14}},
        {"name": "volume_ratio", "weight": 0.20, "params": {"period": 20}},
        {"name": extra_name, "weight": extra_w, "params": extra_p},
    ]
    TESTS.append((lbl, make_cfg(mods, BASE_SIG)))

# === Part 7: 不同时间段 ===
TESTS.append(("--- 不同回测期 ---", None))
for d in [90, 180, 730]:
    TESTS.append((f"{d}天", make_cfg(EMA_RSI_VOL, BASE_SIG, days=d)))

# Run all
print(f"{'配置':28s} | {'收益':>10s} | {'收益%':>8s} | {'交易':>5s} | {'胜率':>6s} | {'回撤':>7s} | {'Sharpe':>7s} | {'PF':>6s}")
print("-" * 100)

all_results = []
for label, cfg in TESTS:
    if cfg is None:
        print(f"\n{label}")
        print("-" * 100)
        continue
    label, m, err = run_bt(label, cfg)
    if err:
        print(f"{label:28s} | ERROR: {err}")
        continue
    ret = m["total_return"]
    rp = m["total_return_pct"]
    t = m["total_trades"]
    wr = m["win_rate"] * 100
    dd = m["max_drawdown_pct"]
    sh = m["sharpe_ratio"]
    pf = m["profit_factor"]
    print(f"{label:28s} | {ret:>+10.0f} | {rp:>+7.1f}% | {t:>5d} | {wr:>5.0f}% | {dd:>6.1f}% | {sh:>7.2f} | {pf:>6.2f}")
    all_results.append((label, ret, rp, t, wr, dd, sh, pf))

print("\n" + "=" * 100)
if all_results:
    top5 = sorted(all_results, key=lambda x: x[6], reverse=True)[:5]
    print("\nTop 5 by Sharpe:")
    for i, (l, ret, rp, t, wr, dd, sh, pf) in enumerate(top5, 1):
        print(f"  {i}. {l:28s} Sharpe={sh:.2f} Return={rp:+.1f}% DD={dd:.1f}% Trades={t} WR={wr:.0f}% PF={pf:.2f}")
