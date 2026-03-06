package eventbus

import (
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestBus_PubSub(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	bus := New(logger)
	defer bus.Close()

	ch := bus.Subscribe(EventKlineUpdate, 10)

	evt := Event{
		Type:      EventKlineUpdate,
		Timestamp: time.Now(),
		Payload:   "test_payload",
	}
	bus.Publish(evt)

	select {
	case received := <-ch:
		if received.Payload != "test_payload" {
			t.Errorf("got payload %v, want test_payload", received.Payload)
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for event")
	}
}

func TestBus_MultipleSubscribers(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	bus := New(logger)
	defer bus.Close()

	ch1 := bus.Subscribe(EventSignal, 10)
	ch2 := bus.Subscribe(EventSignal, 10)

	bus.Publish(Event{
		Type:      EventSignal,
		Timestamp: time.Now(),
		Payload:   "signal_data",
	})

	for i, ch := range []<-chan Event{ch1, ch2} {
		select {
		case received := <-ch:
			if received.Payload != "signal_data" {
				t.Errorf("subscriber %d: got %v, want signal_data", i, received.Payload)
			}
		case <-time.After(time.Second):
			t.Errorf("subscriber %d: timeout", i)
		}
	}
}

func TestBus_NoSubscribers(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	bus := New(logger)
	defer bus.Close()

	// Should not panic when no subscribers
	bus.Publish(Event{
		Type:      EventTradeUpdate,
		Timestamp: time.Now(),
		Payload:   "orphan",
	})
}

func TestBus_TypeIsolation(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	bus := New(logger)
	defer bus.Close()

	klineCh := bus.Subscribe(EventKlineUpdate, 10)
	signalCh := bus.Subscribe(EventSignal, 10)

	// Publish kline event
	bus.Publish(Event{
		Type:      EventKlineUpdate,
		Timestamp: time.Now(),
		Payload:   "kline_data",
	})

	// Signal channel should NOT receive kline event
	select {
	case <-signalCh:
		t.Error("signal channel should not receive kline event")
	case <-time.After(50 * time.Millisecond):
		// Expected
	}

	// Kline channel should receive it
	select {
	case received := <-klineCh:
		if received.Payload != "kline_data" {
			t.Errorf("got %v, want kline_data", received.Payload)
		}
	case <-time.After(time.Second):
		t.Error("timeout")
	}
}

func TestBus_DropOnFullChannel(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	bus := New(logger)
	defer bus.Close()

	// Buffer size 1
	ch := bus.Subscribe(EventKlineUpdate, 1)

	// Publish 3 events, only 1 should be buffered
	for i := 0; i < 3; i++ {
		bus.Publish(Event{
			Type:      EventKlineUpdate,
			Timestamp: time.Now(),
			Payload:   i,
		})
	}

	// Read the first
	select {
	case <-ch:
		// OK
	case <-time.After(time.Second):
		t.Error("should have received at least one event")
	}
}
