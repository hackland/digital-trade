package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jayce/btc-trader/internal/eventbus"
	"github.com/jayce/btc-trader/internal/exchange"
	"go.uber.org/zap"
)

// TelegramConfig holds Telegram bot settings.
type TelegramConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Token   string `mapstructure:"token"`
	ChatID  string `mapstructure:"chat_id"`
	// Prefix is prepended to every outgoing message. Used to mark testnet
	// messages so they can be distinguished from production in a shared chat.
	Prefix string `mapstructure:"-"`
}

// TelegramNotifier listens to EventBus events and sends Telegram messages.
type TelegramNotifier struct {
	cfg    TelegramConfig
	bus    *eventbus.Bus
	logger *zap.Logger
	client *http.Client
}

// NewTelegramNotifier creates a new Telegram notifier.
func NewTelegramNotifier(cfg TelegramConfig, bus *eventbus.Bus, logger *zap.Logger) *TelegramNotifier {
	return &TelegramNotifier{
		cfg:    cfg,
		bus:    bus,
		logger: logger,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// Run subscribes to events and sends notifications. Blocks until ctx is canceled.
func (n *TelegramNotifier) Run(ctx context.Context) error {
	if !n.cfg.Enabled || n.cfg.Token == "" || n.cfg.ChatID == "" {
		n.logger.Info("telegram notifications disabled")
		<-ctx.Done()
		return ctx.Err()
	}

	// Send startup message
	n.send(ctx, "🟢 *BTC Trader 已启动*\n模式: 实时监控中")

	// Guarantee a shutdown message on any return path (ctx cancel, bus close,
	// sibling goroutine error). Uses a fresh context with a short timeout so
	// it still fires when the parent ctx is already canceled, and won't hang
	// indefinitely if Telegram is unreachable during shutdown.
	defer func() {
		sendCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		n.send(sendCtx, "🔴 *BTC Trader 已停止*")
	}()

	orderCh := n.bus.Subscribe(eventbus.EventOrderUpdate, 100)
	signalCh := n.bus.Subscribe(eventbus.EventSignal, 100)
	riskCh := n.bus.Subscribe(eventbus.EventRiskAlert, 100)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case evt, ok := <-orderCh:
			if !ok {
				return nil
			}
			n.handleOrder(ctx, evt)

		case evt, ok := <-signalCh:
			if !ok {
				return nil
			}
			n.handleSignal(ctx, evt)

		case evt, ok := <-riskCh:
			if !ok {
				return nil
			}
			n.handleRisk(ctx, evt)
		}
	}
}

func (n *TelegramNotifier) handleSignal(ctx context.Context, evt eventbus.Event) {
	sig, ok := evt.Payload.(eventbus.SignalEvent)
	if !ok {
		return
	}

	var emoji, label string
	switch sig.Action {
	case "BUY":
		emoji = "📈"
		label = "做多买入"
	case "SELL":
		emoji = "📉"
		label = "做多卖出"
	case "SHORT":
		emoji = "🔻"
		label = "做空信号 (仅提醒)"
	case "COVER":
		emoji = "🔺"
		label = "平空信号 (仅提醒)"
	default:
		return // Don't notify on HOLD
	}

	msg := fmt.Sprintf("%s *%s*\n币对: `%s`\n强度: `%.2f`\n策略: %s\n原因: %s",
		emoji, label, sig.Symbol, sig.Strength, sig.Strategy, sig.Reason)
	n.send(ctx, msg)
}

func (n *TelegramNotifier) handleOrder(ctx context.Context, evt eventbus.Event) {
	ou, ok := evt.Payload.(eventbus.OrderUpdateEvent)
	if !ok {
		return
	}

	order := ou.Order
	var emoji string
	switch order.Status {
	case exchange.OrderStatusFilled:
		if order.Side == exchange.OrderSideBuy {
			emoji = "✅"
		} else {
			emoji = "💰"
		}
	case exchange.OrderStatusCanceled:
		emoji = "❌"
	default:
		return // Only notify on fills and cancels
	}

	msg := fmt.Sprintf("%s *订单 %s*\n币对: `%s`\n方向: %s\n价格: `$%.2f`\n数量: `%.6f`\n状态: %s",
		emoji, order.Status, order.Symbol, order.Side, order.AvgPrice, order.FilledQty, order.Status)
	n.send(ctx, msg)
}

func (n *TelegramNotifier) handleRisk(ctx context.Context, evt eventbus.Event) {
	ra, ok := evt.Payload.(eventbus.RiskAlertEvent)
	if !ok {
		return
	}

	var emoji string
	switch ra.Level {
	case "critical":
		emoji = "🚨"
	default:
		emoji = "⚠️"
	}

	msg := fmt.Sprintf("%s *风控警报*\n规则: %s\n详情: %s\n级别: %s",
		emoji, ra.Rule, ra.Message, ra.Level)
	n.send(ctx, msg)
}

// send sends a Markdown message to Telegram. Tries Markdown first; on 400
// (typically caused by unescaped special chars in interpolated error strings)
// retries with parse_mode disabled so the message still gets through.
func (n *TelegramNotifier) send(ctx context.Context, text string) {
	if n.cfg.Prefix != "" {
		text = n.cfg.Prefix + text
	}
	if n.sendOnce(ctx, text, "Markdown") {
		return
	}
	// Retry as plain text — strip the Markdown emphasis chars to keep it readable
	plain := stripMarkdown(text)
	n.sendOnce(ctx, plain, "")
}

// sendOnce posts the request once and returns true on HTTP 200.
func (n *TelegramNotifier) sendOnce(ctx context.Context, text, parseMode string) bool {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", n.cfg.Token)

	payload := map[string]interface{}{
		"chat_id": n.cfg.ChatID,
		"text":    text,
	}
	if parseMode != "" {
		payload["parse_mode"] = parseMode
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		n.logger.Error("telegram: build request", zap.Error(err))
		return false
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		n.logger.Error("telegram: send message", zap.Error(err))
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		n.logger.Warn("telegram: non-200 response",
			zap.Int("status", resp.StatusCode),
			zap.String("parse_mode", parseMode),
		)
		return false
	}
	return true
}

// stripMarkdown removes Telegram Markdown emphasis characters so the text
// can be sent as plain text fallback.
func stripMarkdown(s string) string {
	r := strings.NewReplacer("*", "", "_", "", "`", "")
	return r.Replace(s)
}
