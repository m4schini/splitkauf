import { useEffect, useState } from 'react'

interface HealthStatus {
  status: string
}

function App() {
  const [status, setStatus] = useState<string>('checking')

  useEffect(() => {
    let cancelled = false

    fetch('/api/v1/health')
      .then((res) => res.json() as Promise<HealthStatus>)
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
