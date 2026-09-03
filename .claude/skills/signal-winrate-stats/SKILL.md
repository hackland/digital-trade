---
name: signal-winrate-stats
description: Use whenever the user asks how reliable a live trading signal is, or wants historical win-rate / statistics for a specific setup (e.g. "这个信号历史胜率多高", "这种情况以前表现怎么样", "根据历史统计分析一下"). Runs a real backtest over the currently-deployed strategy config and buckets round-trip results by whatever dimension the question is about (HTF distance zone, module score range, weekday, etc.), reporting win rate / avg win / avg loss / total PnL per bucket with an explicit sample-size caveat. Do not just eyeball or guess a win rate from memory — always re-run the backtest, since config changes over time.
---

# Signal win-rate / historical stats analysis

The user will paste a live signal (Telegram alert or Dashboard diagnostics reason
string) or describe a setup, then ask something like "历史胜率多高" / "这种情况以前
准不准" / "结合历史统计分析一下". The job is to answer with real numbers pulled from
a fresh backtest — never estimate from memory, because strategy config changes
over time in this repo and stale numbers are misleading.

## Procedure

1. **Get the currently-deployed strategy config.** Read `configs/config.yaml`'s
   `strategy.config` block (or, if a backend is reachable, `GET /api/v1/strategy/status`
   for the live in-memory config — prefer this if available since it's ground truth).
   Do not use `configs/config.example.yaml`, it's stale.

2. **Identify the symbol/interval and available history.** Match the symbol from
   the pasted signal (e.g. `币对: BTCUSDT`). Interval is `strategy.config.interval`
   (currently `1h`). Check how far back klines actually exist:
   ```sql
   SELECT min(time), max(time) FROM klines WHERE symbol='<SYM>' AND interval='<htf_interval>';
   ```
   (or ask whoever has DB access) — the backtest can't produce useful HTF buckets
   beyond that range. Use as many days as are available (currently ~365d).

3. **Run the backtest via the REST API**, `POST /api/v1/backtest` (see
   `internal/web/handler/backtest.go` for the request schema), with
   `strategy_config` set to an exact copy of the config from step 1 — do not
   let it fall back to defaults. Target whichever backend is reachable (local
   `go run` instance on :9090, or the VPS at the deployed IP if that's what's
   live — check which one is actually running the current config before
   trusting the result over the other). Example payload shape:
   ```json
   {
     "symbol": "BTCUSDT", "interval": "1h", "strategy": "custom_weighted",
     "days": 365, "cash": 10000, "fee": 0.001, "alloc": 0.1,
     "strategy_config": { ...copied verbatim from configs/config.yaml... }
   }
   ```

4. **Parse trade reasons, don't just read the summary metrics.** Each
   `TradeRecord.reason` (on BUY records) is a human-readable string containing
   the numbers needed to bucket by, e.g.:
   `Custom buy (score=0.42, threshold=0.36, htf=看多(8.4%)): ema_cross=0.35, macd=0.64, mfi=-0.05`
   Regex out whatever the question is about — most commonly:
   - `htf=(看多|看空)\(([\-\d.]+)%\)` → signed HTF distance (看空 = negative)
   - `score=([\-\d.]+)` and `threshold=([\-\d.]+)` → score margin
   - individual module scores (`ema_cross=`, `macd=`, `mfi=`, `kdj=`, etc.)

5. **Pair BUY→SELL chronologically into round trips.** In this engine, buys and
   sells for a single-symbol long-only strategy alternate 1:1 in the `trades`
   array (no overlapping positions) — walk the list, remember the pending BUY,
   and when the next SELL arrives pair it with that BUY's parsed reason using
   the SELL's `pnl` field as the round trip's net result.

6. **Bucket to match the mechanism actually in the code**, not arbitrary bins.
   For HTF-distance questions, use the exact zones from
   `effectiveBuyThreshold` in `internal/strategy/trend/custom_weighted.go`
   (currently: >7%, 3-7%, 0.8-3%, 0-0.8%, <0%) — these are the zones the
   strategy itself treats differently, so they're the ones worth reporting.
   For other dimensions, pick natural code-meaningful cutoffs the same way.

7. **Report per bucket**: trade count, win rate, avg win, avg loss, total PnL.
   **Always state the trade count next to the win rate** and flag explicitly
   when a bucket has too few samples (rule of thumb: n<10 is not statistically
   meaningful) — say so plainly rather than presenting a 50% win rate on 2
   trades as if it means something. If the bucket matching the user's current
   question is thin, say which adjacent/broader bucket has enough samples to
   be useful instead, and give that number too.

## Notes

- This is read-only analysis (a backtest run), not a config change — no need
  to ask permission before running it.
- If the deployed config on the box being asked about doesn't match
  `configs/config.yaml` (e.g. VPS wasn't redeployed after a local edit),
  say so — the backtest must reflect what's *actually running*, not what's
  in the repo.