import type { ConfigValues } from './types.ts'

export function captureSummary(file: ConfigValues | null, failed = false): { text: string; title: string } {
  if (failed) {
    return { text: '无法读取配置', title: '' }
  }
  if (file == null) {
    return { text: '正在读取配置', title: '' }
  }

  const parts: string[] = []
  if (!file.capture) parts.push('抓包已暂停')
  parts.push(file.capture_empty === 'all' ? '空规则：抓全部' : '空规则：不抓')
  parts.push(file.capture_https === 'decrypt' ? '未抓 HTTPS：解密' : '未抓 HTTPS：隧道')

  const hosts = file.capture_hosts
  if (hosts.length === 0) {
    parts.push('主机：无')
    return { text: parts.join(' · '), title: '无' }
  }

  const listed = hosts.length > 3 ? `${hosts.slice(0, 3).join('、')} 等 ${hosts.length} 条` : hosts.join('、')
  parts.push(`主机：${listed}`)
  return { text: parts.join(' · '), title: hosts.join('\n') }
}

export function capturePayload(
  file: ConfigValues,
  empty: 'all' | 'none',
  httpsMode: 'decrypt' | 'tunnel',
  hostsText: string,
): ConfigValues {
  return {
    ...file,
    capture_empty: empty,
    capture_https: httpsMode,
    capture_hosts: hostsText
      .split('\n')
      .map((line) => line.trim())
      .filter((line) => line !== ''),
  }
}
