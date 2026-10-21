import { useEffect, useRef, type CSSProperties } from 'react'
import { activeColumns, flowCell, tableMinWidth, type ColumnDef } from '../columns'
import type { FlowSummary } from '../types'

interface Props {
  flows: FlowSummary[]
  selectedId: string | null
  onSelect: (id: string) => void
  visible: string[]
}

export function formatSize(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} kB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

export function formatTime(ms: number): string {
  if (ms < 1000) return `${ms} ms`
  return `${(ms / 1000).toFixed(2)} s`
}

const formats = { size: formatSize, time: formatTime }

function colStyle(column: ColumnDef): CSSProperties {
  if (column.width === 'auto') return { minWidth: 160, textAlign: column.align }
  return { width: column.width, textAlign: column.align }
}

export default function FlowTable({ flows, selectedId, onSelect, visible }: Props) {
  const bodyRef = useRef<HTMLDivElement>(null)
  const stick = useRef(true)
  const cols = activeColumns(visible)

  useEffect(() => {
    const el = bodyRef.current
    if (el && stick.current) el.scrollTop = el.scrollHeight
  }, [flows.length])

  const onScroll = () => {
    const el = bodyRef.current
    if (el) stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 24
  }

  return (
    <div className="table-wrap" ref={bodyRef} onScroll={onScroll}>
      <table className="flow-table" style={{ minWidth: tableMinWidth(visible) }}>
        <thead>
          <tr>
            {cols.map((c) => (
              <th key={c.id} style={colStyle(c)}>{c.label}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {flows.map((f) => {
            const status = flowCell('status', f, formats)
            return (
              <tr
                key={f.id}
                className={f.id === selectedId ? 'selected' : status.statusClass === 'st-err' ? 'row-err' : ''}
                onClick={() => onSelect(f.id)}
              >
                {cols.map((c) => {
                  const cell = c.id === 'status' ? status : flowCell(c.id, f, formats)
                  const statusCls = c.id === 'status' && cell.statusClass ? ` ${cell.statusClass}` : ''
                  return (
                    <td key={c.id} className={statusCls.trim()} style={colStyle(c)} title={cell.title}>
                      {c.id === 'name' ? <span className="name-path">{cell.text}</span> : cell.text}
                    </td>
                  )
                })}
              </tr>
            )
          })}
          {flows.length === 0 && (
            <tr className="empty">
              <td colSpan={cols.length}>尚未捕获请求。将客户端指向该代理后即可开始记录。</td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  )
}
