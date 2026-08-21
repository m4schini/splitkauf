import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { PersistQueryClientProvider } from '@tanstack/react-query-persist-client'
import './index.css'
import App from './App.tsx'
import { queryClient, persister, cacheBuster, sevenDaysMs, dehydrateOptions } from './queryClient'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <PersistQueryClientProvider
      client={queryClient}
      persistOptions={{
        persister,
        maxAge: sevenDaysMs,
        buster: cacheBuster,
        // Persist the paused-mutation outbox alongside the cache (US-O.2 Key
        // Decision 2) so offline changes survive a tab close / reload; the
        // auth-mode query is deliberately excluded (see queryClient.ts).
        dehydrateOptions,
      }}
      // Replay the outbox once the cache is restored on startup (US-O.3).
      onSuccess={() => queryClient.resumePausedMutations()}
    >
      <App />
    </PersistQueryClientProvider>
  </StrictMode>,
)
