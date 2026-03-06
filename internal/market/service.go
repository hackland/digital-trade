package market

import (
	"context"
	"sync"
	"time"

	"github.com/jayce/btc-trader/internal/eventbus"
	"github.com/jayce/btc-trader/internal/exchange"
	"github.com/jayce/btc-trader/internal/strategy"
	"go.uber.org/zap"
)

// Service aggregates market data and computes indicators.
// It consumes raw kline events from the bus, maintains rolling windows,
// and publishes enriched MarketDataEvents with computed indicators.
type Service struct {
	bus      *eventbus.Bus
	logger   *zap.Logger
	computer *IndicatorComputer

	mu      sync.RWMutex
	windows map[string][]exchange.Kline // key: "BTCUSDT:5m"

	requirements []strategy.IndicatorRequirement
	historySize  int
}

// MarketDataEvent is published by the Service with computed indicators.
type MarketDataEvent struct {
	Symbol     string
	Interval   string
	Kline      exchange.Kline
	Indicators strategy.IndicatorSet
	Window     []exchange.Kline
}

// NewService creates a new market data service.
func NewService(
	bus *eventbus.Bus,
	requirements []strategy.IndicatorRequirement,
	historySize int,
	logger *zap.Logger,
) *Service {
	return &Service{
		bus:          bus,
		logger:       logger,
		computer:     NewIndicatorComputer(),
		windows:      make(map[string][]exchange.Kline),
		requirements: requirements,
		historySize:  historySize,
	}
}

// Run starts consuming kline events and computing indicators.
func (s *Service) Run(ctx context.Context) error {
	klineCh := s.bus.Subscribe(eventbus.EventKlineUpdate, 1000)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case evt, ok := <-klineCh:
			if !ok {
				return nil
			}

			ke, ok := evt.Payload.(eventbus.KlineEvent)
			if !ok {
				continue
			}

			s.processKline(ke)
		}
	}
}

// GetWindow returns the current kline window for a symbol/interval.
func (s *Service) GetWindow(symbol, interval string) []exchange.Kline {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := symbol + ":" + interval
	window := s.windows[key]
	result := make([]exchange.Kline, len(window))
	copy(result, window)
	return result
}

// GetIndicators computes current indicators for a symbol/interval.
func (s *Service) GetIndicators(symbol, interval string) strategy.IndicatorSet {
	window := s.GetWindow(symbol, interval)
	if len(window) == 0 {
		return strategy.IndicatorSet{}
	}
	return s.computer.ComputeAll(window, s.requirements)
}

func (s *Service) processKline(ke eventbus.KlineEvent) {
	s.mu.Lock()

	key := ke.Symbol + ":" + ke.Interval
	window := s.windows[key]

	// Only append final klines; update last for non-final
	if ke.Kline.IsFinal {
		window = append(window, ke.Kline)
	} else if len(window) > 0 {
		// Update the last kline in the window with real-time data
		last := &window[len(window)-1]
		if last.OpenTime == ke.Kline.OpenTime {
			*last = ke.Kline
		}
	}

	// Trim window to 2x history size
	maxSize := s.historySize * 2
	if maxSize < 200 {
		maxSize = 200
	}
	if len(window) > maxSize {
		window = window[len(window)-maxSize:]
	}
	s.windows[key] = window
	s.mu.Unlock()

	// Compute indicators if we have enough data
	if len(window) >= s.historySize {
		indicators := s.computer.ComputeAll(window, s.requirements)

		// Publish enriched market data event
		// Other components can subscribe to this instead of raw klines
		s.bus.Publish(eventbus.Event{
			Type:      eventbus.EventKlineUpdate, // Reuse same type for now
			Timestamp: time.Now(),
			Payload: MarketDataEvent{
				Symbol:     ke.Symbol,
				Interval:   ke.Interval,
				Kline:      ke.Kline,
				Indicators: indicators,
				Window:     window,
			},
		})
	}
}
