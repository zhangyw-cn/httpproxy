interface Props {
  onClear: () => void
  count: number
  proxy: string
  capture: boolean | null
  disabled: boolean
  error: string
  onToggle: () => void
}

export default function TopBar({ onClear, count, proxy, capture, disabled, error, onToggle }: Props) {
  return (
    <header className="topbar">
      <span className="topbar-title">httpproxy</span>
      <span className="topbar-hint" title="将客户端的 HTTP(S) 代理指向此地址">
        代理 <code>{proxy}</code>
      </span>
      <span className="topbar-spacer" />
      <span className="topbar-count">{count} 条请求</span>
      <button className="btn" type="button" disabled={disabled || capture == null} onClick={onToggle}>
        {capture ? '暂停抓包' : '开始抓包'}
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
