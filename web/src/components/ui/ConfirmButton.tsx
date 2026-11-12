import { useEffect, useState, type ReactNode } from 'react'

/** A button that asks for a second click before acting. */
export function ConfirmButton({
  onConfirm,
  children,
  confirmLabel = 'Confirm',
  disabled,
  className = '',
}: {
  onConfirm: () => void
  children: ReactNode
  confirmLabel?: string
  disabled?: boolean
  className?: string
}) {
  const [armed, setArmed] = useState(false)

  useEffect(() => {
    if (!armed) return
    const t = setTimeout(() => setArmed(false), 4000)
    return () => clearTimeout(t)
  }, [armed])

  return armed ? (
    <span className="inline-flex gap-2">
      <button
        type="button"
        disabled={disabled}
        onClick={() => {
          setArmed(false)
          onConfirm()
        }}
        className={`rounded bg-red-700 px-2 py-1 text-sm font-medium text-white disabled:opacity-50 ${className}`}
      >
        {confirmLabel}
      </button>
      <button type="button" onClick={() => setArmed(false)} className="text-sm text-stone-600">
        Cancel
      </button>
    </span>
  ) : (
    <button
      type="button"
      disabled={disabled}
      onClick={() => setArmed(true)}
      className={`text-sm text-red-700 hover:underline disabled:opacity-50 ${className}`}
    >
      {children}
    </button>
  )
}
