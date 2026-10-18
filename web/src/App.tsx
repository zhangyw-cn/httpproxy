import { useCallback, useEffect, useRef, useState } from 'react'
import { clearFlows, connectFlowsWS, getFlow, listFlows } from './api'
import FilterBar from './components/FilterBar'
import FlowDetail from './components/FlowDetail'
import FlowTable from './components/FlowTable'
import TopBar from './components/TopBar'
import type { Filters, Flow, FlowSummary } from './types'

function matchStatus(code: number, filter: string): boolean {
  if (!filter) return true
  switch (filter.length) {
    case 1: return Math.floor(code / 100) === Number(filter)
    case 2: return Math.floor(code / 10) === Number(filter)
    case 3: return code === Number(filter)
    default: return false
  }
}

function matches(f: FlowSummary, flt: Filters): boolean {
  return f.host.includes(flt.host) && f.path.includes(flt.path) && matchStatus(f.status_code, flt.status)
}

export default function App() {
  const [flows, setFlows] = useState<FlowSummary[]>([])
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [filters, setFilters] = useState<Filters>({ host: '', path: '', status: '' })
  const [detail, setDetail] = useState<Flow | null>(null)
  const [loading, setLoading] = useState(false)
  const filtersRef = useRef(filters)
  filtersRef.current = filters

  const refetch = useCallback(async () => {
    try {
      const list = await listFlows(filtersRef.current)
      setFlows([...list].reverse()) // server is newest-first; Network shows oldest-first
    } catch {
      /* keep current state; WS reconnect will retry */
    }
  }, [])

  // Refetch on filter change (debounced).
  useEffect(() => {
    const t = window.setTimeout(refetch, 150)
    return () => window.clearTimeout(t)
  }, [filters, refetch])

  // WebSocket: merge events; refetch on every (re)connect.
  useEffect(() => {
    return connectFlowsWS(
      (ev) => {
        if (ev.type === 'flows.cleared') {
          setFlows([])
          setSelectedId(null)
          return
        }
        const f = ev.flow
        if (!f) return
        setFlows((prev) => {
          const idx = prev.findIndex((p) => p.id === f.id)
          const ok = matches(f, filtersRef.current)
          if (idx >= 0) {
            const next = prev.slice()
            if (ok) next[idx] = f
            else next.splice(idx, 1)
            return next
          }
          return ok ? [...prev, f] : prev
        })
      },
      () => void refetch(),
    )
  }, [refetch])

  // Detail fetch on selection; refresh when the selected flow updates.
  const selected = flows.find((f) => f.id === selectedId)
  const selectedStamp = selected ? `${selected.status_code}|${selected.resp_size}|${selected.duration_ms}|${selected.error ?? ''}` : ''
  useEffect(() => {
    if (!selectedId) {
      setDetail(null)
      return
    }
    let cancelled = false
    setLoading(true)
    getFlow(selectedId)
      .then((f) => !cancelled && setDetail(f))
      .catch(() => !cancelled && setDetail(null))
      .finally(() => !cancelled && setLoading(false))
    return () => {
      cancelled = true
    }
  }, [selectedId, selectedStamp])

  const onClear = async () => {
    await clearFlows()
    setFlows([])
    setSelectedId(null)
  }

  return (
    <div className="app">
      <TopBar onClear={onClear} count={flows.length} />
      <FilterBar filters={filters} onChange={setFilters} />
      <main className="workspace">
        <div className="pane pane-top">
          <FlowTable flows={flows} selectedId={selectedId} onSelect={setSelectedId} />
        </div>
        <div className="pane pane-bottom">
          <FlowDetail flow={detail} loading={loading} />
        </div>
      </main>
    </div>
  )
}
