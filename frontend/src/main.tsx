import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { PersistQueryClientProvider } from '@tanstack/react-query-persist-client'
import './index.css'
import App from './App.tsx'
import { queryClient, persister, cacheBuster, sevenDaysMs } from './queryClient'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <PersistQueryClientProvider
      client={queryClient}
      persistOptions={{ persister, maxAge: sevenDaysMs, buster: cacheBuster }}
    >
      <App />
    </PersistQueryClientProvider>
  </StrictMode>,
)
