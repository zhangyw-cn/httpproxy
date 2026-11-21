export type View = 'home' | 'capture' | 'hosts' | 'settings'

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
  /** Dial address after a hosts override; absent otherwise. */
  upstream_addr?: string
  /** Hosts profile that rewrote the dial address; absent otherwise. */
  hosts_profile?: string
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

export interface ConfigValues {
  proxy: string
  ui: string
  max_flows: number
  max_body: number
  theme: 'dark' | 'light'
  capture: boolean
  capture_empty: 'all' | 'none'
  capture_https: 'decrypt' | 'tunnel'
  capture_hosts: string[]
  hosts_profile: string
}

export interface ConfigSnapshot {
  file: ConfigValues
  runtime: ConfigValues
}

export interface ClientInfo {
  ip: string
  active_conns: number
  requests: number
  tunnels: number
  last_seen: string
  hosts: string[]
}

export interface Overview {
  proxy: string
  ui: string
  started_at: string
  uptime_seconds: number
  capture: boolean
  flow_count: number
  hosts_profile: string
  hosts_rules: number
  hosts_error: string
  clients: ClientInfo[]
}

export interface HostWarning {
  domain: string
  ignored_line: number
  first_line: number
}

export interface HostProfileInfo {
  name: string
  rules: number
  warnings: HostWarning[]
  error: string
}

export interface HostsList {
  active: string
  active_error: string
  profiles: HostProfileInfo[]
}

export interface HostDetail {
  name: string
  content: string
}

export interface HostSaveResult {
  name: string
  rules: number
  warnings: HostWarning[]
}
