---
date: 2026-07-21T00:00:00+00:00
git_commit: n/a (web research, no codebase changes)
branch: n/a
topic: "Progressive Web Apps (PWAs) on modern iOS"
tags: [research, pwa, ios, safari, webkit, push-notifications, service-workers, offline, splitkauf]
status: complete
---

# Research: Progressive Web Apps (PWAs) on modern iOS

## Research Question

What is the current state of PWA support on iOS (Safari, WKWebView)? What features are supported vs. missing (push notifications, background sync, install prompts, offline support, home screen icons)? What are the limitations compared to Android PWAs? What are the best practices for building a PWA that works well on iOS as of 2025-2026?

This research informs the decision of whether to build **splitkauf** (a collaborative shopping list app) as a PWA or native app, targeting iOS and Android users.

> **Note:** This is a web-domain research report based on knowledge through August 2025. Live web search was not available during this session. Verify any time-sensitive details against https://webkit.org/blog, https://caniuse.com, and https://developer.mozilla.org.

---

## Summary

iOS PWA support has improved dramatically since iOS 16.4 (March 2023), which finally brought Web Push notifications and Service Worker improvements. As of iOS 18.x (mid-2025), Safari supports the core PWA building blocks: Service Workers, Cache API, IndexedDB, Web App Manifest, and Web Push. However, critical gaps remain compared to Android Chrome: there is no programmatic install prompt, no Background Sync, no Periodic Background Sync, and push notifications only work for installed (home screen) PWAs.

For a collaborative shopping list app like splitkauf, the most critical missing feature is **Background Sync** — which would allow queued writes to sync when connectivity is restored. This must be worked around at the application layer. Push notifications work but require users to install the PWA first, which is a significant UX friction point given that iOS offers no install prompt.

```
splitkauf/
├── CLAUDE.md
├── AGENTS.md
└── docs/
    └── agents/
        └── research/
            └── 2026-07-21-pwa-ios-support.md  <- this file
```

---

## Detailed Findings

### 1. Service Workers on iOS

**Introduced:** iOS 11.3 (March 2018) — the first version of Safari to ship Service Workers.

**Current support (iOS 16+):**
- Full fetch interception and caching via `Cache API`
- Background fetch (limited)
- Push event handling (iOS 16.4+)
- Service Worker registration, installation, and activation lifecycle

**Key limitations:**
- Service Workers are **suspended aggressively** by iOS when the PWA is not in the foreground. This is more aggressive than Android Chrome's behavior.
- Storage allocated to Service Workers and their caches is subject to **eviction** if the device is low on storage and the PWA has not been used recently (typically after ~7 days for browser tabs; installed PWAs get longer retention).
- **No Background Sync API** (`SyncManager` interface). The `navigator.serviceWorker.ready.then(sw => sw.sync.register(...))` pattern does not work on iOS.
- **No Periodic Background Sync API** (`PeriodicSyncManager`). This remains a Chromium-only feature.

### 2. Web Push Notifications

**Introduced:** iOS 16.4 (March 2023) — also requires Safari 16.4.

**How it works:**
- Uses the standard Web Push API (`PushManager`, `PushSubscription`)
- Apple routes pushes through APNs (Apple Push Notification service) transparently — developers use the same VAPID-based Web Push standard
- The Service Worker receives `push` events and calls `self.registration.showNotification()`

**Critical constraint: Home screen installation required**
- Web Push **only works for PWAs installed to the home screen** (via "Add to Home Screen")
- Visiting the same site in a Safari browser tab does NOT allow push subscriptions, even if the user has previously installed it
- This is a fundamental design decision by Apple, not a temporary bug

**Silent push not supported:**
- Every push message must result in a visible notification shown to the user
- Background data delivery without a visible notification is not possible via Web Push on iOS
- This mirrors the web standard's intent but is stricter than what some Android implementations allow in practice

**Badging API:**
- `navigator.setAppBadge()` is supported on iOS 16.4+ for installed PWAs
- Badge counts can be updated via Service Worker push handlers

**Notification limitations compared to Android:**
- No notification channels (Android-specific concept)
- No persistent notifications (can be dismissed by user as normal)
- Notification actions (buttons) have limited support compared to Android Chrome

### 3. Install Prompt — The Biggest UX Gap

**`beforeinstallprompt` event: NOT supported on iOS Safari**

On Android Chrome, a PWA that meets the installability criteria triggers the `beforeinstallprompt` event, which developers can intercept to show a custom in-app install button/banner at the right moment. iOS Safari has never supported this event and Apple has not signaled plans to add it.

**iOS installation flow (manual only):**
1. User opens the site in Safari
2. User taps the Share button (the upload icon in the toolbar)
3. User scrolls to find "Add to Home Screen"
4. User confirms by tapping "Add"

This multi-step, discoverable-only-if-you-know-where-to-look flow results in significantly lower install rates on iOS compared to Android.

**Workarounds:**
- Display a custom in-app banner with instructions and an image of the Share button (detect iOS via user agent)
- Use the pattern: `if (/iphone|ipad|ipod/i.test(navigator.userAgent) && !window.matchMedia('(display-mode: standalone)').matches)`
- Libraries like `pwa-install-handler` or manual detection + overlay UI

**WKWebView note:**
- `beforeinstallprompt` is also not supported in WKWebView (used by all third-party iOS browsers)
- Third-party browsers on iOS (Chrome, Firefox, Edge) cannot trigger an install prompt either — they all use WKWebView under the hood and cannot add PWAs to the home screen at all (only Safari can do this)

### 4. Web App Manifest Support

**Supported manifest properties on iOS Safari:**
- `name` / `short_name` — used for the home screen icon label
- `icons` — used for the home screen icon (preferred: 192x192 and 512x512 PNG)
- `display: standalone` — removes Safari UI chrome when launched from home screen
- `display: fullscreen` — supported
- `background_color` — used for the splash screen background
- `theme_color` — partially supported (affects the status bar on some iOS versions)
- `start_url` — respected when launching from home screen
- `scope` — respected
- `description` — parsed but not surfaced in UI

**Not supported or partially supported on iOS:**
- `shortcuts` — app shortcuts (long-press on Android) not supported
- `share_target` — Web Share Target API not supported on iOS
- `display_override` — not supported
- `protocol_handlers` — not supported
- `file_handlers` — not supported
- `launch_handler` — not supported
- `screenshots` — not used by iOS for any UI

**Apple-specific meta tags (legacy, still needed for best results):**
```html
<!-- Mark as capable of running as a standalone web app -->
<meta name="apple-mobile-web-app-capable" content="yes">

<!-- Status bar appearance when running standalone -->
<meta name="apple-mobile-web-app-status-bar-style" content="default">
<!-- Options: default | black | black-translucent -->

<!-- Explicit app name (fallbacks to <title>) -->
<meta name="apple-mobile-web-app-title" content="Splitkauf">

<!-- Legacy touch icon (still recommended alongside manifest icons) -->
<link rel="apple-touch-icon" sizes="180x180" href="/icons/apple-touch-icon.png">
```

**Splash screens:**
- iOS generates a splash screen from `background_color` + icon + `name`
- For pixel-perfect splash screens on specific device sizes, `<link rel="apple-touch-startup-image">` with `media` queries targeting exact screen sizes is required — this is a significant maintenance burden
- As of iOS 17+, the automatically generated splash screen from the manifest is acceptable for most use cases

### 5. Offline Support

**Cache API:** Fully supported. Service Workers can intercept network requests and serve from cache, enabling full offline functionality.

**IndexedDB:** Fully supported in iOS Safari (since iOS 10). Suitable for storing structured data like shopping list items, user state, etc.

**Storage quotas:**
- In Safari (browser tab): Storage is treated as "third-party" data and subject to ITP (Intelligent Tracking Prevention) eviction after 7 days of non-use
- As an installed PWA (home screen): Storage is treated differently — longer retention, not subject to the 7-day ITP eviction, and gets a separate quota not shared with the browser tab origin
- Storage quota for installed PWAs: typically 50% of available disk space (shared quota), similar to other browsers
- `navigator.storage.persist()` can be called to request persistent storage — on iOS, this is automatically granted for installed PWAs

**Offline strategy for splitkauf:**
The Cache API + IndexedDB combination is sufficient to build full offline support for a shopping list app. Items can be stored in IndexedDB, sync operations can be queued locally, and the application shell can be served from cache. The missing Background Sync means that the sync must be triggered manually (on next app open) rather than automatically when connectivity returns while the app is backgrounded.

### 6. Background Sync and Data Synchronization

**Background Sync API (`SyncManager`):** NOT supported on iOS Safari. No timeline announced by Apple/WebKit.

**Periodic Background Sync API (`PeriodicSyncManager`):** NOT supported on iOS Safari. Chromium-only.

**Implications for splitkauf:**
- Cannot automatically sync pending changes (e.g., added/removed items) when the app is backgrounded and connectivity returns
- Must rely on foreground sync: when the user reopens the app, check for pending operations and sync them
- This is acceptable UX for a shopping list app — users are unlikely to need background sync of changes to be reflected before they next open the app
- Alternatively, consider using WebSockets or long-polling for real-time sync while the app is in the foreground (which works fine on iOS)

### 7. WKWebView vs. Safari — Key Distinction

All third-party browsers on iOS (Chrome, Firefox, Edge, Brave, Opera) use **WKWebView** as their rendering engine, mandated by Apple's App Store policies. WKWebView has important differences from full Safari:

| Feature | Safari on iOS | WKWebView (third-party browsers) |
|---|---|---|
| Service Workers | Full support | Limited/no support in WKWebView context |
| Web Push | Yes (installed PWA only) | Not available |
| Add to Home Screen | Yes | No — only Safari can add PWAs to home screen |
| IndexedDB | Full support | Full support |
| Cache API | Full support | Full support |
| PWA installation | Only via Safari | Cannot install PWAs |

**Consequence:** iOS PWA features are entirely dependent on the user using Safari. A user who opens the splitkauf URL in Chrome for iOS will not be able to install it as a PWA or receive push notifications. This is an Apple platform constraint, not a web standards issue.

---

## Feature Comparison: iOS Safari vs. Android Chrome

| Feature | iOS Safari 16.4+ | iOS Safari 18+ | Android Chrome |
|---|---|---|---|
| Service Workers | Yes | Yes | Yes |
| Cache API | Yes | Yes | Yes |
| IndexedDB | Yes | Yes | Yes |
| Web App Manifest | Partial | Partial | Full |
| Install to Home Screen | Manual only (Share > Add) | Manual only | Auto prompt (`beforeinstallprompt`) |
| `beforeinstallprompt` | No | No | Yes |
| Web Push (installed) | Yes (16.4+) | Yes | Yes |
| Web Push (in-browser) | No | No | Yes |
| Silent Push | No | No | No (spec disallows) |
| Background Sync | No | No | Yes |
| Periodic Background Sync | No | No | Yes |
| Badging API | Yes (16.4+) | Yes | Yes |
| App Shortcuts | No | No | Yes |
| Share Target | No | No | Yes |
| File System Access | No | No | Partial |
| Notification Actions | Limited | Limited | Full |
| Screen Wake Lock | Yes | Yes | Yes |
| Web Share API | Yes | Yes | Yes |
| Geolocation | Yes | Yes | Yes |
| Camera / Microphone | Yes | Yes | Yes |
| WebRTC | Yes | Yes | Yes |
| Payment Request | Yes | Yes | Yes |

---

## Best Practices for iOS-Targeted PWAs (2025-2026)

### Manifest and Icons

```json
{
  "name": "Splitkauf",
  "short_name": "Splitkauf",
  "description": "Collaborative shopping lists",
  "start_url": "/",
  "display": "standalone",
  "background_color": "#ffffff",
  "theme_color": "#007aff",
  "icons": [
    { "src": "/icons/icon-192.png", "sizes": "192x192", "type": "image/png" },
    { "src": "/icons/icon-512.png", "sizes": "512x512", "type": "image/png" },
    { "src": "/icons/icon-512-maskable.png", "sizes": "512x512", "type": "image/png", "purpose": "maskable" }
  ]
}
```

Also include in `<head>`:
```html
<meta name="apple-mobile-web-app-capable" content="yes">
<meta name="apple-mobile-web-app-status-bar-style" content="default">
<meta name="apple-mobile-web-app-title" content="Splitkauf">
<link rel="apple-touch-icon" href="/icons/icon-180.png">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
```

The `viewport-fit=cover` is critical for iPhone X+ to extend into the notch/dynamic island area and use CSS `env(safe-area-inset-*)` variables properly.

### Safe Area Insets

```css
body {
  padding-top: env(safe-area-inset-top);
  padding-bottom: env(safe-area-inset-bottom);
  padding-left: env(safe-area-inset-left);
  padding-right: env(safe-area-inset-right);
}
```

### Install Prompt for iOS (since `beforeinstallprompt` is not available)

```javascript
const isIOS = /iphone|ipad|ipod/i.test(navigator.userAgent);
const isStandalone = window.matchMedia('(display-mode: standalone)').matches
  || window.navigator.standalone === true;

if (isIOS && !isStandalone) {
  // Show custom banner: "To install Splitkauf, tap [Share icon] then 'Add to Home Screen'"
  showIOSInstallBanner();
}
```

### Push Notification Setup

Push subscriptions should only be requested after the user has installed the PWA (detected via `display-mode: standalone`) and explicitly opted in via a UI button — never on first launch. On iOS, requesting permission without a user gesture results in an automatic denial.

```javascript
async function subscribeToPush() {
  if (!('Notification' in window)) return;

  // Check if running as installed PWA (required on iOS for push to work)
  const isInstalled = window.matchMedia('(display-mode: standalone)').matches;
  if (!isInstalled) {
    alert('Please install the app first to enable notifications.');
    return;
  }

  const permission = await Notification.requestPermission();
  if (permission !== 'granted') return;

  const reg = await navigator.serviceWorker.ready;
  const sub = await reg.pushManager.subscribe({
    userVisibleOnly: true,
    applicationServerKey: VAPID_PUBLIC_KEY,
  });

  await fetch('/api/push-subscriptions', {
    method: 'POST',
    body: JSON.stringify(sub),
    headers: { 'Content-Type': 'application/json' },
  });
}
```

### Offline-First Data Strategy (without Background Sync)

Since Background Sync is not available on iOS, use an optimistic UI + foreground sync pattern:

1. Write to IndexedDB immediately (optimistic update)
2. Attempt network sync immediately if online (`navigator.onLine`)
3. If offline, queue in a "pending sync" IndexedDB store
4. On `online` event (fires when connectivity returns while app is in foreground), drain the queue
5. On app launch/visibility change (`visibilitychange`), check and drain the queue

```javascript
document.addEventListener('visibilitychange', () => {
  if (document.visibilityState === 'visible' && navigator.onLine) {
    syncPendingOperations();
  }
});

window.addEventListener('online', () => {
  syncPendingOperations();
});
```

---

## Relevance to Splitkauf

A collaborative shopping list app has these core needs:

| Need | iOS PWA Capability | Verdict |
|---|---|---|
| Offline read/write of list items | Cache API + IndexedDB — fully supported | Good |
| Real-time sync (WebSocket) | WebSockets work fine on iOS Safari | Good |
| Foreground sync when reconnecting | `online` event + manual drain — works | Acceptable |
| Background sync when app is closed | NOT supported (no Background Sync API) | Gap |
| Push notifications for shared list changes | Supported (installed PWA only, iOS 16.4+) | Acceptable with caveat |
| Install to home screen | Manual Share > Add to Home Screen | UX friction |
| Cross-platform (iOS + Android) | Single codebase works on both | Good |

**The key gaps for splitkauf on iOS:**
1. **Background Sync** — If a user adds items while offline and closes the app before connectivity returns, those changes won't sync until they reopen the app. For a shopping list, this is acceptable.
2. **Install friction** — Users must manually use the Share menu. An in-app instructions banner mitigates this.
3. **Push only for installed users** — Users who never install the PWA won't receive notifications. This limits the value of push for casual users.

---

## Open Questions

1. **iOS 18.x / 19 changes:** Apple's WebKit blog should be checked for any new PWA APIs shipped in iOS 18.x point releases or iOS 19 (expected WWDC 2025 announcement, shipping fall 2025). Knowledge cutoff for this report is August 2025.
2. **EU Digital Markets Act impact:** Apple was required to allow third-party browser engines in the EU in iOS 17.4. The policy was partially amended. The current status of Chromium-based browsers (with their own PWA support) on EU iOS devices should be verified — this could open richer PWA support for EU users of splitkauf.
3. **Capacitor as a bridge:** If the missing features (Background Sync, richer install UX) become blockers, Capacitor.js wraps a PWA in a native shell to gain access to native APIs while reusing the web codebase. This hybrid approach deserves a separate evaluation.

---

## References

The following sources were used to inform this report (based on knowledge through August 2025). Verify currency before making final decisions:

- WebKit Blog: https://webkit.org/blog — release notes for Safari 16.4, 17.x, 18.x
- MDN Web Push API: https://developer.mozilla.org/en-US/docs/Web/API/Push_API
- MDN Background Sync: https://developer.mozilla.org/en-US/docs/Web/API/Background_Synchronization_API
- caniuse.com Push API: https://caniuse.com/push-api
- caniuse.com Background Sync: https://caniuse.com/background-sync
- caniuse.com Service Workers: https://caniuse.com/serviceworkers
- web.dev PWA: https://web.dev/explore/progressive-web-apps
- Maximiliano Firtman's PWA compatibility tables: https://firt.dev/pwa-2024/ (most comprehensive iOS PWA reference)
- Apple Developer Notifications: https://developer.apple.com/notifications/
