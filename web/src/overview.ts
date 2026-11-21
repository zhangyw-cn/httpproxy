import type { HostWarning } from './types.ts'

/** Human-readable process uptime: 秒, 分钟, 小时, 天. */
export function formatUptime(seconds: number): string {
  const total = Math.max(0, Math.floor(seconds))
  if (total === 0) return '刚刚'
  if (total < 60) return `${total} 秒`
  if (total < 3600) return `${Math.floor(total / 60)} 分钟`
  if (total < 86400) {
    const hours = Math.floor(total / 3600)
    const minutes = Math.floor((total % 3600) / 60)
    return minutes === 0 ? `${hours} 小时` : `${hours} 小时 ${minutes} 分钟`
  }
  const days = Math.floor(total / 86400)
  const hours = Math.floor((total % 86400) / 3600)
  return hours === 0 ? `${days} 天` : `${days} 天 ${hours} 小时`
}

/** Relative time from an RFC3339 timestamp, measured against now (ms). */
export function formatAgo(iso: string, now = Date.now()): string {
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return '—'
  const seconds = Math.max(0, Math.floor((now - t) / 1000))
  if (seconds === 0) return '刚刚'
  if (seconds < 60) return `${seconds} 秒前`
  if (seconds < 3600) return `${Math.floor(seconds / 60)} 分钟前`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)} 小时前`
  return `${Math.floor(seconds / 86400)} 天前`
}

/** Short host list for a card: at most max names plus the total count. */
export function hostsSummary(hosts: string[], max = 3): { text: string; title: string } {
  if (hosts.length === 0) return { text: '—', title: '' }
  const title = hosts.join('、')
  if (hosts.length <= max) return { text: title, title }
  return { text: `${hosts.slice(0, max).join('、')} 等 ${hosts.length} 个`, title }
}

/** Duplicate-mapping warning in the same wording the backend uses. */
export function warningText(w: HostWarning): string {
  return `第 ${w.ignored_line} 行与第 ${w.first_line} 行重复，已按第 ${w.first_line} 行生效`
}
