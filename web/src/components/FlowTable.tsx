import { useEffect, useRef } from 'react'
import type { FlowSummary } from '../types'

interface Props {
  flows: FlowSummary[]
  selectedId: string | null
  onSelect: (id: string) => void
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

function statusCell(f: FlowSummary): { text: string; cls: string } {
  if (f.error) return { text: '(failed)', cls: 'st-err' }
  if (!f.status_code) return { text: '(pending)', cls: 'st-pending' }
  const c = Math.floor(f.status_code / 100)
  return { text: String(f.status_code), cls: c >= 4 ? 'st-err' : c === 3 ? 'st-redirect' : 'st-ok' }
}

export default function FlowTable({ flows, selectedId, onSelect }: Props) {
  const bodyRef = useRef<HTMLDivElement>(null)
  const stick = useRef(true)

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
      <table className="flow-table">
        <thead>
          <tr>
            <th className="col-name">Name</th>
            <th className="col-status">Status</th>
            <th className="col-method">Method</th>
            <th className="col-version">Version</th>
            <th className="col-size">Size</th>
            <th className="col-time">Time</th>
          </tr>
        </thead>
        <tbody>
          {flows.map((f) => {
            const st = statusCell(f)
            return (
              <tr
                key={f.id}
                className={f.id === selectedId ? 'selected' : st.cls === 'st-err' ? 'row-err' : ''}
                onClick={() => onSelect(f.id)}
              >
                <td className="col-name" title={`${f.scheme}://${f.host}${f.path}`}>
                  <span className="name-path">{f.path || '/'}</span>
                  <span className="name-host">{f.host}</span>
                </td>
                <td className={`col-status ${st.cls}`}>{st.text}</td>
                <td className="col-method">{f.method}</td>
                <td className="col-version">{f.http_version}</td>
                <td className="col-size">{formatSize(f.resp_size)}</td>
                <td className="col-time">{f.status_code || f.error ? formatTime(f.duration_ms) : '…'}</td>
              </tr>
            )
          })}
          {flows.length === 0 && (
            <tr className="empty">
              <td colSpan={6}>No requests captured. Point a client at the proxy to start recording.</td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  )
}
