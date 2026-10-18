export interface Header {
  name: string
  value: string
}

export interface FlowSummary {
  id: string
  scheme: string
  method: string
  host: string
  path: string
  http_version: string
  status_code: number
  req_size: number
  resp_size: number
  req_truncated: boolean
  resp_truncated: boolean
  started_at: string
  finished_at?: string
  duration_ms: number
  error?: string
  client_addr: string
}

/** Full flow; req_body / resp_body are base64 (Go []byte). */
export interface Flow extends FlowSummary {
  req_headers: Header[] | null
  resp_headers: Header[] | null
  req_body?: string
  resp_body?: string
  /** Decompressed copy of resp_body when Content-Encoding was set. */
  resp_body_decoded?: string
}

export interface FlowEvent {
  type: 'flow.created' | 'flow.updated' | 'flows.cleared' | 'flow.evicted' | string
  flow?: FlowSummary
  flow_id?: string
}

export interface Filters {
  host: string
  path: string
  status: string
}

export interface AppConfig {
  max_flows: number
}
