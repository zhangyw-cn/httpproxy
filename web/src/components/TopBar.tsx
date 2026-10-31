import { captureActionLabel } from '../capture'

interface Props {
  onClear: () => void
  count: number
  proxy: string
  capture: boolean | null
  disabled: boolean
  error: string
  onToggle: () => void
}

function PauseIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true">
      <rect x="3" y="2" width="3.2" height="12" fill="currentColor" />
      <rect x="9.8" y="2" width="3.2" height="12" fill="currentColor" />
    </svg>
  )
}

function PlayIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true">
      <path d="M4 2.2v11.6L13.2 8 4 2.2z" fill="currentColor" />
    </svg>
  )
}

export default function TopBar({ onClear, count, proxy, capture, disabled, error, onToggle }: Props) {
  const label = captureActionLabel(capture)
  return (
    <header className="topbar">
      <span className="topbar-title">httpproxy</span>
      <span className="topbar-hint" title="将客户端的 HTTP(S) 代理指向此地址">
        代理 <code>{proxy}</code>
      </span>
      <span className="topbar-spacer" />
      <span className="topbar-count">{count} 条请求</span>
      <button
        className="btn btn-icon"
        type="button"
        disabled={disabled || capture == null}
        aria-label={label}
        title={label}
        onClick={onToggle}
      >
        {capture ? <PauseIcon /> : <PlayIcon />}
      </button>
      {error ? <span className="topbar-error">{error}</span> : null}
      <a className="btn" href="/api/ca.crt" download="httpproxy-ca.crt">
        下载 CA
      </a>
      <button className="btn" onClick={onClear}>
        清空
      </button>
    </header>
  )
}
