# Capture Page UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the capture text button and the summary line with icons, and let the detail pane sit below or to the right of the list with clearer header, request, and response content.

**Architecture:** `rulesMark` and `captureActionLabel` stay pure in `web/src/capture.ts`. `web/src/detailView.ts` owns dock storage, query parsing, header copy text, and pretty versus raw body text. `RulesMark` and `TopBar` only render. `App` owns the dock value because the class sits on the workspace. `FlowDetail` owns the shared pretty/raw mode and the copy feedback.

**Tech Stack:** React 19, TypeScript, Vite, one hand-written `styles.css`. No new runtime dependency. Pure logic is tested with Node's built-in `node:test` and type stripping. `web/tsconfig.json` includes only `src`, so `npm run build` typechecks `src` and does not typecheck `web/*.test.ts`.

**Spec:** `docs/superpowers/specs/2026-11-01-capture-ui-design.md`

## Global Constraints

- Web capture page only. Do not change Go, REST, WebSocket, `config.yaml` fields, `PUT /api/capture`, `PUT /api/config`, or README.
- Do not add an activity-bar icon, and do not move rule editing out of the settings page.
- Do not dock the detail pane to the left, do not open a separate window, and do not add a draggable splitter. A narrow window does not snap the pane back to the bottom.
- Do not add a JSON tree, image preview, cookies, timing, or "copy as cURL".
- Do not write the pretty/raw choice to `localStorage`.
- Do not add a browser test framework or a runtime dependency. Do not change Go tests.
- Do not edit `internal/ui/dist`.
- Capture button copy is exactly `暂停抓包` when `capture === true`, and `开始抓包` when `capture` is `false` or `null`. Both `aria-label` and `title` use that string. The button is disabled when `capture == null` or the existing write lock is set.
- Rules icon `aria-label` is exactly `抓包规则`. Popover lines are exactly `正在读取配置`, `无法读取配置`, `空规则：抓全部`, `空规则：不抓`, `未抓 HTTPS：解密`, `未抓 HTTPS：隧道`, `主机：无`, or `主机：` followed by one stored host per line. Do not insert `抓包已暂停` or `等 N 条`.
- `active` is true only when `capture_hosts` is non-empty, `capture_empty` is `none`, or `capture_https` is `tunnel`. `capture: false` does not make it active. `failed === true` ignores `file` and is inactive.
- Dock storage key is `httpproxy.detailDock`. Stored values are JSON strings `bottom` and `right`. Anything else, a missing key, or broken JSON loads as `bottom`.
- Dock button copy is exactly `详情放到右侧` when the current dock is `bottom`, and `详情放到下方` when it is `right`.
- List and detail panes use classes `pane pane-list` and `pane pane-detail`. Both flex bases are `55%` and `45%`. Both keep `min-width: 0` and `min-height: 0`.
- Query parsing: text after the first `?`, split on `&`, drop empty segments, split on the first `=`, replace `+` with a space, then `decodeURIComponent`. A thrown side keeps the post-plus text. No `?` or an empty query returns `[]`. The UI shows `(无)`.
- Header copy sections are separated by one blank line, with no trailing blank line. General labels use the fullwidth colon `：`. Query and header rows use `name: value`, or `name:` when the value is empty. Null header lists count as `(0)`.
- Copy success shows `已复制` for 1500 ms. Failure, or a missing `navigator.clipboard.writeText`, shows `无法复制` until the next success on that tab or until the tab changes. Body copy is disabled when `displayBody` returns `''`. Header copy stays enabled whenever a flow is selected.
- Pretty and raw are one shared `FlowDetail` state, default `pretty`. `pretty` calls `prettyBody`. `raw` returns the decoded text unchanged. The truncation notice stays outside the copied text.
- Add `.st-ok { color: var(--ok); }`. Do not change `.flow-table tbody tr.selected td { color: #ffffff; }`.
- `capturePayload` stays as it is.

## File Structure

| Path | Responsibility |
| --- | --- |
| `web/src/capture.ts` | `rulesMark`, `captureActionLabel`, existing `capturePayload` |
| `web/capture.test.ts` | `node:test` for those functions |
| `web/src/components/RulesMark.tsx` | Rules icon and hover/focus popover |
| `web/src/components/TopBar.tsx` | Play/pause icon button |
| `web/src/detailView.ts` | Dock storage, query params, header copy text, `prettyBody`, `displayBody` |
| `web/detailView.test.ts` | `node:test` for that module |
| `web/package.json` | Adds `detailView.test.ts` to the `test` script |
| `web/src/columns.ts` | Export existing `statusCell` |
| `web/src/components/FlowDetail.tsx` | Query section, status color, copy, pretty/raw, dock button |
| `web/src/App.tsx` | Remove the summary line, render `RulesMark`, own the dock |
| `web/src/styles.css` | Rules popover, icon button, dock layout, `.st-ok`; delete `.capture-summary`, `.pane-top`, `.pane-bottom` |

---

### Task 1: Rules mark

**Files:**
- Modify: `web/src/capture.ts`
- Modify: `web/capture.test.ts`
- Create: `web/src/components/RulesMark.tsx`
- Modify: `web/src/App.tsx`
- Modify: `web/src/styles.css`
- Test: `web/capture.test.ts`

**Interfaces:**
- Consumes: `ConfigValues` from `web/src/types.ts`. `App` already has `snapshot` and `loadError`.
- Produces:
  - `export function rulesMark(file: ConfigValues | null, failed?: boolean): { active: boolean; lines: string[] }`
  - `RulesMark` props `{ file: ConfigValues | null; failed: boolean }`

- [ ] **Step 1: Write the failing test**

Replace the `captureSummary` import and describe block in `web/capture.test.ts`. Keep the `file` fixture and the `capturePayload` describe block unchanged.

```ts
import { capturePayload, rulesMark } from './src/capture.ts'
```

```ts
describe('rulesMark', () => {
  it('says the config is loading', () => {
    assert.deepEqual(rulesMark(null), { active: false, lines: ['正在读取配置'] })
  })

  it('says the config failed to load and ignores the file', () => {
    assert.deepEqual(rulesMark(file, true), { active: false, lines: ['无法读取配置'] })
  })

  it('lists every host and stays active while capture is paused', () => {
    const got = rulesMark(file)
    assert.equal(got.active, true)
    assert.deepEqual(got.lines, [
      '空规则：抓全部',
      '未抓 HTTPS：解密',
      '主机：',
      'example.com',
      '*.example.com',
      'a.b.example.com',
      'example.com:8443',
    ])
    assert.equal(got.lines.some((line) => line.includes('抓包已暂停')), false)
    assert.equal(got.lines.some((line) => line.includes('等 4 条')), false)
  })

  it('is active for a non-default empty rule and tunnel with no hosts', () => {
    const got = rulesMark({
      ...file,
      capture: true,
      capture_empty: 'none',
      capture_https: 'tunnel',
      capture_hosts: [],
    })
    assert.equal(got.active, true)
    assert.deepEqual(got.lines, ['空规则：不抓', '未抓 HTTPS：隧道', '主机：无'])
  })

  it('stays inactive for defaults even when capture is paused', () => {
    const got = rulesMark({ ...file, capture: false, capture_hosts: [] })
    assert.deepEqual(got, {
      active: false,
      lines: ['空规则：抓全部', '未抓 HTTPS：解密', '主机：无'],
    })
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && node --experimental-strip-types --test capture.test.ts`

Expected: FAIL. The module does not provide an export named `rulesMark`.

- [ ] **Step 3: Implement `rulesMark` and delete `captureSummary`**

Replace `captureSummary` in `web/src/capture.ts` with:

```ts
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
```

Leave `capturePayload` unchanged.

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd web && node --experimental-strip-types --test capture.test.ts`

Expected: PASS

- [ ] **Step 5: Render the icon and remove the summary line**

Create `web/src/components/RulesMark.tsx`:

```tsx
import { rulesMark } from '../capture'
import type { ConfigValues } from '../types'

interface Props {
  file: ConfigValues | null
  failed: boolean
}

export default function RulesMark({ file, failed }: Props) {
  const mark = rulesMark(file, failed)
  return (
    <div className={mark.active ? 'rules active' : 'rules'}>
      <button type="button" className="rules-btn" aria-label="抓包规则">
        <svg
          width="16"
          height="16"
          viewBox="0 0 16 16"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.5"
          strokeLinecap="round"
          aria-hidden="true"
        >
          <path d="M2 4h12" />
          <path d="M2 8h12" />
          <path d="M2 12h12" />
          <circle cx="6" cy="4" r="1.6" fill="var(--bg)" />
          <circle cx="11" cy="8" r="1.6" fill="var(--bg)" />
          <circle cx="5" cy="12" r="1.6" fill="var(--bg)" />
        </svg>
      </button>
      <div className="rules-pop">
        {mark.lines.map((line, index) => (
          <div key={index}>{line}</div>
        ))}
      </div>
    </div>
  )
}
```

In `web/src/App.tsx`, replace the `captureSummary` import with:

```tsx
import RulesMark from './components/RulesMark'
```

Delete `const summary = captureSummary(snapshot?.file ?? null, loadError)`.

Delete this element:

```tsx
<p className="capture-summary" title={summary.title}>
  {summary.text}
</p>
```

Inside `.filterbar`, put `RulesMark` after `.filterbar-main` and before `ColumnPicker`:

```tsx
<RulesMark file={snapshot?.file ?? null} failed={loadError} />
```

In `web/src/styles.css`, delete the `.capture-summary` rule. Add:

```css
.rules { position: relative; flex: none; }
.rules-btn {
  height: 24px; width: 24px; padding: 0;
  display: flex; align-items: center; justify-content: center;
  border: 0; background: transparent; color: var(--text-muted); cursor: pointer;
}
.rules-btn:focus-visible { outline: 2px solid var(--accent); outline-offset: -1px; }
.rules.active .rules-btn { color: var(--accent); }
.rules-pop {
  display: none;
  position: absolute; top: 100%; right: 0; z-index: 3;
  max-width: 360px; max-height: 240px; overflow: auto;
  padding: 6px 8px;
  background: var(--bg); color: var(--text);
  border: 1px solid var(--border); border-radius: 6px;
  font-family: var(--mono); font-size: 11px; line-height: 16px;
}
.rules-pop div { white-space: nowrap; }
.rules:hover .rules-pop,
.rules:focus-within .rules-pop { display: block; }
```

The popover is `position: absolute` with `top: 100%`, so there is no gap under the button. It is a child of `.rules`, so moving the pointer onto it keeps `:hover`.

- [ ] **Step 6: Typecheck**

Run: `cd web && npx tsc -b --pretty false && node --experimental-strip-types --test capture.test.ts`

Expected: PASS. `tsc` prints nothing.

- [ ] **Step 7: Commit**

```bash
git add web/src/capture.ts web/capture.test.ts web/src/components/RulesMark.tsx web/src/App.tsx web/src/styles.css
git commit -m "$(cat <<'EOF'
feat: show capture rules from the capture page

EOF
)"
```

---

### Task 2: Capture icon button

**Files:**
- Modify: `web/src/capture.ts`
- Modify: `web/capture.test.ts`
- Modify: `web/src/components/TopBar.tsx`
- Modify: `web/src/styles.css`
- Test: `web/capture.test.ts`

**Interfaces:**
- Consumes: existing `TopBar` props. `onToggle` still calls `PUT /api/capture`.
- Produces: `export function captureActionLabel(capture: boolean | null): '开始抓包' | '暂停抓包'`

- [ ] **Step 1: Write the failing test**

In `web/capture.test.ts`, change the existing import to:

```ts
import { captureActionLabel, capturePayload, rulesMark } from './src/capture.ts'
```

Then append this describe block after the `rulesMark` tests:

```ts
describe('captureActionLabel', () => {
  it('names the action the button will take', () => {
    assert.equal(captureActionLabel(true), '暂停抓包')
    assert.equal(captureActionLabel(false), '开始抓包')
    assert.equal(captureActionLabel(null), '开始抓包')
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && node --experimental-strip-types --test capture.test.ts`

Expected: FAIL. The module does not provide an export named `captureActionLabel`.

- [ ] **Step 3: Implement the label and the icon button**

Add to `web/src/capture.ts`:

```ts
export function captureActionLabel(capture: boolean | null): '开始抓包' | '暂停抓包' {
  return capture ? '暂停抓包' : '开始抓包'
}
```

Replace `web/src/components/TopBar.tsx` with:

```tsx
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
```

Add to `web/src/styles.css`:

```css
.btn-icon { width: 24px; padding: 0; justify-content: center; }
```

Do not change `onToggle`, `putCapture`, or `.topbar-error`.

- [ ] **Step 4: Run the test and typecheck**

Run: `cd web && node --experimental-strip-types --test capture.test.ts && npx tsc -b --pretty false`

Expected: PASS. `tsc` prints nothing.

- [ ] **Step 5: Commit**

```bash
git add web/src/capture.ts web/capture.test.ts web/src/components/TopBar.tsx web/src/styles.css
git commit -m "$(cat <<'EOF'
feat: use an icon for the capture toggle

EOF
)"
```

---

### Task 3: Detail view helpers

**Files:**
- Create: `web/src/detailView.ts`
- Create: `web/detailView.test.ts`
- Modify: `web/package.json`
- Test: `web/detailView.test.ts`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:

```ts
export interface NameValue { name: string; value: string }

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

export function queryParams(path: string): NameValue[]
export function headersCopyText(input: HeadersCopyInput): string
export function prettyBody(text: string, headers: NameValue[] | null): string
export function displayBody(text: string, headers: NameValue[] | null, mode: 'pretty' | 'raw'): string
export function normalizeDock(raw: unknown): 'bottom' | 'right'
export function loadDock(storage?: Pick<Storage, 'getItem'>): 'bottom' | 'right'
export function saveDock(dock: 'bottom' | 'right', storage?: Pick<Storage, 'setItem'>): void
export const DOCK_STORAGE_KEY = 'httpproxy.detailDock'
```

- [ ] **Step 1: Write the failing test**

Create `web/detailView.test.ts`:

```ts
import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import {
  DOCK_STORAGE_KEY,
  displayBody,
  headersCopyText,
  loadDock,
  normalizeDock,
  queryParams,
  saveDock,
} from './src/detailView.ts'

describe('queryParams', () => {
  it('parses the spec example', () => {
    assert.deepEqual(queryParams('/search?q=a+b&x=%20&novalue&a=1&'), [
      { name: 'q', value: 'a b' },
      { name: 'x', value: ' ' },
      { name: 'novalue', value: '' },
      { name: 'a', value: '1' },
    ])
  })

  it('returns nothing when there is no query', () => {
    assert.deepEqual(queryParams('/search'), [])
    assert.deepEqual(queryParams('/search?'), [])
  })

  it('keeps a side that fails to decode', () => {
    assert.deepEqual(queryParams('/?bad=%ZZ&ok=1'), [
      { name: 'bad', value: '%ZZ' },
      { name: 'ok', value: '1' },
    ])
  })
})

describe('headersCopyText', () => {
  it('matches the spec example', () => {
    assert.equal(
      headersCopyText({
        url: 'https://example.com/a?q=1',
        method: 'GET',
        status: '200',
        version: 'HTTP/1.1',
        client: '127.0.0.1:54321',
        query: [{ name: 'q', value: '1' }],
        respHeaders: [{ name: 'Content-Type', value: 'text/plain' }],
        reqHeaders: [{ name: 'Host', value: 'example.com' }],
      }),
      [
        '常规',
        '请求 URL：https://example.com/a?q=1',
        '请求方法：GET',
        '状态码：200',
        '协议：HTTP/1.1',
        '远程地址：127.0.0.1:54321',
        '',
        '查询参数',
        'q: 1',
        '',
        '响应标头 (1)',
        'Content-Type: text/plain',
        '',
        '请求标头 (1)',
        'Host: example.com',
      ].join('\n'),
    )
  })

  it('adds an error, an empty query, and zero counts for null headers', () => {
    assert.equal(
      headersCopyText({
        url: 'https://example.com/a',
        method: 'GET',
        status: '(失败)',
        version: 'HTTP/1.1',
        client: '127.0.0.1:1',
        error: 'boom',
        query: [],
        respHeaders: null,
        reqHeaders: null,
      }),
      [
        '常规',
        '请求 URL：https://example.com/a',
        '请求方法：GET',
        '状态码：(失败)',
        '协议：HTTP/1.1',
        '远程地址：127.0.0.1:1',
        '错误：boom',
        '',
        '查询参数',
        '(无)',
        '',
        '响应标头 (0)',
        '',
        '请求标头 (0)',
      ].join('\n'),
    )
  })

  it('writes an empty value with nothing after the colon', () => {
    const text = headersCopyText({
      url: 'https://example.com/',
      method: 'GET',
      status: '204',
      version: 'HTTP/1.1',
      client: '127.0.0.1:1',
      query: [{ name: 'novalue', value: '' }],
      respHeaders: [],
      reqHeaders: [],
    })
    assert.equal(text.includes('novalue:'), true)
    assert.equal(text.includes('novalue: '), false)
  })
})

describe('displayBody', () => {
  it('indents JSON when pretty and keeps the original when raw', () => {
    const headers = [{ name: 'Content-Type', value: 'text/plain' }]
    assert.equal(displayBody('{"a":1}', headers, 'pretty'), '{\n  "a": 1\n}')
    assert.equal(displayBody('{"a":1}', headers, 'raw'), '{"a":1}')
  })
})

describe('dock storage', () => {
  it('accepts only bottom and right', () => {
    assert.equal(normalizeDock('bottom'), 'bottom')
    assert.equal(normalizeDock('right'), 'right')
    assert.equal(normalizeDock('left'), 'bottom')
    assert.equal(normalizeDock(null), 'bottom')
    assert.equal(normalizeDock(1), 'bottom')
  })

  it('round-trips through injected storage and rejects broken JSON', () => {
    const data = new Map<string, string>()
    const store = {
      getItem(key: string) {
        return data.has(key) ? data.get(key)! : null
      },
      setItem(key: string, value: string) {
        data.set(key, value)
      },
    }
    assert.equal(loadDock(store), 'bottom')
    saveDock('right', store)
    assert.equal(data.get(DOCK_STORAGE_KEY), '"right"')
    assert.equal(loadDock(store), 'right')
    data.set(DOCK_STORAGE_KEY, '{')
    assert.equal(loadDock(store), 'bottom')
  })
})
```

In `web/package.json`, change the test script to:

```json
"test": "node --experimental-strip-types --test columns.test.ts settings.test.ts capture.test.ts detailView.test.ts"
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && node --experimental-strip-types --test detailView.test.ts`

Expected: FAIL with `ERR_MODULE_NOT_FOUND` for `./src/detailView.ts`.

- [ ] **Step 3: Implement `detailView.ts`**

Create `web/src/detailView.ts`:

```ts
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
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npm test && npx tsc -b --pretty false`

Expected: PASS. `tsc` prints nothing.

- [ ] **Step 5: Commit**

```bash
git add web/src/detailView.ts web/detailView.test.ts web/package.json
git commit -m "$(cat <<'EOF'
feat: add detail view text helpers

EOF
)"
```

---

### Task 4: Detail content

**Files:**
- Modify: `web/src/columns.ts`
- Modify: `web/src/components/FlowDetail.tsx`
- Modify: `web/src/styles.css`
- Test: `web/columns.test.ts` (existing status classification must stay green)

**Interfaces:**
- Consumes: `queryParams`, `headersCopyText`, `displayBody`, and `NameValue` from `web/src/detailView.ts`. `formatSize` stays imported from `./FlowTable`.
- Produces: `export function statusCell(flow: CellFlow): FlowCell` from `web/src/columns.ts`. `FlowDetail` props stay `{ flow: Flow | null; loading: boolean }`.

- [ ] **Step 1: Export `statusCell` and color `st-ok`**

In `web/src/columns.ts`, change `function statusCell` to `export function statusCell`. Do not change its body.

In `web/src/styles.css`, next to the other status classes, add:

```css
.st-ok { color: var(--ok); }
```

Do not edit the selected-row color rule.

- [ ] **Step 2: Replace `FlowDetail`**

Replace `web/src/components/FlowDetail.tsx` with the file below. This deletes the local `prettyBody`. The dock button is not in this task.

```tsx
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

export default function FlowDetail({ flow, loading }: Props) {
  const [tab, setTab] = useState<Tab>('headers')
  const [mode, setMode] = useState<BodyMode>('pretty')
  const [copied, setCopied] = useState(false)
  const [copyError, setCopyError] = useState(false)
  const copyTimer = useRef<number | null>(null)

  const clearCopyTimer = () => {
    if (copyTimer.current == null) return
    window.clearTimeout(copyTimer.current)
    copyTimer.current = null
  }

  useEffect(() => () => clearCopyTimer(), [])

  useEffect(() => {
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
    const write = navigator.clipboard?.writeText
    if (!write) {
      setCopied(false)
      setCopyError(true)
      return
    }
    try {
      await write.call(navigator.clipboard, text)
      setCopyError(false)
      setCopied(true)
      clearCopyTimer()
      copyTimer.current = window.setTimeout(() => {
        setCopied(false)
        copyTimer.current = null
      }, 1500)
    } catch {
      clearCopyTimer()
      setCopied(false)
      setCopyError(true)
    }
  }

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
```

`mode` is not written to `localStorage`. Changing `tab` clears both copy messages and the 1500 ms timer. Changing the selected flow does not reset `mode`.

Add to `web/src/styles.css`:

```css
.tabs { align-items: center; height: auto; min-height: 28px; flex-wrap: nowrap; }
.tabs-main { display: flex; min-width: 0; }
.tabs-actions { margin-left: auto; display: flex; align-items: center; gap: 4px; flex: none; padding-right: 4px; }
.copy-error { color: var(--danger); }
```

The existing `.tabs` rule sets `display: flex` and `height: 26px`. The new rule overrides the height so the 24px buttons fit on one row.

- [ ] **Step 3: Run tests and typecheck**

Run: `cd web && npm test && npx tsc -b --pretty false`

Expected: PASS. `columns.test.ts` still passes with `statusCell` exported. `tsc` prints nothing.

- [ ] **Step 4: Commit**

```bash
git add web/src/columns.ts web/src/components/FlowDetail.tsx web/src/styles.css
git commit -m "$(cat <<'EOF'
feat: show query params and copy detail text

EOF
)"
```

---

### Task 5: Detail dock

**Files:**
- Modify: `web/src/App.tsx`
- Modify: `web/src/components/FlowDetail.tsx`
- Modify: `web/src/styles.css`

**Interfaces:**
- Consumes: `loadDock` and `saveDock` from `web/src/detailView.ts`. `FlowDetail` from Task 4.
- Produces: `FlowDetail` props `{ flow: Flow | null; loading: boolean; dock: 'bottom' | 'right'; onToggleDock: () => void }`.

Clicking the dock button changes only `dock`. It does not change the selected flow, `tab`, or `mode`.

- [ ] **Step 1: Hold the dock in `App`**

In `web/src/App.tsx`, import `loadDock` and `saveDock` from `./detailView`.

Add state next to the other `useState` calls:

```tsx
const [dock, setDock] = useState<'bottom' | 'right'>(() => loadDock())
const onToggleDock = () => {
  setDock((current) => {
    const next = current === 'bottom' ? 'right' : 'bottom'
    saveDock(next)
    return next
  })
}
```

Replace the workspace markup with:

```tsx
<main className={dock === 'right' ? 'workspace dock-right' : 'workspace dock-bottom'}>
  <div className="pane pane-list">
    <FlowTable flows={flows} selectedId={selectedId} onSelect={setSelectedId} visible={visible} />
  </div>
  <div className="pane pane-detail">
    <FlowDetail flow={detail} loading={loading} dock={dock} onToggleDock={onToggleDock} />
  </div>
</main>
```

- [ ] **Step 2: Add the dock button to `FlowDetail`**

Add `dock` and `onToggleDock` to `Props` and to the function arguments. Leave the Task 4 state, copy handler, query section, and body rendering in place.

```tsx
interface Props {
  flow: Flow | null
  loading: boolean
  dock: 'bottom' | 'right'
  onToggleDock: () => void
}
```

Add this component in the same file:

```tsx
function DockIcon({ edge }: { edge: 'right' | 'bottom' }) {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" aria-hidden="true">
      <rect x="2" y="2" width="12" height="12" rx="1" />
      {edge === 'right' ? <path d="M10 2v12" strokeWidth="2.5" /> : <path d="M2 10h12" strokeWidth="2.5" />}
    </svg>
  )
}
```

Immediately before `return (` in `FlowDetail`, add:

```tsx
const dockLabel = dock === 'bottom' ? '详情放到右侧' : '详情放到下方'
```

Inside `.tabs-actions`, after the copy-error span, add:

```tsx
<button type="button" className="btn btn-icon" title={dockLabel} aria-label={dockLabel} onClick={onToggleDock}>
  <DockIcon edge={dock === 'bottom' ? 'right' : 'bottom'} />
</button>
```

The action order is pretty/raw, then `复制`, then the dock button. Pretty/raw still render only on the request and response tabs. The dock button renders on every tab, including when `flow` is null.

- [ ] **Step 3: Replace the pane classes**

In `web/src/styles.css`, replace the workspace and pane rules:

```css
.workspace { flex: 1; display: flex; min-height: 0; min-width: 0; }
.workspace.dock-bottom { flex-direction: column; }
.workspace.dock-right { flex-direction: row; }
.pane { min-height: 0; min-width: 0; overflow: hidden; display: flex; flex-direction: column; }
.dock-bottom .pane-list { flex: 1 1 55%; border-bottom: 1px solid var(--border); }
.dock-bottom .pane-detail { flex: 1 1 45%; }
.dock-right .pane-list { flex: 1 1 55%; border-right: 1px solid var(--border); }
.dock-right .pane-detail { flex: 1 1 45%; }
```

Delete `.pane-top` and `.pane-bottom`. Do not add a resize handle.

- [ ] **Step 4: Typecheck and run the web tests**

Run: `cd web && npm test && npx tsc -b --pretty false`

Expected: PASS. `tsc` prints nothing. Confirm `rg "pane-top|pane-bottom|capture-summary|captureSummary" web/src web/*.test.ts` prints nothing.

- [ ] **Step 5: Commit**

```bash
git add web/src/App.tsx web/src/components/FlowDetail.tsx web/src/styles.css
git commit -m "$(cat <<'EOF'
feat: dock the flow detail below or to the right

EOF
)"
```
