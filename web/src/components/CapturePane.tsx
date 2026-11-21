import { useState } from 'react'
import { captureActionLabel } from '../capture'
import type { ConfigSnapshot, Filters, Flow, FlowSummary } from '../types'
import CaptureRules from './CaptureRules'
import ColumnPicker from './ColumnPicker'
import FilterBar from './FilterBar'
import FlowDetail from './FlowDetail'
import FlowTable from './FlowTable'
import RulesMark from './RulesMark'

interface Props {
  flows: FlowSummary[]
  selectedId: string | null
  onSelect: (id: string) => void
  filters: Filters
  onFilters: (filters: Filters) => void
  visible: string[]
  onVisible: (ids: string[]) => void
  detail: Flow | null
  loading: boolean
  dock: 'bottom' | 'right'
  onToggleDock: () => void
  snapshot: ConfigSnapshot | null
  loadError: boolean
  onClear: () => void
  onToggleCapture: () => void
  captureBusy: boolean
  captureError: string
  onSaved: (snapshot: ConfigSnapshot) => void
}

export default function CapturePane({
  flows,
  selectedId,
  onSelect,
  filters,
  onFilters,
  visible,
  onVisible,
  detail,
  loading,
  dock,
  onToggleDock,
  snapshot,
  loadError,
  onClear,
  onToggleCapture,
  captureBusy,
  captureError,
  onSaved,
}: Props) {
  const [rulesOpen, setRulesOpen] = useState(false)
  const capture = snapshot?.file.capture

  return (
    <div className="capture">
      <div className="capture-toolbar">
        <button
          className="btn"
          type="button"
          disabled={captureBusy || snapshot == null}
          onClick={onToggleCapture}
        >
          {captureActionLabel(capture ?? null)}
        </button>
        <span className="muted">{flows.length} 条请求</span>
        <button className="btn" type="button" onClick={onClear}>
          清空
        </button>
        {captureError ? <span className="toolbar-error">{captureError}</span> : null}
        <div className="capture-toolbar-actions">
          <div className="rules-menu">
            <button
              className="btn"
              type="button"
              disabled={snapshot == null}
              aria-haspopup="dialog"
              aria-expanded={rulesOpen}
              onClick={() => setRulesOpen(true)}
            >
              规则
            </button>
            {rulesOpen && snapshot ? (
              <CaptureRules
                snapshot={snapshot}
                onSaved={onSaved}
                onClose={() => setRulesOpen(false)}
              />
            ) : null}
          </div>
        </div>
      </div>
      {capture === false ? (
        <div className="notice">抓包已关闭，HTTPS 以隧道转发，不需要 CA。已有记录保留。</div>
      ) : null}
      <div className="filterbar">
        <div className="filterbar-main">
          <FilterBar filters={filters} onChange={onFilters} />
        </div>
        <RulesMark file={snapshot?.file ?? null} failed={loadError} />
        <ColumnPicker visible={visible} onChange={onVisible} />
      </div>
      <main className={dock === 'right' ? 'workspace dock-right' : 'workspace dock-bottom'}>
        <div className="pane pane-list">
          <FlowTable flows={flows} selectedId={selectedId} onSelect={onSelect} visible={visible} />
        </div>
        <div className="pane pane-detail">
          <FlowDetail flow={detail} loading={loading} dock={dock} onToggleDock={onToggleDock} />
        </div>
      </main>
    </div>
  )
}
