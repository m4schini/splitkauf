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
//
// Close ends every subscription by closing its channel, so long-lived
// consumers (the SSE streams) return during graceful server shutdown instead
// of holding their connections open until the shutdown deadline.
type Broker struct {
	mu     sync.Mutex
	subs   map[chan Event]struct{}
	closed bool
}

// NewBroker returns an empty Broker ready to accept subscribers.
func NewBroker() *Broker {
	return &Broker{
		mu:     sync.Mutex{},
		subs:   make(map[chan Event]struct{}),
		closed: false,
	}
}

// Subscribe registers a new subscriber and returns a buffered receive channel
// plus an unsubscribe func. The channel delivers events until unsubscribe or
// Close is called, either of which removes the subscriber and closes the
// channel; a receive then reports !ok. unsubscribe is safe to call any number
// of times, including after Close (later calls are no-ops). Callers must call
// unsubscribe (e.g. via defer) to avoid leaking the subscriber. After Close,
// Subscribe returns an already-closed channel.
func (b *Broker) Subscribe() (<-chan Event, func()) {
	sub := make(chan Event, subscriberBuffer)

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		close(sub)

		return sub, func() {}
	}

	b.subs[sub] = struct{}{}

	unsubscribe := func() {
		b.mu.Lock()
		defer b.mu.Unlock()

		// Only close the channel if it is still registered: Close may already
		// have closed it, and closing twice would panic.
		if _, ok := b.subs[sub]; ok {
			delete(b.subs, sub)
			close(sub)
		}
	}

	return sub, unsubscribe
}

// Close closes every subscriber channel, drops all subscribers and makes later
// Subscribe calls return an already-closed channel. Publish after Close is a
// no-op. Close is idempotent and safe for concurrent use; it is meant to be
// registered with http.Server.RegisterOnShutdown so open SSE streams end when
// the server shuts down.
func (b *Broker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.closed = true

	for sub := range b.subs {
		delete(b.subs, sub)
		close(sub)
	}
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
