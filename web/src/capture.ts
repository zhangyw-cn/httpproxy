import type { ConfigValues } from './types.ts'

/** Exactly the three rule keys PUT /api/capture/rules accepts. */
export interface CaptureRules {
  capture_empty: 'all' | 'none'
  capture_https: 'decrypt' | 'tunnel'
  capture_hosts: string[]
}

/** Trims each line, drops blank lines, and keeps the input order. */
function splitHosts(hostsText: string): string[] {
  return hostsText
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line !== '')
}

export function rulesMark(
  file: ConfigValues | null,
  failed = false,
): { active: boolean; lines: string[] } {
  if (failed) return { active: false, lines: ['无法读取配置'] }
  if (file == null) return { active: false, lines: ['正在读取配置'] }

  const lines = [
    file.capture_empty === 'all' ? '空规则：抓全部' : '空规则：不抓',
    file.capture_https === 'decrypt' ? '未抓 HTTPS：解密' : '未抓 HTTPS：隧道',
  ]
  if (file.capture_hosts.length === 0) {
    lines.push('主机：无')
  } else {
    lines.push('主机：', ...file.capture_hosts)
  }
  const active =
    file.capture_hosts.length > 0 || file.capture_empty === 'none' || file.capture_https === 'tunnel'
  return { active, lines }
}

export function captureActionLabel(capture: boolean | null): '开始抓包' | '暂停抓包' {
  return capture ? '暂停抓包' : '开始抓包'
}

/** Builds the body for the capture-rules-only endpoint. */
export function captureRulesPayload(
  empty: 'all' | 'none',
  httpsMode: 'decrypt' | 'tunnel',
  hostsText: string,
): CaptureRules {
  return {
    capture_empty: empty,
    capture_https: httpsMode,
    capture_hosts: splitHosts(hostsText),
  }
}
