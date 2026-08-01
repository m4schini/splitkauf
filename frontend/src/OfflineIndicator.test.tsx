import { render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { onlineManager } from '@tanstack/react-query'
import { act } from 'react'
import { OfflineIndicator } from './OfflineIndicator'

afterEach(() => {
  onlineManager.setOnline(true)
})

describe('OfflineIndicator', () => {
  it('renders nothing while online', () => {
    onlineManager.setOnline(true)
    render(<OfflineIndicator />)

    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('shows a quiet status banner when onlineManager reports offline', () => {
    render(<OfflineIndicator />)

    act(() => {
      onlineManager.setOnline(false)
    })

    const status = screen.getByRole('status')
    expect(status).toHaveTextContent(/offline/i)
    expect(status).toHaveTextContent(/sync when you're back online/i)
  })

  it('clears the banner once back online', () => {
    render(<OfflineIndicator />)

    act(() => {
      onlineManager.setOnline(false)
    })
    expect(screen.getByRole('status')).toBeInTheDocument()

    act(() => {
      onlineManager.setOnline(true)
    })
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })
})
