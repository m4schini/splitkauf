# Research: Progressive Web Apps on Modern iOS

*Based on training knowledge (cutoff August 2025). Web access was unavailable. Verify against `firt.dev` and `webkit.org/blog` for post-2025 changes.*

---

## What iOS PWAs Support Well (iOS 16.4+ / 18.x)

- **Service Workers + Cache API + IndexedDB** — fully supported; offline-first architecture is viable
- **Web Push notifications** — available since iOS 16.4, uses standard VAPID/Web Push, routed through APNs
- **Web App Manifest** — core properties (`icons`, `display: standalone`, `start_url`, `theme_color`) are respected
- **WebSockets / real-time sync** — works fine in the foreground

---

## Critical Gaps vs. Android Chrome

| Gap | Impact for splitkauf |
|---|---|
| No `beforeinstallprompt` | Users must manually navigate Share → Add to Home Screen; lower install rates |
| Push only works for installed PWAs | Users who never install won't receive list-change notifications |
| No Background Sync API | Offline writes must be synced on next foreground open, not automatically |
| No Periodic Background Sync | Cannot refresh data in the background |
| Only Safari can install PWAs | Chrome/Firefox for iOS users cannot install the PWA at all |

---

## Feature Matrix

| Feature | iOS Safari | Android Chrome |
|---|---|---|
| Service Worker | ✅ | ✅ |
| Cache API | ✅ | ✅ |
| IndexedDB | ✅ | ✅ |
| Web Push (installed only) | ✅ iOS 16.4+ | ✅ |
| `beforeinstallprompt` | ❌ | ✅ |
| Background Sync API | ❌ | ✅ |
| Periodic Background Sync | ❌ | ✅ |
| Install from non-Safari browser | ❌ | ✅ (Chrome) |
| `display: standalone` | ✅ | ✅ |
| File System Access API | ❌ | ✅ |
| Web Bluetooth | ❌ | ✅ |

---

## Implications for Splitkauf

### Offline
Background Sync absence is manageable. Sync on `visibilitychange` + the `online` event covers the real-world case for a shopping list (user opens the app → sync triggers). Use `workbox-background-sync` for Chrome/Firefox; for iOS, replay the `offline_queue` on app foreground.

```js
document.addEventListener('visibilitychange', () => {
  if (document.visibilityState === 'visible' && navigator.onLine) {
    replayOfflineQueue()
  }
})
window.addEventListener('online', replayOfflineQueue)
```

### Push Notifications
Push notifications for collaborative updates (someone checked an item, someone added to your list) only work on iOS for users who have installed the PWA to their home screen. Design the notification opt-in flow to happen *after* install, or gracefully degrade — in-app polling/SSE stays active in foreground regardless.

### Install Prompt
No `beforeinstallprompt` means no custom install banner. Mitigate with:
1. Detect `navigator.standalone === false` on iOS (Safari sets this)
2. Show an in-app prompt with instructions: "Tap Share → Add to Home Screen"
3. Display once, dismissible, re-show after 7 days

### Browser Restriction
Only Safari on iOS can install PWAs. Users on Chrome/Firefox for iOS see the full web app but cannot add it to home screen. This is a platform-level limitation with no workaround.

---

## PWA vs. Native Wrapper

| Approach | Pros | Cons |
|---|---|---|
| **Pure PWA** | Single codebase, no app store, instant deploy | iOS install friction, push only post-install |
| **Capacitor.js** | Closes push gap, app store presence, full background sync | Two distribution channels, build complexity |
| **React Native** | Full native performance | Separate codebase for web |

**Recommendation for splitkauf MVP**: Pure PWA. The gaps are acceptable for a household shopping list — check-off is primarily a foreground activity, and background sync is nice-to-have, not blocking. Revisit Capacitor if notification reach becomes important after launch.

---

## Key Open Question

Check `webkit.org/blog` for iOS 19 PWA changes (WWDC 2025) — knowledge cutoff for this report is August 2025. Maximiliano Firtman's site (`firt.dev`) maintains the most comprehensive iOS PWA compatibility table and is the authoritative ongoing reference.
