import { useState } from 'react'
import type { Unit } from './api'

/**
 * The quantity a `QuantityField` reports for a raw input draft, or null when
 * the draft is not a submittable quantity yet.
 *
 * Quantities are integers ≥ 1 across the OpenAPI spec, the domain and the
 * database, so a decimal draft is floored ("1.9" → 1) and anything empty,
 * zero, negative or non-numeric yields null — the field then keeps its last
 * valid value instead of snapping the parent's state to 1 mid-typing.
 */
export function parseQuantity(draft: string): number | null {
  const trimmed = draft.trim()
  const n = Math.floor(Number(trimmed))
  return trimmed !== '' && Number.isFinite(n) && n >= 1 ? n : null
}

export interface QuantityFieldProps {
  /** id of the number input; pairs with the visible or implicit label. */
  id: string
  /** Visible label text; when omitted the input carries aria-label="Quantity". */
  label?: string
  value: number
  unit: Unit
  /** Called only with a valid integer ≥ 1. */
  onChange: (quantity: number) => void
  /** Optional data-testid for the number input (quick-add keeps `quick-add-quantity`). */
  inputTestId?: string
}

/**
 * A typeable quantity control: `−`/`+` step by 1 with a minimum of 1, and the
 * value between them is a number input, so "200 g" is typed rather than tapped
 * 199 times (US-L.12).
 *
 * While the user types, the raw string lives in `draft` and only parses that
 * yield a valid integer ≥ 1 reach `onChange`. That keeps the parent's quantity
 * always submittable while still letting the field be empty mid-typing —
 * clamping every keystroke (`Number(v) || 1`) would turn clearing the field
 * into a `1`, so typing "200" would become "1200". Blur drops the draft, so
 * the display snaps back to the committed value.
 */
export function QuantityField({ id, label, value, onChange, inputTestId }: QuantityFieldProps) {
  const [draft, setDraft] = useState<string | null>(null)

  /** Sets an explicit quantity (stepper button), discarding any pending draft. */
  function commit(quantity: number) {
    setDraft(null)
    onChange(Math.max(1, quantity))
  }

  return (
    <div className="quantity-field">
      {label && <label htmlFor={id}>{label}</label>}
      {/* No group label: the input itself is named "Quantity" now, and a group
          carrying the same name would just be a second thing by that name. */}
      <div className="stepper">
        <button
          type="button"
          className="stepper-button"
          aria-label="Decrease quantity"
          onClick={() => commit(value - 1)}
          disabled={value <= 1}
        >
          −
        </button>
        <input
          id={id}
          className="stepper-input"
          type="number"
          inputMode="numeric"
          min={1}
          step={1}
          aria-label={label ? undefined : 'Quantity'}
          data-testid={inputTestId}
          value={draft ?? String(value)}
          onChange={(e) => {
            setDraft(e.target.value)
            const parsed = parseQuantity(e.target.value)
            if (parsed !== null) onChange(parsed)
          }}
          onBlur={() => setDraft(null)}
        />
        <button
          type="button"
          className="stepper-button"
          aria-label="Increase quantity"
          onClick={() => commit(value + 1)}
        >
          +
        </button>
      </div>
    </div>
  )
}
