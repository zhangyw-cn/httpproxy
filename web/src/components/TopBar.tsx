import { useState } from 'react'

interface Props {
  onClear: () => void
  count: number
}

type Theme = 'dark' | 'light'

function readTheme(): Theme {
  return document.documentElement.getAttribute('data-theme') === 'light' ? 'light' : 'dark'
}

function applyTheme(theme: Theme) {
  document.documentElement.setAttribute('data-theme', theme)
  try {
    localStorage.setItem('httpproxy.theme', theme)
  } catch {
    /* attribute already updated; leave the stored value unchanged */
  }
}

export default function TopBar({ onClear, count }: Props) {
  const [theme, setTheme] = useState<Theme>(readTheme)
  const proxyHint = `${location.hostname || '127.0.0.1'}:8080`
  const toggleTheme = () => {
    const next: Theme = theme === 'dark' ? 'light' : 'dark'
    applyTheme(next)
    setTheme(next)
  }
  return (
    <header className="topbar">
      <span className="topbar-title">httpproxy</span>
      <span className="topbar-hint" title="将客户端的 HTTP(S) 代理指向此地址">
        代理 <code>{proxyHint}</code>
      </span>
      <span className="topbar-spacer" />
      <span className="topbar-count">{count} 条请求</span>
      <button className="btn" onClick={toggleTheme}>
        {theme === 'dark' ? '浅色' : '黑色'}
      </button>
      <a className="btn" href="/api/ca.crt" download="httpproxy-ca.crt">
        下载 CA
      </a>
      <button className="btn" onClick={onClear}>
        清空
      </button>
    </header>
  )
}
