import { useState } from 'react'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { QuantityField, parseQuantity } from './QuantityField'
import type { Unit } from './api'

/** A controlled host, so typing behaves exactly as it does in the real forms. */
function Harness({ initial = 1, unit = 'amount' as Unit, onChange = (_: number) => {} }) {
  const [value, setValue] = useState(initial)
  return (
    <QuantityField
      id="qty"
      value={value}
      unit={unit}
      onChange={(n) => {
        setValue(n)
        onChange(n)
      }}
    />
  )
}

describe('parseQuantity', () => {
  it('accepts integers ≥ 1, ignoring surrounding whitespace', () => {
    expect(parseQuantity('200')).toBe(200)
    expect(parseQuantity(' 7 ')).toBe(7)
  })

  it('floors a decimal draft (quantities are integers)', () => {
    expect(parseQuantity('1.9')).toBe(1)
  })

  it('rejects drafts that are not a submittable quantity', () => {
    expect(parseQuantity('')).toBeNull()
    expect(parseQuantity('0')).toBeNull()
    expect(parseQuantity('-3')).toBeNull()
    expect(parseQuantity('abc')).toBeNull()
  })
})

describe('QuantityField', () => {
  it('commits a typed quantity without any button taps', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    render(<Harness onChange={onChange} />)

    const input = screen.getByLabelText('Quantity')
    await user.clear(input)
    await user.type(input, '200')

    expect(onChange).toHaveBeenLastCalledWith(200)
    expect(input).toHaveValue(200)
  })

  it('allows an empty draft mid-typing and snaps back to the committed value on blur', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    render(<QuantityField id="qty" value={5} unit="amount" onChange={onChange} />)

    const input = screen.getByLabelText('Quantity')
    await user.clear(input)

    // An empty field is a draft, not a quantity: the parent keeps its 5.
    expect(input).toHaveValue(null)
    expect(onChange).not.toHaveBeenCalled()

    await user.tab()
    expect(input).toHaveValue(5)
  })

  it('steps by 1 and stops at the minimum of 1', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    const { rerender } = render(
      <QuantityField id="qty" value={1} unit="amount" onChange={onChange} />,
    )

    expect(screen.getByRole('button', { name: 'Decrease quantity' })).toBeDisabled()

    await user.click(screen.getByRole('button', { name: 'Increase quantity' }))
    expect(onChange).toHaveBeenLastCalledWith(2)

    rerender(<QuantityField id="qty" value={2} unit="amount" onChange={onChange} />)
    const decrease = screen.getByRole('button', { name: 'Decrease quantity' })
    expect(decrease).toBeEnabled()
    await user.click(decrease)
    expect(onChange).toHaveBeenLastCalledWith(1)
  })

  it('is labelled either implicitly or by its visible label', () => {
    const { unmount } = render(
      <QuantityField id="qty" value={1} unit="amount" onChange={() => {}} />,
    )
    expect(screen.getByLabelText('Quantity')).toHaveAttribute('id', 'qty')
    unmount()

    render(<QuantityField id="qty" label="Quantity" value={1} unit="amount" onChange={() => {}} />)
    // The visible <label> resolves to the same input, so no placeholder-as-label.
    expect(screen.getByText('Quantity').tagName).toBe('LABEL')
    expect(screen.getByLabelText('Quantity')).toHaveAttribute('id', 'qty')
  })
})
