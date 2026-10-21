export interface ColumnDef {
  id: string
  label: string
  locked: boolean
  width: number | 'auto'
  align: 'left' | 'right'
  defaultVisible: boolean
}

export const COLUMNS: ColumnDef[] = [
  { id: 'name', label: '名称', locked: true, width: 'auto', align: 'left', defaultVisible: true },
  { id: 'host', label: '主机', locked: false, width: 180, align: 'left', defaultVisible: true },
  { id: 'status', label: '状态', locked: false, width: 80, align: 'left', defaultVisible: true },
  { id: 'method', label: '方法', locked: false, width: 72, align: 'left', defaultVisible: true },
  { id: 'version', label: '版本', locked: false, width: 72, align: 'left', defaultVisible: true },
  { id: 'size', label: '大小', locked: false, width: 80, align: 'right', defaultVisible: true },
  { id: 'reqSize', label: '请求大小', locked: false, width: 88, align: 'right', defaultVisible: false },
  { id: 'time', label: '耗时', locked: false, width: 80, align: 'right', defaultVisible: true },
  { id: 'started', label: '开始时间', locked: false, width: 112, align: 'left', defaultVisible: false },
  { id: 'client', label: '客户端地址', locked: false, width: 148, align: 'left', defaultVisible: false },
  { id: 'error', label: '错误', locked: false, width: 200, align: 'left', defaultVisible: false },
]

export const COLUMN_STORAGE_KEY = 'httpproxy.columns'

const OPTIONAL_ORDER = COLUMNS.filter((c) => !c.locked).map((c) => c.id)
const OPTIONAL_IDS = new Set(OPTIONAL_ORDER)

export const DEFAULT_VISIBLE: string[] = COLUMNS.filter((c) => !c.locked && c.defaultVisible).map((c) => c.id)

export function normalizeVisible(raw: unknown): string[] {
  if (!Array.isArray(raw)) return [...DEFAULT_VISIBLE]
  const present = new Set<string>()
  for (const item of raw) {
    if (typeof item === 'string' && OPTIONAL_IDS.has(item)) present.add(item)
  }
  return OPTIONAL_ORDER.filter((id) => present.has(id))
}

export function loadVisible(storage?: Pick<Storage, 'getItem'>): string[] {
  try {
    const store = storage ?? localStorage
    const text = store.getItem(COLUMN_STORAGE_KEY)
    if (text == null) return [...DEFAULT_VISIBLE]
    return normalizeVisible(JSON.parse(text))
  } catch {
    return [...DEFAULT_VISIBLE]
  }
}

export function saveVisible(ids: string[], storage?: Pick<Storage, 'setItem'>): void {
  try {
    const store = storage ?? localStorage
    store.setItem(COLUMN_STORAGE_KEY, JSON.stringify(normalizeVisible(ids)))
  } catch {
    /* in-memory selection stays */
  }
}

export function formatStarted(raw: string): string {
  const d = new Date(raw)
  if (Number.isNaN(d.getTime())) return raw
  const p = (n: number, w = 2) => String(n).padStart(w, '0')
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}.${p(d.getMilliseconds(), 3)}`
}

export interface CellFlow {
  scheme: string
  method: string
  host: string
  path: string
  http_version: string
  status_code: number
  req_size: number
  resp_size: number
  started_at: string
  duration_ms: number
  error?: string
  client_addr: string
}

export interface CellFormats {
  size: (n: number) => string
  time: (ms: number) => string
}

export interface FlowCell {
  text: string
  title?: string
  statusClass?: 'st-err' | 'st-pending' | 'st-ok' | 'st-redirect'
}

function titled(text: string): FlowCell {
  return text ? { text, title: text } : { text }
}

function statusCell(flow: CellFlow): FlowCell {
  if (flow.error) return { text: '(失败)', statusClass: 'st-err' }
  if (!flow.status_code) return { text: '(等待)', statusClass: 'st-pending' }
  const hundred = Math.floor(flow.status_code / 100)
  const statusClass = hundred >= 4 ? 'st-err' : hundred === 3 ? 'st-redirect' : 'st-ok'
  return { text: String(flow.status_code), statusClass }
}

export function flowCell(id: string, flow: CellFlow, format: CellFormats): FlowCell {
  switch (id) {
    case 'name': {
      const path = flow.path || '/'
      return { text: path, title: `${flow.scheme}://${flow.host}${path}` }
    }
    case 'host':
      return titled(flow.host)
    case 'status':
      return statusCell(flow)
    case 'method':
      return { text: flow.method }
    case 'version':
      return { text: flow.http_version }
    case 'size':
      return { text: format.size(flow.resp_size) }
    case 'reqSize':
      return { text: format.size(flow.req_size) }
    case 'time':
      return { text: flow.status_code || flow.error ? format.time(flow.duration_ms) : '…' }
    case 'started':
      return { text: formatStarted(flow.started_at) }
    case 'client':
      return titled(flow.client_addr)
    case 'error':
      return titled(flow.error ?? '')
    default:
      return { text: '' }
  }
}

export function activeColumns(visible: string[]): ColumnDef[] {
  const on = new Set(normalizeVisible(visible))
  return COLUMNS.filter((c) => c.locked || on.has(c.id))
}

export function tableMinWidth(visible: string[]): number {
  return activeColumns(visible).reduce((sum, c) => sum + (typeof c.width === 'number' ? c.width : 160), 0)
}
