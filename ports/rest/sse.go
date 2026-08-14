// SPDX-License-Identifier: CC0-1.0

package rest

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/m4schini/splitkauf/events"
	"github.com/m4schini/splitkauf/telemetry"
	"go.uber.org/zap"
)

// heartbeatInterval is how often the stream emits a comment line to keep the
// connection alive through proxy idle timeouts.
const heartbeatInterval = 25 * time.Second

// sseHandler streams events.Broker events to the client as Server-Sent Events.
// It subscribes on connect, writes each event as a `data: <json>` frame,
// flushes so the browser sees it immediately, and emits a periodic `: ping`
// comment as a heartbeat. It returns — unsubscribing and stopping the ticker —
// when the request context is done (client disconnect or server shutdown), so
// it never leaks the subscription or the ticker. It is nil-safe: with a nil
// broker it degrades to a heartbeat-only stream rather than panicking.
func sseHandler(broker *events.Broker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log := telemetry.Logger("sse")

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		rc := http.NewResponseController(w)

		// Subscribe before the first flush so no event published after the
		// stream opens is missed. In degraded mode (nil broker) events is nil
		// and the loop relies solely on the heartbeat and context.
		var eventsCh <-chan events.Event
		if broker != nil {
			ch, unsubscribe := broker.Subscribe()
			defer unsubscribe()
			eventsCh = ch
		}

		// Flush the headers so the client's EventSource opens immediately rather
		// than waiting for the first event or heartbeat.
		if err := rc.Flush(); err != nil {
			log.Debug("initial flush failed; client likely gone", zap.Error(err))
			return
		}

		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := w.Write([]byte(": ping\n\n")); err != nil {
					log.Debug("heartbeat write failed; ending stream", zap.Error(err))
					return
				}
				if err := rc.Flush(); err != nil {
					log.Debug("heartbeat flush failed; ending stream", zap.Error(err))
					return
				}
			case e, ok := <-eventsCh:
				if !ok {
					// Broker closed our subscription; end the stream.
					return
				}
				payload, err := json.Marshal(e)
				if err != nil {
					log.Error("marshalling event", zap.Error(err))
					continue
				}
				if _, err := w.Write([]byte("data: ")); err != nil {
					log.Debug("event write failed; ending stream", zap.Error(err))
					return
				}
				if _, err := w.Write(payload); err != nil {
					log.Debug("event write failed; ending stream", zap.Error(err))
					return
				}
				if _, err := w.Write([]byte("\n\n")); err != nil {
					log.Debug("event write failed; ending stream", zap.Error(err))
					return
				}
				if err := rc.Flush(); err != nil {
					log.Debug("event flush failed; ending stream", zap.Error(err))
					return
				}
			}
		}
	}
}
