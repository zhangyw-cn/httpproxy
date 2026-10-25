import { useEffect, useRef, useState } from 'react'
import { COLUMNS } from '../columns'

interface Props {
  visible: string[]
  onChange: (ids: string[]) => void
}

export default function ColumnPicker({ visible, onChange }: Props) {
  const [open, setOpen] = useState(false)
  const rootRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onPointerDown = (event: PointerEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false)
    }
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false)
    }
    document.addEventListener('pointerdown', onPointerDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('pointerdown', onPointerDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  const toggle = (id: string) => {
    const next = new Set(visible)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    onChange([...next])
  }

  return (
    <div className="col-picker" ref={rootRef}>
      <button
        type="button"
        className={`btn col-picker-btn${open ? ' open' : ''}`}
        aria-expanded={open}
        aria-haspopup="true"
        onClick={() => setOpen((value) => !value)}
      >列</button>
      {open && (
        <div className="col-menu">
          {COLUMNS.map((c) => (
            <label key={c.id} className="col-menu-item">
              <input
                type="checkbox"
                checked={c.locked || visible.includes(c.id)}
                disabled={c.locked}
                onChange={() => {
                  if (!c.locked) toggle(c.id)
                }}
              />
              {c.label}
            </label>
          ))}
        </div>
      )}
    </div>
  )
}
