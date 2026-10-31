import { useEffect, useMemo, useRef, useState } from 'react'
import { statusCell } from '../columns'
import { displayBody, headersCopyText, queryParams } from '../detailView'
import type { Flow, Header } from '../types'
import { formatSize } from './FlowTable'

type Tab = 'headers' | 'request' | 'response'
type BodyMode = 'pretty' | 'raw'

const TAB_LABEL: Record<Tab, string> = {
  headers: '标头',
  request: '请求',
  response: '响应',
}

interface Props {
  flow: Flow | null
  loading: boolean
  dock: 'bottom' | 'right'
  onToggleDock: () => void
}

function DockIcon({ edge }: { edge: 'right' | 'bottom' }) {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" aria-hidden="true">
      <rect x="2" y="2" width="12" height="12" rx="1" />
      {edge === 'right' ? <path d="M10 2v12" strokeWidth="2.5" /> : <path d="M2 10h12" strokeWidth="2.5" />}
    </svg>
  )
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

function HeaderList({ title, headers }: { title: string; headers: Header[] | null }) {
  const list = headers ?? []
  return (
    <section className="hdr-section">
      <h4>
        {title} <span className="muted">({list.length})</span>
      </h4>
      <dl className="hdr-list">
        {list.map((header, index) => (
          <div key={index} className="hdr-row">
            <dt>{header.name}:</dt>
            <dd>{header.value}</dd>
          </div>
        ))}
      </dl>
    </section>
  )
}

function Body({ text, truncated, size }: { text: string; truncated: boolean; size: number }) {
  if (!text) return <div className="muted pad">{size > 0 ? '未捕获正文。' : '(无正文)'}</div>
  return (
    <>
      {truncated && (
        <div className="notice">
          正文已截断——仅保存有限预览；原始传输大小 {formatSize(size)}。
        </div>
      )}
      <pre className="body">{text}</pre>
    </>
  )
}

export default function FlowDetail({ flow, loading, dock, onToggleDock }: Props) {
  const [tab, setTab] = useState<Tab>('headers')
  const [mode, setMode] = useState<BodyMode>('pretty')
  const [copied, setCopied] = useState(false)
  const [copyError, setCopyError] = useState(false)
  const copyTimer = useRef<number | null>(null)
  const copyFeedbackToken = useRef(0)

  const clearCopyTimer = () => {
    if (copyTimer.current == null) return
    window.clearTimeout(copyTimer.current)
    copyTimer.current = null
  }

  useEffect(() => () => clearCopyTimer(), [])

  useEffect(() => {
    copyFeedbackToken.current += 1
    setCopied(false)
    setCopyError(false)
    clearCopyTimer()
  }, [tab])

  const params = useMemo(() => (flow ? queryParams(flow.path) : []), [flow])
  const reqDecoded = useMemo(() => (flow ? decodeBase64(flow.req_body) : ''), [flow])
  const respDecoded = useMemo(
    () => (flow ? decodeBase64(flow.resp_body_decoded || flow.resp_body) : ''),
    [flow],
  )
  const reqText = useMemo(
    () => displayBody(reqDecoded, flow?.req_headers ?? null, mode),
    [reqDecoded, flow, mode],
  )
  const respText = useMemo(
    () => displayBody(respDecoded, flow?.resp_headers ?? null, mode),
    [respDecoded, flow, mode],
  )
  const status = flow ? statusCell(flow) : null
  const bodyText = tab === 'request' ? reqText : respText
  const copyDisabled = !flow || (tab !== 'headers' && bodyText === '')

  const onCopy = async () => {
    if (!flow || !status || copyDisabled) return
    const text =
      tab === 'headers'
        ? headersCopyText({
            url: `${flow.scheme}://${flow.host}${flow.path}`,
            method: flow.method,
            status: status.text,
            version: flow.http_version,
            client: flow.client_addr,
            error: flow.error,
            query: params,
            respHeaders: flow.resp_headers,
            reqHeaders: flow.req_headers,
          })
        : bodyText
    copyFeedbackToken.current += 1
    const token = copyFeedbackToken.current
    const write = navigator.clipboard?.writeText
    if (!write) {
      if (token !== copyFeedbackToken.current) return
      setCopied(false)
      setCopyError(true)
      return
    }
    try {
      await write.call(navigator.clipboard, text)
      if (token !== copyFeedbackToken.current) return
      setCopyError(false)
      setCopied(true)
      clearCopyTimer()
      copyTimer.current = window.setTimeout(() => {
        setCopied(false)
        copyTimer.current = null
      }, 1500)
    } catch {
      if (token !== copyFeedbackToken.current) return
      clearCopyTimer()
      setCopied(false)
      setCopyError(true)
    }
  }

  const dockLabel = dock === 'bottom' ? '详情放到右侧' : '详情放到下方'

  return (
    <div className="detail">
      <div className="tabs">
        <div className="tabs-main">
          {(['headers', 'request', 'response'] as Tab[]).map((item) => (
            <button key={item} className={`tab${tab === item ? ' active' : ''}`} onClick={() => setTab(item)}>
              {TAB_LABEL[item]}
            </button>
          ))}
        </div>
        <div className="tabs-actions">
          {tab !== 'headers' ? (
            <>
              <button
                type="button"
                className={mode === 'pretty' ? 'btn seg-btn active' : 'btn seg-btn'}
                aria-pressed={mode === 'pretty'}
                onClick={() => setMode('pretty')}
              >
                美化
              </button>
              <button
                type="button"
                className={mode === 'raw' ? 'btn seg-btn active' : 'btn seg-btn'}
                aria-pressed={mode === 'raw'}
                onClick={() => setMode('raw')}
              >
                原始
              </button>
            </>
          ) : null}
          <button type="button" className="btn" disabled={copyDisabled} onClick={() => void onCopy()}>
            {copied ? '已复制' : '复制'}
          </button>
          {copyError ? <span className="copy-error">无法复制</span> : null}
          <button type="button" className="btn btn-icon" title={dockLabel} aria-label={dockLabel} onClick={onToggleDock}>
            <DockIcon edge={dock === 'bottom' ? 'right' : 'bottom'} />
          </button>
        </div>
      </div>
      <div className="detail-body">
        {!flow ? (
          <div className="muted pad">{loading ? '加载中…' : '选择一条请求以查看详情。'}</div>
        ) : tab === 'headers' ? (
          <>
            <section className="hdr-section">
              <h4>常规</h4>
              <dl className="hdr-list">
                <div className="hdr-row"><dt>请求 URL：</dt><dd>{`${flow.scheme}://${flow.host}${flow.path}`}</dd></div>
                <div className="hdr-row"><dt>请求方法：</dt><dd>{flow.method}</dd></div>
                <div className="hdr-row">
                  <dt>状态码：</dt>
                  <dd className={status?.statusClass}>{status?.text}</dd>
                </div>
                <div className="hdr-row"><dt>协议：</dt><dd>{flow.http_version}</dd></div>
                <div className="hdr-row"><dt>远程地址：</dt><dd>{flow.client_addr}</dd></div>
                {flow.error && (
                  <div className="hdr-row err"><dt>错误：</dt><dd>{flow.error}</dd></div>
                )}
              </dl>
            </section>
            <section className="hdr-section">
              <h4>查询参数</h4>
              {params.length === 0 ? (
                <div className="hdr-list muted">(无)</div>
              ) : (
                <dl className="hdr-list">
                  {params.map((item, index) => (
                    <div key={index} className="hdr-row">
                      <dt>{item.name}:</dt>
                      <dd>{item.value}</dd>
                    </div>
                  ))}
                </dl>
              )}
            </section>
            <HeaderList title="响应标头" headers={flow.resp_headers} />
            <HeaderList title="请求标头" headers={flow.req_headers} />
          </>
        ) : tab === 'request' ? (
          <Body text={reqText} truncated={flow.req_truncated} size={flow.req_size} />
        ) : (
          <Body text={respText} truncated={flow.resp_truncated} size={flow.resp_size} />
        )}
      </div>
    </div>
  )
}
