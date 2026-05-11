package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// 简单内存缓存：key → (expires, value)。避免高频调用打爆外部 API。
type sentimentCache struct {
	mu sync.RWMutex
	m  map[string]sentimentCacheEntry
}
type sentimentCacheEntry struct {
	exp time.Time
	val any
}

var sentCache = &sentimentCache{m: make(map[string]sentimentCacheEntry)}

func (c *sentimentCache) get(key string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.m[key]
	if !ok || time.Now().After(e.exp) {
		return nil, false
	}
	return e.val, true
}
func (c *sentimentCache) set(key string, val any, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = sentimentCacheEntry{exp: time.Now().Add(ttl), val: val}
}

// ─────────────────────────────────────────────────────────────────────────────
// 资金费率 (Binance 永续公开 API，无需鉴权)
// ─────────────────────────────────────────────────────────────────────────────

type FundingRatePoint struct {
	Time time.Time `json:"time"`
	Rate float64   `json:"rate"` // 8h 周期费率，正=多付空，负=空付多
}

type FundingRateResp struct {
	Symbol      string             `json:"symbol"`
	Current     float64            `json:"current"`      // 最新一期
	MarkPrice   float64            `json:"mark_price"`   // 标记价
	NextFunding time.Time          `json:"next_funding"` // 下次结算时间
	History     []FundingRatePoint `json:"history"`      // 近 30 期
	Verdict     string             `json:"verdict"`      // "neutral" | "overheated" | "panic"
	Hint        string             `json:"hint"`
}

// GET /api/v1/sentiment/funding?symbol=BTCUSDT
func (h *Handler) GetFundingRate(c *gin.Context) {
	symbol := c.DefaultQuery("symbol", "BTCUSDT")
	httpc := &http.Client{Timeout: 8 * time.Second}

	// 1. 最新 mark price + nextFundingTime
	premURL := fmt.Sprintf("https://fapi.binance.com/fapi/v1/premiumIndex?symbol=%s", symbol)
	pr, err := httpc.Get(premURL)
	if err != nil {
		errResp(c, http.StatusBadGateway, "binance premium index: "+err.Error())
		return
	}
	defer pr.Body.Close()
	var prem struct {
		Symbol          string `json:"symbol"`
		MarkPrice       string `json:"markPrice"`
		LastFundingRate string `json:"lastFundingRate"`
		NextFundingTime int64  `json:"nextFundingTime"`
	}
	if err := json.NewDecoder(pr.Body).Decode(&prem); err != nil {
		errResp(c, http.StatusBadGateway, "decode premium: "+err.Error())
		return
	}

	// 2. 历史 30 期
	histURL := fmt.Sprintf("https://fapi.binance.com/fapi/v1/fundingRate?symbol=%s&limit=30", symbol)
	hr, err := httpc.Get(histURL)
	if err != nil {
		errResp(c, http.StatusBadGateway, "binance funding history: "+err.Error())
		return
	}
	defer hr.Body.Close()
	var hist []struct {
		Symbol      string `json:"symbol"`
		FundingTime int64  `json:"fundingTime"`
		FundingRate string `json:"fundingRate"`
	}
	if err := json.NewDecoder(hr.Body).Decode(&hist); err != nil {
		errResp(c, http.StatusBadGateway, "decode history: "+err.Error())
		return
	}

	resp := FundingRateResp{Symbol: symbol}
	resp.Current, _ = strconv.ParseFloat(prem.LastFundingRate, 64)
	resp.MarkPrice, _ = strconv.ParseFloat(prem.MarkPrice, 64)
	resp.NextFunding = time.UnixMilli(prem.NextFundingTime)

	for _, h := range hist {
		r, _ := strconv.ParseFloat(h.FundingRate, 64)
		resp.History = append(resp.History, FundingRatePoint{
			Time: time.UnixMilli(h.FundingTime),
			Rate: r,
		})
	}

	// Verdict: 单期 ±0.05% 是阈值（年化约 ±55%）
	switch {
	case resp.Current >= 0.0005:
		resp.Verdict = "overheated"
		resp.Hint = "多头资金过热，警惕反转/回调"
	case resp.Current <= -0.0003:
		resp.Verdict = "panic"
		resp.Hint = "空方占主导，逆向可能是机会"
	default:
		resp.Verdict = "neutral"
		resp.Hint = "费率正常，无极端信号"
	}

	ok(c, resp)
}

// ─────────────────────────────────────────────────────────────────────────────
// Fear & Greed Index (alternative.me 免费 API)
// ─────────────────────────────────────────────────────────────────────────────

type FearGreedPoint struct {
	Time       time.Time `json:"time"`
	Value      int       `json:"value"`
	ValueClass string    `json:"value_class"`
}

type FearGreedResp struct {
	Current      int              `json:"current"`
	CurrentClass string           `json:"current_class"`
	History      []FearGreedPoint `json:"history"`
	Verdict      string           `json:"verdict"` // "extreme_fear" | "fear" | "neutral" | "greed" | "extreme_greed"
	Hint         string           `json:"hint"`
}

// GET /api/v1/sentiment/feargreed
func (h *Handler) GetFearGreed(c *gin.Context) {
	httpc := &http.Client{Timeout: 8 * time.Second}
	r, err := httpc.Get("https://api.alternative.me/fng/?limit=30")
	if err != nil {
		errResp(c, http.StatusBadGateway, "alternative.me: "+err.Error())
		return
	}
	defer r.Body.Close()

	var raw struct {
		Data []struct {
			Value      string `json:"value"`
			ValueClass string `json:"value_classification"`
			Timestamp  string `json:"timestamp"`
		} `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		errResp(c, http.StatusBadGateway, "decode F&G: "+err.Error())
		return
	}
	if len(raw.Data) == 0 {
		errResp(c, http.StatusBadGateway, "F&G empty response")
		return
	}

	resp := FearGreedResp{}
	for i, d := range raw.Data {
		v, _ := strconv.Atoi(d.Value)
		ts, _ := strconv.ParseInt(d.Timestamp, 10, 64)
		pt := FearGreedPoint{
			Time:       time.Unix(ts, 0),
			Value:      v,
			ValueClass: d.ValueClass,
		}
		resp.History = append(resp.History, pt)
		if i == 0 {
			resp.Current = v
			resp.CurrentClass = d.ValueClass
		}
	}

	switch {
	case resp.Current <= 20:
		resp.Verdict = "extreme_fear"
		resp.Hint = "极度恐惧，历史上常对应阶段性底部，逆向布局机会"
	case resp.Current <= 40:
		resp.Verdict = "fear"
		resp.Hint = "情绪偏空，可考虑分批建仓"
	case resp.Current <= 60:
		resp.Verdict = "neutral"
		resp.Hint = "情绪中性，无明显方向"
	case resp.Current <= 80:
		resp.Verdict = "greed"
		resp.Hint = "情绪偏多，注意追高风险"
	default:
		resp.Verdict = "extreme_greed"
		resp.Hint = "极度贪婪，历史上常对应阶段性顶部，警惕回调"
	}

	ok(c, resp)
}
