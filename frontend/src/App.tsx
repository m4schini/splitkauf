import { useEffect, useState } from 'react'
import { apiFetch } from './api'

interface HealthStatus {
  status: string
}

function App() {
  const [status, setStatus] = useState<string>('checking')

  useEffect(() => {
    let cancelled = false

    apiFetch<HealthStatus>('/api/v1/health')
      .then((data) => {
        if (!cancelled) setStatus(data.status)
      })
      .catch(() => {
        if (!cancelled) setStatus('degraded')
      })

    return () => {
      cancelled = true
    }
  }, [])

  return (
    <main>
      <h1>Splitkauf — API: {status}</h1>
    </main>
  )
}

export default App
