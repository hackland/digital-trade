package eventbus

import (
	"sync"

	"go.uber.org/zap"
)

// Bus is a typed event bus using Go channels.
// Each subscriber gets a buffered channel and receives events
// published to the subscribed event type.
type Bus struct {
	mu          sync.RWMutex
	subscribers map[EventType][]chan Event
	logger      *zap.Logger
}

// New creates a new event bus.
func New(logger *zap.Logger) *Bus {
	return &Bus{
		subscribers: make(map[EventType][]chan Event),
		logger:      logger,
	}
}

// Subscribe returns a channel that receives events of the given type.
// bufSize controls the channel buffer size for backpressure.
func (b *Bus) Subscribe(eventType EventType, bufSize int) <-chan Event {
	ch := make(chan Event, bufSize)
	b.mu.Lock()
	b.subscribers[eventType] = append(b.subscribers[eventType], ch)
	b.mu.Unlock()
	return ch
}

// Publish sends an event to all subscribers of that event type.
// If a subscriber channel is full, the event is dropped and logged.
func (b *Bus) Publish(evt Event) {
	b.mu.RLock()
	subs := b.subscribers[evt.Type]
	b.mu.RUnlock()

	for _, ch := range subs {
		select {
		case ch <- evt:
		default:
			b.logger.Warn("event dropped: subscriber channel full",
				zap.Int("event_type", int(evt.Type)),
			)
		}
	}
}

// Close closes all subscriber channels.
func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for eventType, subs := range b.subscribers {
		for _, ch := range subs {
			close(ch)
		}
		delete(b.subscribers, eventType)
	}
}
