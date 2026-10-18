import type { AppConfig, Filters, Flow, FlowEvent, FlowSummary } from './types'

export async function getConfig(): Promise<AppConfig> {
  const r = await fetch('/api/config')
  if (!r.ok) throw new Error(`config: ${r.status}`)
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
