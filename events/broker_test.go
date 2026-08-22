// SPDX-License-Identifier: CC0-1.0

package events_test

import (
	"testing"
	"time"

	"github.com/m4schini/splitkauf/events"
)

// recv waits briefly for an event on ch, failing if none arrives.
func recv(t *testing.T, ch <-chan events.Event) events.Event {
	t.Helper()

	select {
	case e := <-ch:
		return e
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")

		return events.Event{Type: "", ListID: ""}
	}
}

func TestSubscriberReceivesEvent(t *testing.T) {
	t.Parallel()

	broker := events.NewBroker()

	eventCh, unsub := broker.Subscribe()
	defer unsub()

	want := events.Event{Type: events.TypeItems, ListID: "abc"}
	broker.Publish(want)

	if got := recv(t, eventCh); got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestUnsubscribeStopsDeliveryAndDropsSubscriber(t *testing.T) {
	t.Parallel()

	broker := events.NewBroker()
	eventCh, unsub := broker.Subscribe()

	if broker.Count() != 1 {
		t.Fatalf("Count = %d, want 1", broker.Count())
	}

	unsub()

	if broker.Count() != 0 {
		t.Errorf("Count after unsubscribe = %d, want 0", broker.Count())
	}

	// The channel is closed on unsubscribe: a receive returns !ok.
	if _, ok := <-eventCh; ok {
		t.Error("channel should be closed after unsubscribe")
	}

	// Publishing after unsubscribe must not panic or deliver.
	broker.Publish(events.Event{Type: events.TypeLists, ListID: ""})

	// Second unsubscribe is a safe no-op.
	unsub()
}

func TestMultipleSubscribersEachReceive(t *testing.T) {
	t.Parallel()

	broker := events.NewBroker()

	ch1, unsub1 := broker.Subscribe()
	defer unsub1()

	ch2, unsub2 := broker.Subscribe()
	defer unsub2()

	if broker.Count() != 2 {
		t.Fatalf("Count = %d, want 2", broker.Count())
	}

	want := events.Event{Type: events.TypeLists, ListID: ""}
	broker.Publish(want)

	if got := recv(t, ch1); got != want {
		t.Errorf("ch1 got %+v, want %+v", got, want)
	}

	if got := recv(t, ch2); got != want {
		t.Errorf("ch2 got %+v, want %+v", got, want)
	}
}

// TestPublishDoesNotBlockOnFullSubscriber proves a slow subscriber whose buffer
// is full never stalls delivery to a healthy one: the healthy subscriber still
// receives, and Publish returns promptly.
func TestPublishDoesNotBlockOnFullSubscriber(t *testing.T) {
	t.Parallel()

	broker := events.NewBroker()

	// A subscriber that never drains: fill its buffer past capacity.
	slow, unsubSlow := broker.Subscribe()
	defer unsubSlow()

	fast, unsubFast := broker.Subscribe()
	defer unsubFast()

	// Publish many events; the slow subscriber's buffer fills and further hints
	// are dropped for it, but this must not block Publish or the fast subscriber.
	done := make(chan struct{})

	go func() {
		for range subscriberBufferPlus() {
			broker.Publish(events.Event{Type: events.TypeItems, ListID: ""})
		}

		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked on a full subscriber")
	}

	// The fast subscriber still gets deliveries (its buffer also filled, but it
	// received at least the first events).
	if got := recv(t, fast); got.Type != events.TypeItems {
		t.Errorf("fast subscriber got %+v", got)
	}

	// The slow subscriber's buffered events are still readable (not blocked/lost
	// beyond the drops); draining should not deadlock.
	select {
	case <-slow:
	case <-time.After(time.Second):
		t.Fatal("slow subscriber buffer unexpectedly empty")
	}
}

// subscriberBufferPlus returns a count comfortably larger than the internal
// buffer so at least one send is dropped, without importing the unexported
// constant.
func subscriberBufferPlus() int { return 100 }

func TestCountReflectsSubscribeUnsubscribe(t *testing.T) {
	t.Parallel()

	broker := events.NewBroker()
	if broker.Count() != 0 {
		t.Fatalf("initial Count = %d, want 0", broker.Count())
	}

	_, unsub1 := broker.Subscribe()

	_, unsub2 := broker.Subscribe()
	if broker.Count() != 2 {
		t.Fatalf("Count = %d, want 2", broker.Count())
	}

	unsub1()

	if broker.Count() != 1 {
		t.Fatalf("Count = %d, want 1", broker.Count())
	}

	unsub2()

	if broker.Count() != 0 {
		t.Fatalf("Count = %d, want 0", broker.Count())
	}
}

// TestBrokerIsPublisher pins the interface satisfaction the handlers rely on.
func TestBrokerIsPublisher(t *testing.T) {
	t.Parallel()

	var _ events.Publisher = events.NewBroker()
}
