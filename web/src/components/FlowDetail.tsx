import { useMemo, useState } from 'react'
import type { Flow, Header } from '../types'
import { formatSize } from './FlowTable'

type Tab = 'headers' | 'request' | 'response'

interface Props {
  flow: Flow | null
  loading: boolean
}

function decodeBase64(b64?: string): string {
  if (!b64) return ''
  try {
    const bin = atob(b64)
    const bytes = Uint8Array.from(bin, (c) => c.charCodeAt(0))
    return new TextDecoder('utf-8').decode(bytes)
  } catch {
    return ''
  }
}

export function prettyBody(text: string, headers: Header[] | null): string {
  if (!text) return ''
  const ct = headers?.find((h) => h.name.toLowerCase() === 'content-type')?.value.toLowerCase() ?? ''
  const trimmed = text.trim()
  if (ct.includes('json') || trimmed.startsWith('{') || trimmed.startsWith('[')) {
    try {
      return JSON.stringify(JSON.parse(trimmed), null, 2)
    } catch {
      /* not JSON, fall through */
    }
  }
  return text
}

function HeaderList({ title, headers }: { title: string; headers: Header[] | null }) {
  return (
    <section className="hdr-section">
      <h4>
        {title} <span className="muted">({headers?.length ?? 0})</span>
      </h4>
      <dl className="hdr-list">
        {(headers ?? []).map((h, i) => (
          <div key={i} className="hdr-row">
            <dt>{h.name}:</dt>
            <dd>{h.value}</dd>
          </div>
        ))}
      </dl>
    </section>
  )
}

function Body({ text, truncated, size }: { text: string; truncated: boolean; size: number }) {
  if (!text) return <div className="muted pad">{size > 0 ? 'Body not captured.' : '(no body)'}</div>
  return (
    <>
      {truncated && (
        <div className="notice">Body truncated — showing first {formatSize(text.length)} of {formatSize(size)}.</div>
      )}
      <pre className="body">{text}</pre>
    </>
  )
}

export default function FlowDetail({ flow, loading }: Props) {
  const [tab, setTab] = useState<Tab>('headers')

  const req = useMemo(
    () => (flow ? prettyBody(decodeBase64(flow.req_body), flow.req_headers) : ''),
    [flow],
  )
  const resp = useMemo(
    () =>
      flow ? prettyBody(decodeBase64(flow.resp_body_decoded || flow.resp_body), flow.resp_headers) : '',
    [flow],
  )

  return (
    <div className="detail">
      <div className="tabs">
        {(['headers', 'request', 'response'] as Tab[]).map((t) => (
          <button key={t} className={`tab${tab === t ? ' active' : ''}`} onClick={() => setTab(t)}>
            {t[0].toUpperCase() + t.slice(1)}
          </button>
        ))}
      </div>
      <div className="detail-body">
        {!flow ? (
          <div className="muted pad">{loading ? 'Loading…' : 'Select a request to view details.'}</div>
        ) : tab === 'headers' ? (
          <>
            <section className="hdr-section">
              <h4>General</h4>
              <dl className="hdr-list">
                <div className="hdr-row"><dt>Request URL:</dt><dd>{`${flow.scheme}://${flow.host}${flow.path}`}</dd></div>
                <div className="hdr-row"><dt>Request Method:</dt><dd>{flow.method}</dd></div>
                <div className="hdr-row">
                  <dt>Status Code:</dt>
                  <dd>{flow.status_code || (flow.error ? '(failed)' : '(pending)')}</dd>
                </div>
                <div className="hdr-row"><dt>Protocol:</dt><dd>{flow.http_version}</dd></div>
                <div className="hdr-row"><dt>Remote Address:</dt><dd>{flow.client_addr}</dd></div>
                {flow.error && (
                  <div className="hdr-row err"><dt>Error:</dt><dd>{flow.error}</dd></div>
                )}
              </dl>
            </section>
            <HeaderList title="Response Headers" headers={flow.resp_headers} />
            <HeaderList title="Request Headers" headers={flow.req_headers} />
          </>
        ) : tab === 'request' ? (
          <Body text={req} truncated={flow.req_truncated} size={flow.req_size} />
        ) : (
          <Body text={resp} truncated={flow.resp_truncated} size={flow.resp_size} />
        )}
      </div>
    </div>
  )
}
