interface Props {
  onClear: () => void
  count: number
}

export default function TopBar({ onClear, count }: Props) {
  const proxyHint = `${location.hostname || '127.0.0.1'}:8080`
  return (
    <header className="topbar">
      <span className="topbar-title">httpproxy</span>
      <span className="topbar-hint" title="Configure your client to use this HTTP(S) proxy">
        Proxy <code>{proxyHint}</code>
      </span>
      <span className="topbar-spacer" />
      <span className="topbar-count">{count} requests</span>
      <a className="btn" href="/api/ca.crt" download="httpproxy-ca.crt">
        Download CA
      </a>
      <button className="btn" onClick={onClear}>
        Clear
      </button>
    </header>
  )
}
