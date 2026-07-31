// Shared SSE connection for live updates (M3, Key Decision 5). Deliberately
// free of React Query: this module only owns the transport (one ref-counted
// EventSource) and a plain listener registry. Views translate AppEvents into
// `queryClient.invalidateQueries(...)` calls themselves.
import { useEffect, useRef } from 'react'

export type AppEvent = { type: 'lists' } | { type: 'items'; listId: string } | { type: 'reconnect' }

type Listener = (event: AppEvent) => void

const listeners = new Set<Listener>()
let source: EventSource | null = null
// Set when the EventSource reports an error (dropped connection). EventSource
// auto-reconnects on its own; we treat the `open` that follows an `error` as
// "reconnected" and emit exactly one synthetic `reconnect` event for it, so a
// single logical drop/reconnect cycle doesn't spam listeners with repeats.
let awaitingReconnect = false

function dispatch(event: AppEvent) {
  for (const listener of listeners) listener(event)
}

function handleMessage(event: MessageEvent) {
  try {
    const parsed = JSON.parse(event.data as string) as AppEvent
    dispatch(parsed)
  } catch {
    // Malformed/heartbeat frame — ignore; the next valid event or a
    // reconnect will bring listeners back in sync.
  }
}

function handleError() {
  awaitingReconnect = true
}

function handleOpen() {
  if (awaitingReconnect) {
    awaitingReconnect = false
    dispatch({ type: 'reconnect' })
  }
}

function connect() {
  const es = new EventSource('/api/v1/events')
  es.addEventListener('message', handleMessage)
  es.addEventListener('error', handleError)
  es.addEventListener('open', handleOpen)
  source = es
}

function disconnect() {
  source?.close()
  source = null
  awaitingReconnect = false
}

/**
 * Registers `handler` for live events, opening the shared EventSource on the
 * first subscriber and closing it once the last subscriber unsubscribes (so
 * it never connects while logged out — live views only mount when
 * authenticated). Returns an unsubscribe function.
 */
export function subscribeLive(handler: Listener): () => void {
  if (listeners.size === 0) connect()
  listeners.add(handler)
  return () => {
    listeners.delete(handler)
    if (listeners.size === 0) disconnect()
  }
}

/**
 * React hook wrapping `subscribeLive`. `handler` is read via a ref so the
 * subscription is only opened/closed on mount/unmount, not on every render.
 */
export function useLiveEvents(handler: Listener): void {
  const handlerRef = useRef(handler)
  handlerRef.current = handler

  useEffect(() => {
    return subscribeLive((event) => handlerRef.current(event))
  }, [])
}
