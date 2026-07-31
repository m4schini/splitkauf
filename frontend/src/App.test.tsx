import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import App from './App'

describe('App', () => {
  it('renders', () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.reject(new Error('no network in tests'))),
    )

    render(<App />)

    expect(screen.getByRole('heading', { level: 1 })).toBeInTheDocument()
  })
})
