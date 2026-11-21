import { useCallback, useEffect, useRef, useState } from 'react'
import { activateHost, getConfig, getOverview, listHosts } from '../api'
import { captureActionLabel, rulesMark } from '../capture'
import { formatAgo, formatUptime, hostsSummary } from '../overview'
import type { ConfigSnapshot, HostsList, Overview } from '../types'

interface Props {
  snapshot: ConfigSnapshot | null
  loadError: boolean
  captureBusy: boolean
  captureError: string
  onToggleCapture: () => void
  onOpenCapture: () => void
  onOpenHosts: () => void
  onConfigChanged: (snapshot: ConfigSnapshot) => void
}

/** One-line summary of the capture rules from rulesMark's lines. */
function rulesSummary(snapshot: ConfigSnapshot | null, failed: boolean): string {
  return rulesMark(snapshot?.file ?? null, failed)
    .lines.filter((line) => line !== '主机：')
    .join(' · ')
}

export default function HomePane({
  snapshot,
  loadError,
  captureBusy,
  captureError,
  onToggleCapture,
  onOpenCapture,
  onOpenHosts,
  onConfigChanged,
}: Props) {
  const [overview, setOverview] = useState<Overview | null>(null)
  const [pollFailed, setPollFailed] = useState(false)
  const [hosts, setHosts] = useState<HostsList | null>(null)
  const [hostsBusy, setHostsBusy] = useState(false)
  const [hostsError, setHostsError] = useState('')
  const [copied, setCopied] = useState(false)
  const copyTimer = useRef<number | undefined>(undefined)

  useEffect(() => {
    let cancelled = false
    const load = () => {
      getOverview()
        .then((next) => {
          if (cancelled) return
          setOverview(next)
          setPollFailed(false)
        })
        .catch(() => {
          if (!cancelled) setPollFailed(true)
        })
    }
    load()
    const timer = window.setInterval(load, 2000)
    return () => {
      cancelled = true
      window.clearInterval(timer)
    }
  }, [])

  useEffect(() => () => window.clearTimeout(copyTimer.current), [])

  const refreshHosts = useCallback(async () => {
    try {
      setHosts(await listHosts())
      setHostsError('')
    } catch (err) {
      setHostsError(err instanceof Error ? err.message : '无法读取 Hosts 配置')
    }
  }, [])

  useEffect(() => {
    void refreshHosts()
  }, [refreshHosts])

  const proxy = overview?.proxy ?? snapshot?.runtime.proxy ?? ''
  const capture = snapshot ? snapshot.file.capture : null
  const rules = rulesSummary(snapshot, loadError)
  const profiles = hosts?.profiles ?? []
  const activeHost = hosts?.active ?? overview?.hosts_profile ?? ''
  const activeProfile = profiles.find((profile) => profile.name === activeHost)
  const hostsCount = activeProfile ? activeProfile.rules : (overview?.hosts_rules ?? 0)
  const hostsWarn = overview?.hosts_error ?? ''

  const onCopy = () => {
    const clipboard: Clipboard | undefined = navigator.clipboard
    if (proxy === '' || !clipboard || typeof clipboard.writeText !== 'function') return
    clipboard.writeText(`http://${proxy}`).then(
      () => {
        setCopied(true)
        window.clearTimeout(copyTimer.current)
        copyTimer.current = window.setTimeout(() => setCopied(false), 1500)
      },
      () => setCopied(false),
    )
  }

  const onActivate = async (name: string) => {
    if (hostsBusy || name === activeHost) return
    setHostsBusy(true)
    setHostsError('')
    try {
      await activateHost(name)
      onConfigChanged(await getConfig())
      await refreshHosts()
    } catch (err) {
      // The select is bound to the active name, so it falls back on its own.
      setHostsError(err instanceof Error ? err.message : '无法启用 Hosts 配置')
    } finally {
      setHostsBusy(false)
    }
  }

  return (
    <div className="home">
      <div className="home-head">
        <h1>httpproxy</h1>
        {overview ? <span className="home-hint">已运行 {formatUptime(overview.uptime_seconds)}</span> : null}
      </div>

      {pollFailed ? <p className="notice">无法读取代理状态</p> : null}
      {captureError ? <p className="notice">{captureError}</p> : null}

      <div className="home-row">
        <span className="home-label">代理</span>
        <code className="home-address">{proxy === '' ? '—' : proxy}</code>
        <button className="btn" type="button" disabled={proxy === ''} onClick={onCopy}>
          {copied ? '已复制' : '复制'}
        </button>
        <span className="home-hint">把客户端 HTTP(S) 代理指向此地址</span>
      </div>

      <div className="home-row">
        <span className="home-label">抓包</span>
        <span className={capture ? 'home-dot on' : 'home-dot'} aria-hidden="true" />
        <span>{capture ? '已开启' : '已关闭'}</span>
        <span className="home-summary" title={rules}>
          {rules}
        </span>
        {overview ? <span className="home-hint">{overview.flow_count} 条记录</span> : null}
        <div className="home-actions">
          <button
            className="btn"
            type="button"
            disabled={captureBusy || snapshot == null}
            onClick={onToggleCapture}
          >
            {captureActionLabel(capture)}
          </button>
          <button className="btn" type="button" onClick={onOpenCapture}>
            查看抓包
          </button>
        </div>
      </div>

      <div className="home-row">
        <span className="home-label">Hosts</span>
        <span>{activeHost === '' ? '未配置' : `当前「${activeHost}」· ${hostsCount} 条`}</span>
        <select
          className="input"
          aria-label="启用 Hosts 配置"
          value={activeHost}
          disabled={hostsBusy}
          onChange={(event) => void onActivate(event.target.value)}
        >
          <option value="">不启用</option>
          {activeHost !== '' && !profiles.some((profile) => profile.name === activeHost) ? (
            <option value={activeHost}>{activeHost}</option>
          ) : null}
          {profiles.map((profile) => (
            <option key={profile.name} value={profile.name}>
              {profile.name}
            </option>
          ))}
        </select>
        <div className="home-actions">
          <button className="btn" type="button" onClick={onOpenHosts}>
            管理
          </button>
        </div>
      </div>
      {hostsError ? <p className="notice">{hostsError}</p> : null}
      {hostsWarn ? <p className="notice">当前启用的 Hosts 配置有误：{hostsWarn}</p> : null}

      <div className="home-row">
        <span className="home-label">CA</span>
        <a className="btn" href="/api/ca.crt" download="httpproxy-ca.crt">
          下载 CA
        </a>
        {capture === false ? <span className="home-hint">抓包关闭时不需要安装证书</span> : null}
      </div>

      <section className="home-section">
        <h2>正在使用代理的客户端{overview ? ` (${overview.clients.length})` : ''}</h2>
        <div className="clients-wrap">
          <table className="client-table">
            <thead>
              <tr>
                <th>连接</th>
                <th>请求</th>
                <th>HTTPS</th>
                <th>最近活动</th>
                <th>最近访问</th>
              </tr>
            </thead>
            <tbody>
              {overview == null ? (
                <tr className="empty">
                  <td colSpan={5}>{pollFailed ? '无法读取代理状态' : '正在读取代理状态'}</td>
                </tr>
              ) : overview.clients.length === 0 ? (
                <tr className="empty">
                  <td colSpan={5}>
                    还没有客户端连接。把系统或浏览器的代理设为 <code>{proxy === '' ? '—' : proxy}</code>。
                  </td>
                </tr>
              ) : (
                overview.clients.map((client) => {
                  const recent = hostsSummary(client.hosts)
                  return (
                    <tr key={client.ip}>
                      <td>{client.active_conns}</td>
                      <td>{client.requests}</td>
                      <td>{client.tunnels}</td>
                      <td title={client.last_seen}>{formatAgo(client.last_seen)}</td>
                      <td className="client-hosts" title={recent.title}>
                        {recent.text}
                      </td>
                    </tr>
                  )
                })
              )}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  )
}
