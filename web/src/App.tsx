import { useCallback, useEffect, useRef, useState } from 'react'
import { clearFlows, connectFlowsWS, getConfig, getFlow, listFlows, putCapture } from './api'
import { captureSummary } from './capture'
import { loadVisible, normalizeVisible, saveVisible } from './columns'
import ActivityBar from './components/ActivityBar'
import ColumnPicker from './components/ColumnPicker'
import FilterBar from './components/FilterBar'
import FlowDetail from './components/FlowDetail'
import FlowTable from './components/FlowTable'
import SettingsPane from './components/SettingsPane'
import TopBar from './components/TopBar'
import type { ConfigSnapshot, Filters, Flow, FlowSummary } from './types'

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

function trimOldest(list: FlowSummary[], max: number): FlowSummary[] {
  if (max <= 0 || list.length <= max) return list
  return list.slice(list.length - max)
}

export default function App() {
  const [flows, setFlows] = useState<FlowSummary[]>([])
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [filters, setFilters] = useState<Filters>({ host: '', path: '', status: '' })
  const [visible, setVisible] = useState<string[]>(() => loadVisible())
  const onVisible = (ids: string[]) => {
    const next = normalizeVisible(ids)
    setVisible(next)
    saveVisible(next)
  }
  const [detail, setDetail] = useState<Flow | null>(null)
  const [loading, setLoading] = useState(false)
  const [snapshot, setSnapshot] = useState<ConfigSnapshot | null>(null)
  const [loadError, setLoadError] = useState(false)
  const [view, setView] = useState<'capture' | 'settings'>('capture')
  const writing = useRef(false)
  const [writeLocked, setWriteLocked] = useState(false)
  const [toggleError, setToggleError] = useState('')
  const maxFlows = snapshot?.runtime.max_flows ?? 1000
  const filtersRef = useRef(filters)
  const maxFlowsRef = useRef(maxFlows)
  filtersRef.current = filters
  maxFlowsRef.current = maxFlows

  useEffect(() => {
    getConfig()
      .then((next) => {
        setSnapshot(next)
        document.documentElement.dataset.theme = next.runtime.theme
      })
      .catch(() => {
        setLoadError(true)
      })
  }, [])

  const refetch = useCallback(async () => {
    try {
      const list = await listFlows(filtersRef.current)
      const next = trimOldest([...list].reverse(), maxFlowsRef.current)
      setFlows(next)
      return next
    } catch {
      /* keep current state; WS reconnect will retry */
      return null
    }
  }, [])

  const onSaved = (next: ConfigSnapshot) => {
    setSnapshot(next)
    document.documentElement.dataset.theme = next.runtime.theme
    if (next.runtime.max_flows === maxFlowsRef.current) return
    maxFlowsRef.current = next.runtime.max_flows
    void refetch().then((list) => {
      if (!list) return
      setSelectedId((cur) => (cur && list.some((flow) => flow.id === cur) ? cur : null))
    })
  }

  useEffect(() => {
    const t = window.setTimeout(refetch, 150)
    return () => window.clearTimeout(t)
  }, [filters, refetch])

  useEffect(() => {
    return connectFlowsWS(
      (ev) => {
        if (ev.type === 'flows.cleared') {
          setFlows([])
          setSelectedId(null)
          return
        }
        if (ev.type === 'flow.evicted' && ev.flow_id) {
          const id = ev.flow_id
          setFlows((prev) => prev.filter((p) => p.id !== id))
          setSelectedId((cur) => (cur === id ? null : cur))
          return
        }
        const f = ev.flow
        if (!f) return
        setFlows((prev) => {
          const idx = prev.findIndex((p) => p.id === f.id)
          const ok = matches(f, filtersRef.current)
          let next: FlowSummary[]
          if (idx >= 0) {
            next = prev.slice()
            if (ok) next[idx] = f
            else next.splice(idx, 1)
          } else {
            next = ok ? [...prev, f] : prev
          }
          return trimOldest(next, maxFlowsRef.current)
        })
      },
      () => void refetch(),
    )
  }, [refetch])

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

  const beginWrite = (): boolean => {
    if (writing.current) return false
    writing.current = true
    setWriteLocked(true)
    return true
  }

  const endWrite = () => {
    writing.current = false
    setWriteLocked(false)
  }

  const onToggle = async () => {
    if (!beginWrite()) return
    try {
      const next = await putCapture(!(snapshot?.file.capture ?? false))
      setToggleError('')
      onSaved(next)
    } catch (err) {
      setToggleError(err instanceof Error ? err.message : '无法写入配置')
    } finally {
      endWrite()
    }
  }

  const summary = captureSummary(snapshot?.file ?? null, loadError)

  return (
    <div className="app">
      <TopBar
        onClear={onClear}
        count={flows.length}
        proxy={snapshot?.runtime.proxy ?? ''}
        capture={snapshot ? snapshot.file.capture : null}
        disabled={writeLocked}
        error={toggleError}
        onToggle={() => void onToggle()}
      />
      <div className="shell">
        <ActivityBar
          open={view === 'settings'}
          onToggle={() => setView((current) => (current === 'settings' ? 'capture' : 'settings'))}
        />
        <SettingsPane
          snapshot={snapshot}
          loadError={loadError}
          open={view === 'settings'}
          onSaved={onSaved}
          writeLocked={writeLocked}
          beginWrite={beginWrite}
          endWrite={endWrite}
        />
        {view === 'capture' ? (
          <div className="capture">
            <div className="filterbar">
              <div className="filterbar-main">
                <FilterBar filters={filters} onChange={setFilters} />
              </div>
              <ColumnPicker visible={visible} onChange={onVisible} />
            </div>
            <p className="capture-summary" title={summary.title}>
              {summary.text}
            </p>
            <main className="workspace">
              <div className="pane pane-top">
                <FlowTable flows={flows} selectedId={selectedId} onSelect={setSelectedId} visible={visible} />
              </div>
              <div className="pane pane-bottom">
                <FlowDetail flow={detail} loading={loading} />
              </div>
            </main>
          </div>
        ) : null}
      </div>
    </div>
  )
}
