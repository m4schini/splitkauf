// SPDX-License-Identifier: CC0-1.0

package events

import "sync"

// subscriberBuffer is the per-subscriber channel capacity. A small buffer
// absorbs brief bursts; when it is full Publish drops the hint for that
// subscriber rather than blocking (the next event, or the reconnect reload,
// refetches the latest state anyway).
const subscriberBuffer = 16

// Broker is an in-memory fan-out of events to a set of subscribers. It is safe
// for concurrent use: every subscriber-set access is guarded by a mutex.
// Publish does a non-blocking send to each subscriber so one slow or
// disconnected client can never stall delivery to the others. The zero value is
// not usable; construct one with NewBroker. Broker implements Publisher.
type Broker struct {
	mu   sync.Mutex
	subs map[chan Event]struct{}
}

// NewBroker returns an empty Broker ready to accept subscribers.
func NewBroker() *Broker {
	return &Broker{
		mu:   sync.Mutex{},
		subs: make(map[chan Event]struct{}),
	}
}

// Subscribe registers a new subscriber and returns a buffered receive channel
// plus an unsubscribe func. The channel delivers events until unsubscribe is
// called; unsubscribe removes the subscriber, closes the channel, and is safe
// to call exactly once (subsequent calls are no-ops). Callers must call
// unsubscribe (e.g. via defer) to avoid leaking the subscriber.
func (b *Broker) Subscribe() (<-chan Event, func()) {
	sub := make(chan Event, subscriberBuffer)

	b.mu.Lock()
	b.subs[sub] = struct{}{}
	b.mu.Unlock()

	var once sync.Once

	unsubscribe := func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subs, sub)
			b.mu.Unlock()
			close(sub)
		})
	}

	return sub, unsubscribe
}

// Publish delivers event to every current subscriber with a non-blocking send: if a
// subscriber's buffer is full the event is dropped for that subscriber only,
// never blocking Publish or the other subscribers. Delivery order across
// subscribers is unspecified.
func (b *Broker) Publish(event Event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for sub := range b.subs {
		select {
		case sub <- event:
		default:
			// Subscriber buffer full: drop this hint for that subscriber.
		}
	}
}

// Count returns the number of current subscribers.
func (b *Broker) Count() int {
	b.mu.Lock()
	defer b.mu.Unlock()

	return len(b.subs)
}
