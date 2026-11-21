import type { CaptureRules } from './capture'
import type {
  ConfigSnapshot,
  ConfigValues,
  Filters,
  Flow,
  FlowEvent,
  FlowSummary,
  HostDetail,
  HostSaveResult,
  HostsList,
  Overview,
} from './types'

/** Reads a JSON {error} body for a non-2xx response, else uses fallback. */
async function readError(r: Response, fallback: string): Promise<Error> {
  try {
    const body: { error?: unknown } = await r.json()
    if (typeof body.error === 'string' && body.error !== '') return new Error(body.error)
  } catch {
    /* body has no error string */
  }
  return new Error(fallback)
}

export async function getConfig(): Promise<ConfigSnapshot> {
  const r = await fetch('/api/config')
  if (!r.ok) throw new Error(`config: ${r.status}`)
  return r.json()
}

export async function putConfig(file: ConfigValues): Promise<ConfigSnapshot> {
  const r = await fetch('/api/config', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(file),
  })
  if (!r.ok) throw await readError(r, '无法写入配置')
  return r.json()
}

export async function putCapture(capture: boolean): Promise<ConfigSnapshot> {
  const r = await fetch('/api/capture', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ capture }),
  })
  if (!r.ok) throw await readError(r, '无法写入配置')
  return r.json()
}

export async function getOverview(): Promise<Overview> {
  const r = await fetch('/api/overview')
  if (!r.ok) throw await readError(r, '无法读取概览')
  return r.json()
}

export async function listHosts(): Promise<HostsList> {
  const r = await fetch('/api/hosts')
  if (!r.ok) throw await readError(r, '无法读取 Hosts 配置')
  return r.json()
}

export async function getHost(name: string): Promise<HostDetail> {
  const r = await fetch(`/api/hosts/${encodeURIComponent(name)}`)
  if (!r.ok) throw await readError(r, '无法读取 Hosts 配置')
  return r.json()
}

export async function saveHost(name: string, content: string): Promise<HostSaveResult> {
  const r = await fetch(`/api/hosts/${encodeURIComponent(name)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ content }),
  })
  if (!r.ok) throw await readError(r, '无法保存 Hosts 配置')
  return r.json()
}

export async function createHost(name: string): Promise<void> {
  const r = await fetch('/api/hosts', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  })
  if (!r.ok) throw await readError(r, '无法创建 Hosts 配置')
}

export async function renameHost(from: string, to: string): Promise<void> {
  const r = await fetch(`/api/hosts/${encodeURIComponent(from)}/rename`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name: to }),
  })
  if (!r.ok) throw await readError(r, '无法重命名 Hosts 配置')
}

export async function deleteHost(name: string): Promise<void> {
  const r = await fetch(`/api/hosts/${encodeURIComponent(name)}`, { method: 'DELETE' })
  if (!r.ok) throw await readError(r, '无法删除 Hosts 配置')
}

export async function activateHost(name: string): Promise<HostsList> {
  const r = await fetch('/api/hosts', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ active: name }),
  })
  if (!r.ok) throw await readError(r, '无法启用 Hosts 配置')
  return r.json()
}

export async function putCaptureRules(rules: CaptureRules): Promise<ConfigSnapshot> {
  const r = await fetch('/api/capture/rules', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(rules),
  })
  if (!r.ok) throw await readError(r, '无法写入配置')
  return r.json()
}

export async function listFlows(params: Partial<Filters>): Promise<FlowSummary[]> {
  const q = new URLSearchParams()
  if (params.host) q.set('host', params.host)
  if (params.path) q.set('path', params.path)
  if (params.status) q.set('status', params.status)
  const r = await fetch(`/api/flows?${q}`)
  if (!r.ok) throw new Error(`list flows: ${r.status}`)
  return (await r.json()) ?? []
}

export async function getFlow(id: string): Promise<Flow> {
  const r = await fetch(`/api/flows/${encodeURIComponent(id)}`)
  if (!r.ok) throw new Error(`get flow: ${r.status}`)
  return r.json()
}

export async function clearFlows(): Promise<void> {
  await fetch('/api/flows', { method: 'DELETE' })
}

/**
 * Connects to /ws/flows with auto-reconnect. onOpen fires on every (re)connect
 * so callers can refetch the list to cover missed events.
 */
export function connectFlowsWS(
  onEvent: (ev: FlowEvent) => void,
  onOpen?: () => void,
): () => void {
  let ws: WebSocket | undefined
  let timer: number | undefined
  let stopped = false

  const connect = () => {
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    ws = new WebSocket(`${proto}://${location.host}/ws/flows`)
    ws.onopen = () => onOpen?.()
    ws.onmessage = (m) => {
      try {
        onEvent(JSON.parse(m.data))
      } catch {
        /* ignore malformed frame */
      }
    }
    ws.onclose = () => {
      if (!stopped) timer = window.setTimeout(connect, 1000)
    }
  }
  connect()
  return () => {
    stopped = true
    window.clearTimeout(timer)
    ws?.close()
  }
}
