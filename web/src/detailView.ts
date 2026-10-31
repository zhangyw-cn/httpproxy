export interface NameValue {
  name: string
  value: string
}

export interface HeadersCopyInput {
  url: string
  method: string
  status: string
  version: string
  client: string
  error?: string
  query: NameValue[]
  respHeaders: NameValue[] | null
  reqHeaders: NameValue[] | null
}

function decodePart(raw: string): string {
  const plus = raw.replaceAll('+', ' ')
  try {
    return decodeURIComponent(plus)
  } catch {
    return plus
  }
}

export function queryParams(path: string): NameValue[] {
  const mark = path.indexOf('?')
  if (mark < 0) return []
  const search = path.slice(mark + 1)
  if (search === '') return []
  const out: NameValue[] = []
  for (const segment of search.split('&')) {
    if (segment === '') continue
    const eq = segment.indexOf('=')
    if (eq < 0) {
      out.push({ name: decodePart(segment), value: '' })
      continue
    }
    out.push({
      name: decodePart(segment.slice(0, eq)),
      value: decodePart(segment.slice(eq + 1)),
    })
  }
  return out
}

function valueLine(name: string, value: string): string {
  return value === '' ? `${name}:` : `${name}: ${value}`
}

function headerBlock(title: string, headers: NameValue[] | null): string {
  const list = headers ?? []
  return [`${title} (${list.length})`, ...list.map((header) => valueLine(header.name, header.value))].join('\n')
}

export function headersCopyText(input: HeadersCopyInput): string {
  const general = [
    '常规',
    `请求 URL：${input.url}`,
    `请求方法：${input.method}`,
    `状态码：${input.status}`,
    `协议：${input.version}`,
    `远程地址：${input.client}`,
  ]
  if (input.error) general.push(`错误：${input.error}`)
  const queryLines = input.query.length === 0 ? ['(无)'] : input.query.map((item) => valueLine(item.name, item.value))
  return [
    general.join('\n'),
    ['查询参数', ...queryLines].join('\n'),
    headerBlock('响应标头', input.respHeaders),
    headerBlock('请求标头', input.reqHeaders),
  ].join('\n\n')
}

export function prettyBody(text: string, headers: NameValue[] | null): string {
  if (!text) return ''
  const ct = headers?.find((header) => header.name.toLowerCase() === 'content-type')?.value.toLowerCase() ?? ''
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

export function displayBody(text: string, headers: NameValue[] | null, mode: 'pretty' | 'raw'): string {
  if (mode === 'raw') return text
  return prettyBody(text, headers)
}

export const DOCK_STORAGE_KEY = 'httpproxy.detailDock'

export function normalizeDock(raw: unknown): 'bottom' | 'right' {
  return raw === 'bottom' || raw === 'right' ? raw : 'bottom'
}

export function loadDock(storage?: Pick<Storage, 'getItem'>): 'bottom' | 'right' {
  try {
    const store = storage ?? localStorage
    const text = store.getItem(DOCK_STORAGE_KEY)
    if (text == null) return 'bottom'
    return normalizeDock(JSON.parse(text))
  } catch {
    return 'bottom'
  }
}

export function saveDock(dock: 'bottom' | 'right', storage?: Pick<Storage, 'setItem'>): void {
  try {
    const store = storage ?? localStorage
    store.setItem(DOCK_STORAGE_KEY, JSON.stringify(dock))
  } catch {
    /* in-memory dock stays */
  }
}
