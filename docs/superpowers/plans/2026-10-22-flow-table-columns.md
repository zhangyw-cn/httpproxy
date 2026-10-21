# Flow List Columns Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the flow list show a fixed set of optional summary columns, chosen from a 「列」 menu and remembered in `localStorage`.

**Architecture:** `web/src/columns.ts` owns the column catalog, storage parsing, time formatting, and cell text. `App` holds the visible-id array and passes it to `ColumnPicker` and `FlowTable`. The picker only toggles that array. The table renders catalog order and applies widths from the catalog. No backend or filter changes.

**Tech Stack:** React 19, TypeScript, Vite, one hand-written `styles.css`. No frontend test framework. Pure logic is tested with Node's built-in `node:test` and type stripping. The web `tsc` project does not typecheck that test file.

**Spec:** `docs/superpowers/specs/2026-10-22-flow-table-columns-design.md`

## Global Constraints

- Web flow list only. Do not change Go, REST, WebSocket, `FlowSummary`, the detail panel, README, or the CLI.
- Do not change which rows match a filter, and do not send column choice to the backend.
- Do not add drag-to-reorder or drag-to-resize.
- Do not add columns beyond the catalog in this plan.
- Do not add a frontend test framework or an npm test dependency.
- Storage key is `httpproxy.columns`. The stored value is a JSON array of optional column ids, without `name`.
- Missing key, thrown storage access, illegal JSON, or a non-array JSON value restores `["host","status","method","version","size","time"]`.
- A legal array drops non-strings, unknown ids, `name`, and duplicates, then sorts by catalog order. `[]` and `["nope"]` both show only 「名称」.
- Read storage once on load. Do not subscribe to storage events. Do not write on load.
- Write the sorted JSON array on every toggle. A thrown write leaves the in-memory selection in place and leaves the stored value unchanged.
- 「名称」 is locked, shows `path` or `/`, and its hover title is `scheme://host` plus that same path. The grey host suffix inside the name cell is removed.
- Column order is `name`, `host`, `status`, `method`, `version`, `size`, `reqSize`, `time`, `started`, `client`, `error`.
- Status text stays `(失败)` / `(等待)` / the status digits, with the existing `st-err`, `st-pending`, `st-ok`, and `st-redirect` classes.
- Duration uses the existing `formatTime`. Pending rows (no status code and no error) show `…`.
- Response size and request size use the existing `formatSize`. `0` is `0 B`.
- `started_at` renders as local `HH:mm:ss.SSS`. An unparseable value renders the raw string.
- Host, client address, and error ellipsize on one line and expose the full text as the cell title when non-empty.
- Empty-list copy stays `尚未捕获请求。将客户端指向该代理后即可开始记录。` Its `colSpan` equals the visible column count.
- Error rows still color every cell with the danger color. Selected rows stay white text.
- The 「列」 button is the last filter-bar item, pinned with `margin-left: auto`. While the menu is open, the button's text and border use `--accent`.
- The menu lists every catalog column as a checkbox. 「名称」 is checked and disabled. Toggling does not close the menu. A second click on the button, Escape, or a `pointerdown` outside the picker closes it. The button's own `pointerdown` is not an outside click.
- The menu overlays the table, stacks above `.workspace`, is not clipped by the list scroller, and scrolls internally at a max height of `320px`.
- Table min-width is `160` plus the fixed widths of the visible columns. Extra horizontal space goes to 「名称」. Narrower viewports scroll inside `.table-wrap`. The header stays sticky on vertical scroll and moves with horizontal scroll.
- Do not stage the unrelated dirty file `internal/ui/embed_test.go`.

## File Structure

| Path | Responsibility |
| --- | --- |
| `web/src/columns.ts` | Catalog, storage read/write, `formatStarted`, cell text, active columns, table min-width |
| `web/columns.test.ts` | `node:test` coverage for that module. Outside `web/src`, so `npm run build` does not typecheck it |
| `web/src/components/ColumnPicker.tsx` | 「列」 button and checkbox menu |
| `web/src/App.tsx` | Visible-column state, filter-bar row, passes `visible` into the table |
| `web/src/components/FilterBar.tsx` | Filter inputs and status chips only. No `.filterbar` wrapper |
| `web/src/components/FlowTable.tsx` | Renders the active columns |
| `web/src/styles.css` | Filter-bar stacking, picker menu, deletion of `.name-host` and the old fixed column-width rules |

`web/src/components/FlowDetail.tsx` stays as it is and keeps importing `formatSize` from `FlowTable`. `formatSize` and `formatTime` stay in `FlowTable.tsx`.

---

### Task 1: Column catalog and pure helpers

**Files:**
- Create: `web/src/columns.ts`
- Test: `web/columns.test.ts`

**Interfaces:**
- Consumes: nothing
- Produces:
  - `ColumnDef`: `{ id: string; label: string; locked: boolean; width: number | 'auto'; align: 'left' | 'right'; defaultVisible: boolean }`
  - `COLUMNS: ColumnDef[]` in spec order
  - `COLUMN_STORAGE_KEY = 'httpproxy.columns'`
  - `DEFAULT_VISIBLE: string[]` equal to `["host","status","method","version","size","time"]`
  - `normalizeVisible(raw: unknown): string[]`
  - `loadVisible(storage?: Pick<Storage, 'getItem'>): string[]`
  - `saveVisible(ids: string[], storage?: Pick<Storage, 'setItem'>): void`
  - `formatStarted(raw: string): string`
  - `CellFlow`, `CellFormats`, `FlowCell`
  - `flowCell(id: string, flow: CellFlow, format: CellFormats): FlowCell`
  - `activeColumns(visible: string[]): ColumnDef[]`
  - `tableMinWidth(visible: string[]): number`

`loadVisible()` and `saveVisible()` with no storage argument use `localStorage`, and that access happens inside the `try`. A default parameter would throw before the `try` in environments where `localStorage` is missing.

- [ ] **Step 1: Write the failing test**

Create `web/columns.test.ts`:

```ts
import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import {
  COLUMNS,
  COLUMN_STORAGE_KEY,
  DEFAULT_VISIBLE,
  activeColumns,
  flowCell,
  formatStarted,
  loadVisible,
  normalizeVisible,
  saveVisible,
  tableMinWidth,
  type CellFlow,
} from './src/columns.ts'

const format = {
  size: (n: number) => `S${n}`,
  time: (ms: number) => `T${ms}`,
}

function flow(patch: Partial<CellFlow> = {}): CellFlow {
  return {
    scheme: 'https',
    method: 'GET',
    host: 'example.test',
    path: '/a',
    http_version: '2',
    status_code: 200,
    req_size: 12,
    resp_size: 34,
    started_at: '2026-10-22T01:02:03.006Z',
    duration_ms: 1500,
    client_addr: '10.0.0.8:4000',
    ...patch,
  }
}

function memory(initial?: string) {
  const data = new Map<string, string>()
  if (initial !== undefined) data.set(COLUMN_STORAGE_KEY, initial)
  return {
    getItem(key: string) {
      return data.has(key) ? data.get(key)! : null
    },
    setItem(key: string, value: string) {
      data.set(key, value)
    },
    raw: data,
  }
}

describe('catalog', () => {
  it('lists the spec columns in order', () => {
    assert.deepEqual(
      COLUMNS.map((c) => [c.id, c.label, c.locked, c.width, c.align, c.defaultVisible]),
      [
        ['name', '名称', true, 'auto', 'left', true],
        ['host', '主机', false, 180, 'left', true],
        ['status', '状态', false, 80, 'left', true],
        ['method', '方法', false, 72, 'left', true],
        ['version', '版本', false, 72, 'left', true],
        ['size', '大小', false, 80, 'right', true],
        ['reqSize', '请求大小', false, 88, 'right', false],
        ['time', '耗时', false, 80, 'right', true],
        ['started', '开始时间', false, 112, 'left', false],
        ['client', '客户端地址', false, 148, 'left', false],
        ['error', '错误', false, 200, 'left', false],
      ],
    )
    assert.deepEqual(DEFAULT_VISIBLE, ['host', 'status', 'method', 'version', 'size', 'time'])
    assert.equal(COLUMN_STORAGE_KEY, 'httpproxy.columns')
  })
})

describe('normalizeVisible', () => {
  it('restores the default for non-arrays', () => {
    for (const raw of [null, undefined, 'host', 1, { host: true }]) {
      assert.deepEqual(normalizeVisible(raw), DEFAULT_VISIBLE)
    }
  })

  it('drops unknown ids, name, non-strings, and duplicates, then sorts', () => {
    assert.deepEqual(normalizeVisible(['time', 'host', 'host', 'name', 'nope', 1, null]), ['host', 'time'])
    assert.deepEqual(normalizeVisible([]), [])
    assert.deepEqual(normalizeVisible(['nope']), [])
  })
})

describe('storage', () => {
  it('uses the default when the key is missing and does not write', () => {
    const store = memory()
    assert.deepEqual(loadVisible(store), DEFAULT_VISIBLE)
    assert.equal(store.raw.size, 0)
  })

  it('uses the default for illegal JSON and non-arrays', () => {
    assert.deepEqual(loadVisible(memory('{')), DEFAULT_VISIBLE)
    assert.deepEqual(loadVisible(memory('{"host":true}')), DEFAULT_VISIBLE)
    assert.deepEqual(loadVisible(memory('null')), DEFAULT_VISIBLE)
  })

  it('keeps a legal array after filtering', () => {
    assert.deepEqual(loadVisible(memory('["host","nope"]')), ['host'])
    assert.deepEqual(loadVisible(memory('[]')), [])
  })

  it('returns the default when reading storage throws', () => {
    assert.deepEqual(
      loadVisible({
        getItem() {
          throw new Error('denied')
        },
      }),
      DEFAULT_VISIBLE,
    )
    assert.deepEqual(loadVisible(), DEFAULT_VISIBLE)
  })

  it('writes canonical JSON and swallows write failures', () => {
    const store = memory()
    saveVisible(['time', 'host', 'host', 'nope'], store)
    assert.equal(store.raw.get(COLUMN_STORAGE_KEY), '["host","time"]')
    assert.doesNotThrow(() =>
      saveVisible(['host'], {
        setItem() {
          throw new Error('denied')
        },
      }),
    )
  })
})

describe('formatStarted', () => {
  it('formats local time with zero-padded milliseconds', () => {
    const local = new Date(2026, 9, 22, 9, 8, 7, 6)
    assert.equal(formatStarted(local.toISOString()), '09:08:07.006')
    const raw = '2026-10-22T01:02:03.006789123+08:00'
    const d = new Date(raw)
    const p = (n: number, w = 2) => String(n).padStart(w, '0')
    assert.equal(
      formatStarted(raw),
      `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}.${p(d.getMilliseconds(), 3)}`,
    )
  })

  it('returns the raw string when it is not a time', () => {
    assert.equal(formatStarted('not-a-date'), 'not-a-date')
    assert.equal(formatStarted(''), '')
  })
})

describe('flowCell', () => {
  it('shows only the path in the name cell and puts the full URL on the title', () => {
    assert.deepEqual(flowCell('name', flow(), format), {
      text: '/a',
      title: 'https://example.test/a',
    })
    assert.deepEqual(flowCell('name', flow({ path: '' }), format), {
      text: '/',
      title: 'https://example.test/',
    })
  })

  it('titles host, client, and error only when they are non-empty', () => {
    assert.deepEqual(flowCell('host', flow(), format), { text: 'example.test', title: 'example.test' })
    assert.deepEqual(flowCell('host', flow({ host: '' }), format), { text: '' })
    assert.deepEqual(flowCell('client', flow(), format), { text: '10.0.0.8:4000', title: '10.0.0.8:4000' })
    assert.deepEqual(flowCell('error', flow({ error: 'boom' }), format), { text: 'boom', title: 'boom' })
    assert.deepEqual(flowCell('error', flow(), format), { text: '' })
  })

  it('keeps the existing status words and classes', () => {
    assert.deepEqual(flowCell('status', flow({ error: 'boom', status_code: 500 }), format), {
      text: '(失败)',
      statusClass: 'st-err',
    })
    assert.deepEqual(flowCell('status', flow({ status_code: 0 }), format), {
      text: '(等待)',
      statusClass: 'st-pending',
    })
    assert.deepEqual(flowCell('status', flow({ status_code: 200 }), format), {
      text: '200',
      statusClass: 'st-ok',
    })
    assert.deepEqual(flowCell('status', flow({ status_code: 302 }), format), {
      text: '302',
      statusClass: 'st-redirect',
    })
    assert.deepEqual(flowCell('status', flow({ status_code: 404 }), format), {
      text: '404',
      statusClass: 'st-err',
    })
  })

  it('formats method, version, both sizes, duration, and start time', () => {
    assert.deepEqual(flowCell('method', flow(), format), { text: 'GET' })
    assert.deepEqual(flowCell('version', flow(), format), { text: '2' })
    assert.deepEqual(flowCell('size', flow(), format), { text: 'S34' })
    assert.deepEqual(flowCell('reqSize', flow({ req_size: 0 }), format), { text: 'S0' })
    assert.deepEqual(flowCell('time', flow(), format), { text: 'T1500' })
    assert.deepEqual(flowCell('time', flow({ status_code: 0 }), format), { text: '…' })
    assert.equal(flowCell('time', flow({ status_code: 0, error: 'boom' }), format).text, 'T1500')
    assert.equal(flowCell('started', flow(), format).text, formatStarted('2026-10-22T01:02:03.006Z'))
  })
})

describe('layout helpers', () => {
  it('always includes name and keeps catalog order', () => {
    assert.deepEqual(
      activeColumns(['time', 'host']).map((c) => c.id),
      ['name', 'host', 'time'],
    )
    assert.deepEqual(
      activeColumns(DEFAULT_VISIBLE).map((c) => c.id),
      ['name', 'host', 'status', 'method', 'version', 'size', 'time'],
    )
    assert.deepEqual(activeColumns([]).map((c) => c.id), ['name'])
  })

  it('sums 160 for the name column plus fixed widths', () => {
    assert.equal(tableMinWidth([]), 160)
    assert.equal(tableMinWidth(DEFAULT_VISIBLE), 724)
    assert.equal(
      tableMinWidth(['host', 'status', 'method', 'version', 'size', 'reqSize', 'time', 'started', 'client', 'error']),
      1272,
    )
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run from the repo root:

```bash
node --experimental-strip-types --test web/columns.test.ts
```

Expected: exit non-zero. Output includes `Cannot find module` and `columns.ts`.

- [ ] **Step 3: Write `web/src/columns.ts`**

```ts
export interface ColumnDef {
  id: string
  label: string
  locked: boolean
  width: number | 'auto'
  align: 'left' | 'right'
  defaultVisible: boolean
}

export const COLUMNS: ColumnDef[] = [
  { id: 'name', label: '名称', locked: true, width: 'auto', align: 'left', defaultVisible: true },
  { id: 'host', label: '主机', locked: false, width: 180, align: 'left', defaultVisible: true },
  { id: 'status', label: '状态', locked: false, width: 80, align: 'left', defaultVisible: true },
  { id: 'method', label: '方法', locked: false, width: 72, align: 'left', defaultVisible: true },
  { id: 'version', label: '版本', locked: false, width: 72, align: 'left', defaultVisible: true },
  { id: 'size', label: '大小', locked: false, width: 80, align: 'right', defaultVisible: true },
  { id: 'reqSize', label: '请求大小', locked: false, width: 88, align: 'right', defaultVisible: false },
  { id: 'time', label: '耗时', locked: false, width: 80, align: 'right', defaultVisible: true },
  { id: 'started', label: '开始时间', locked: false, width: 112, align: 'left', defaultVisible: false },
  { id: 'client', label: '客户端地址', locked: false, width: 148, align: 'left', defaultVisible: false },
  { id: 'error', label: '错误', locked: false, width: 200, align: 'left', defaultVisible: false },
]

export const COLUMN_STORAGE_KEY = 'httpproxy.columns'

const OPTIONAL_ORDER = COLUMNS.filter((c) => !c.locked).map((c) => c.id)
const OPTIONAL_IDS = new Set(OPTIONAL_ORDER)

export const DEFAULT_VISIBLE: string[] = COLUMNS.filter((c) => !c.locked && c.defaultVisible).map((c) => c.id)

export function normalizeVisible(raw: unknown): string[] {
  if (!Array.isArray(raw)) return [...DEFAULT_VISIBLE]
  const present = new Set<string>()
  for (const item of raw) {
    if (typeof item === 'string' && OPTIONAL_IDS.has(item)) present.add(item)
  }
  return OPTIONAL_ORDER.filter((id) => present.has(id))
}

export function loadVisible(storage?: Pick<Storage, 'getItem'>): string[] {
  try {
    const store = storage ?? localStorage
    const text = store.getItem(COLUMN_STORAGE_KEY)
    if (text == null) return [...DEFAULT_VISIBLE]
    return normalizeVisible(JSON.parse(text))
  } catch {
    return [...DEFAULT_VISIBLE]
  }
}

export function saveVisible(ids: string[], storage?: Pick<Storage, 'setItem'>): void {
  try {
    const store = storage ?? localStorage
    store.setItem(COLUMN_STORAGE_KEY, JSON.stringify(normalizeVisible(ids)))
  } catch {
    /* in-memory selection stays */
  }
}

export function formatStarted(raw: string): string {
  const d = new Date(raw)
  if (Number.isNaN(d.getTime())) return raw
  const p = (n: number, w = 2) => String(n).padStart(w, '0')
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}.${p(d.getMilliseconds(), 3)}`
}

export interface CellFlow {
  scheme: string
  method: string
  host: string
  path: string
  http_version: string
  status_code: number
  req_size: number
  resp_size: number
  started_at: string
  duration_ms: number
  error?: string
  client_addr: string
}

export interface CellFormats {
  size: (n: number) => string
  time: (ms: number) => string
}

export interface FlowCell {
  text: string
  title?: string
  statusClass?: 'st-err' | 'st-pending' | 'st-ok' | 'st-redirect'
}

function titled(text: string): FlowCell {
  return text ? { text, title: text } : { text }
}

function statusCell(flow: CellFlow): FlowCell {
  if (flow.error) return { text: '(失败)', statusClass: 'st-err' }
  if (!flow.status_code) return { text: '(等待)', statusClass: 'st-pending' }
  const hundred = Math.floor(flow.status_code / 100)
  const statusClass = hundred >= 4 ? 'st-err' : hundred === 3 ? 'st-redirect' : 'st-ok'
  return { text: String(flow.status_code), statusClass }
}

export function flowCell(id: string, flow: CellFlow, format: CellFormats): FlowCell {
  switch (id) {
    case 'name': {
      const path = flow.path || '/'
      return { text: path, title: `${flow.scheme}://${flow.host}${path}` }
    }
    case 'host':
      return titled(flow.host)
    case 'status':
      return statusCell(flow)
    case 'method':
      return { text: flow.method }
    case 'version':
      return { text: flow.http_version }
    case 'size':
      return { text: format.size(flow.resp_size) }
    case 'reqSize':
      return { text: format.size(flow.req_size) }
    case 'time':
      return { text: flow.status_code || flow.error ? format.time(flow.duration_ms) : '…' }
    case 'started':
      return { text: formatStarted(flow.started_at) }
    case 'client':
      return titled(flow.client_addr)
    case 'error':
      return titled(flow.error ?? '')
    default:
      return { text: '' }
  }
}

export function activeColumns(visible: string[]): ColumnDef[] {
  const on = new Set(normalizeVisible(visible))
  return COLUMNS.filter((c) => c.locked || on.has(c.id))
}

export function tableMinWidth(visible: string[]): number {
  return activeColumns(visible).reduce((sum, c) => sum + (typeof c.width === 'number' ? c.width : 160), 0)
}
```

`FlowSummary` is assignable to `CellFlow`, so `flowCell` accepts a summary with no adapter and this file does not import `./types`.

- [ ] **Step 4: Run the test to verify it passes**

Run from the repo root:

```bash
node --experimental-strip-types --test web/columns.test.ts
```

Expected: exit 0. Output includes `fail 0`.

- [ ] **Step 5: Typecheck**

Run: `npm run build` in `web/`

Expected: exit 0. `columns.ts` is included by `web/tsconfig.json` even though no component imports it yet.

- [ ] **Step 6: Commit**

```bash
git add web/src/columns.ts web/columns.test.ts
git commit -m "$(cat <<'EOF'
feat: add the flow column catalog

Keep column order, stored visibility, and cell text in one module.
EOF
)"
```

Do not `git add` `internal/ui/embed_test.go`.

---

### Task 2: Column picker and remembered selection

**Files:**
- Create: `web/src/components/ColumnPicker.tsx`
- Modify: `web/src/App.tsx`
- Modify: `web/src/components/FilterBar.tsx`
- Modify: `web/src/styles.css` (`.filterbar` and the new picker rules)

**Interfaces:**
- Consumes: `COLUMNS`, `loadVisible`, `normalizeVisible`, `saveVisible` from `web/src/columns.ts`
- Produces: `ColumnPicker` props `{ visible: string[]; onChange: (ids: string[]) => void }`. `App` state `visible: string[]` is the normalized optional-id list. `FilterBar` no longer renders `.filterbar`.

This task does not change `FlowTable`. The menu can change and persist the selection before the table reads it.

- [ ] **Step 1: Write the failing structural check**

Run from the repo root:

```bash
node --input-type=module -e "
import { readFileSync } from 'node:fs';
const fail = (m) => { console.error(m); process.exit(1); };
const app = readFileSync('web/src/App.tsx', 'utf8');
const filter = readFileSync('web/src/components/FilterBar.tsx', 'utf8');
const css = readFileSync('web/src/styles.css', 'utf8');
let picker = '';
try { picker = readFileSync('web/src/components/ColumnPicker.tsx', 'utf8'); } catch { fail('missing ColumnPicker'); }
if (!app.includes('className=\"filterbar\"')) fail('App must own filterbar');
if (!app.includes('loadVisible')) fail('App must load columns once');
if (!app.includes('normalizeVisible')) fail('App must normalize before saving');
if (!app.includes('<ColumnPicker')) fail('App must render ColumnPicker');
if (filter.includes('filterbar')) fail('FilterBar must not own filterbar');
if (!picker.includes('>列</button>')) fail('button label');
if (!picker.includes('disabled={c.locked}')) fail('name checkbox locked');
if (!picker.includes(\"event.key === 'Escape'\")) fail('Escape closes');
if (!picker.includes(\"'pointerdown'\")) fail('pointerdown closes');
if (!css.includes('margin-left: auto')) fail('button pinned right');
if (!css.includes('max-height: 320px')) fail('menu max height');
if (!css.includes('z-index: 2')) fail('menu stacks above the table');
console.log('picker ok');
"
```

Expected: exit non-zero because `ColumnPicker.tsx` does not exist. Output includes `missing ColumnPicker`.

- [ ] **Step 2: Replace `FilterBar` so it renders only the controls**

Replace `web/src/components/FilterBar.tsx` with:

```tsx
import type { Filters } from '../types'

interface Props {
  filters: Filters
  onChange: (f: Filters) => void
}

const STATUS_PRESETS = ['2', '3', '4', '5']

export default function FilterBar({ filters, onChange }: Props) {
  const set = (patch: Partial<Filters>) => onChange({ ...filters, ...patch })
  return (
    <>
      <input
        className="input"
        placeholder="筛选主机"
        value={filters.host}
        onChange={(e) => set({ host: e.target.value })}
      />
      <input
        className="input"
        placeholder="筛选路径"
        value={filters.path}
        onChange={(e) => set({ path: e.target.value })}
      />
      <input
        className="input input-status"
        placeholder="状态码"
        title="1 位表示类别（2 → 2xx），2 位表示前缀（40 → 40x），3 位表示精确匹配"
        value={filters.status}
        onChange={(e) => set({ status: e.target.value.replace(/\D/g, '').slice(0, 3) })}
      />
      <div className="chips">
        <button
          className={`chip${filters.status === '' ? ' active' : ''}`}
          onClick={() => set({ status: '' })}
        >
          全部
        </button>
        {STATUS_PRESETS.map((s) => (
          <button
            key={s}
            className={`chip${filters.status === s ? ' active' : ''}`}
            onClick={() => set({ status: filters.status === s ? '' : s })}
          >
            {s}xx
          </button>
        ))}
      </div>
    </>
  )
}
```

- [ ] **Step 3: Create `ColumnPicker`**

Create `web/src/components/ColumnPicker.tsx`:

```tsx
import { useEffect, useRef, useState } from 'react'
import { COLUMNS } from '../columns'

interface Props {
  visible: string[]
  onChange: (ids: string[]) => void
}

export default function ColumnPicker({ visible, onChange }: Props) {
  const [open, setOpen] = useState(false)
  const rootRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onPointerDown = (event: PointerEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false)
    }
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false)
    }
    document.addEventListener('pointerdown', onPointerDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('pointerdown', onPointerDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  const toggle = (id: string) => {
    const next = new Set(visible)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    onChange([...next])
  }

  return (
    <div className="col-picker" ref={rootRef}>
      <button
        type="button"
        className={`btn col-picker-btn${open ? ' open' : ''}`}
        onClick={() => setOpen((value) => !value)}
      >列</button>
      {open && (
        <div className="col-menu">
          {COLUMNS.map((c) => (
            <label key={c.id} className="col-menu-item">
              <input
                type="checkbox"
                checked={c.locked || visible.includes(c.id)}
                disabled={c.locked}
                onChange={() => {
                  if (!c.locked) toggle(c.id)
                }}
              />
              {c.label}
            </label>
          ))}
        </div>
      )}
    </div>
  )
}
```

The checkbox `onChange` does not call `setOpen(false)`. The document listener ignores events whose target is inside `rootRef`, so pressing the button only runs the button's `onClick`.

- [ ] **Step 4: Hold the selection in `App`**

In `web/src/App.tsx`, add these imports beside the existing component imports:

```tsx
import { loadVisible, normalizeVisible, saveVisible } from './columns'
import ColumnPicker from './components/ColumnPicker'
```

Inside `App`, immediately after the `filters` state, add:

```tsx
  const [visible, setVisible] = useState<string[]>(() => loadVisible())
  const onVisible = (ids: string[]) => {
    const next = normalizeVisible(ids)
    setVisible(next)
    saveVisible(next)
  }
```

Replace the `<FilterBar ... />` line with:

```tsx
      <div className="filterbar">
        <FilterBar filters={filters} onChange={setFilters} />
        <ColumnPicker visible={visible} onChange={onVisible} />
      </div>
```

Leave `FlowTable` without a `visible` prop in this task. Do not pass `visible` into `matches`, `listFlows`, or the websocket handler.

- [ ] **Step 5: Stack the menu above the table**

Replace the `.filterbar` rule in `web/src/styles.css` with:

```css
.filterbar {
  display: flex; align-items: center; gap: 8px;
  position: relative; z-index: 2;
  padding: 4px 12px;
  background: var(--bg);
  border-bottom: 1px solid var(--border);
  flex: none;
}
```

Add these rules immediately after the `.chip.active` rule:

```css
.col-picker { position: relative; margin-left: auto; }
.col-picker-btn.open { color: var(--accent); border-color: var(--accent); }
.col-menu {
  position: absolute; top: calc(100% + 4px); right: 0; z-index: 2;
  min-width: 140px; max-height: 320px; overflow: auto;
  padding: 4px 0;
  background: var(--bg); color: var(--text);
  border: 1px solid var(--border); border-radius: 6px;
}
.col-menu-item {
  display: flex; align-items: center; gap: 8px;
  height: 24px; padding: 0 10px;
  cursor: pointer; white-space: nowrap;
}
.col-menu-item:hover { background: var(--bg-hover); }
.col-menu-item input { margin: 0; }
```

- [ ] **Step 6: Run the structural check and the column tests**

Run the Step 1 command again from the repo root.

Expected: prints `picker ok` and exits 0.

Then run:

```bash
node --experimental-strip-types --test web/columns.test.ts
```

Expected: exit 0 and `fail 0`.

- [ ] **Step 7: Build**

Run: `npm run build` in `web/`

Expected: exit 0.

- [ ] **Step 8: Commit**

```bash
git add web/src/App.tsx web/src/components/ColumnPicker.tsx web/src/components/FilterBar.tsx web/src/styles.css
git commit -m "$(cat <<'EOF'
feat: add the flow list column picker

Remember the visible columns locally without changing the filtered request.
EOF
)"
```

Do not `git add` `internal/ui/embed_test.go`.

---

### Task 3: Render the selected columns

**Files:**
- Modify: `web/src/components/FlowTable.tsx`
- Modify: `web/src/App.tsx` (pass `visible`)
- Modify: `web/src/styles.css` (delete `.name-host` and the old column width rules)

**Interfaces:**
- Consumes: `activeColumns`, `flowCell`, `tableMinWidth`, `ColumnDef` from `web/src/columns.ts`. `App`'s `visible: string[]`. `formatSize` and `formatTime` already exported from `FlowTable.tsx`.
- Produces: `FlowTable` props `{ flows: FlowSummary[]; selectedId: string | null; onSelect: (id: string) => void; visible: string[] }`

- [ ] **Step 1: Write the failing structural check**

Run from the repo root:

```bash
node --input-type=module -e "
import { readFileSync } from 'node:fs';
const fail = (m) => { console.error(m); process.exit(1); };
const app = readFileSync('web/src/App.tsx', 'utf8');
const table = readFileSync('web/src/components/FlowTable.tsx', 'utf8');
const css = readFileSync('web/src/styles.css', 'utf8');
const detail = readFileSync('web/src/components/FlowDetail.tsx', 'utf8');
if (!app.includes('visible={visible}')) fail('App must pass visible');
if (!table.includes('activeColumns(visible)')) fail('table must use active columns');
if (!table.includes('tableMinWidth(visible)')) fail('table min width');
if (!table.includes('flowCell(')) fail('table must use flowCell');
if (!table.includes('colSpan={cols.length}')) fail('empty colspan');
if (table.includes('name-host')) fail('name cell still shows host');
if (table.includes('colSpan={6}')) fail('hardcoded colspan');
if (css.includes('name-host')) fail('name-host css remains');
if (css.includes('.col-status { width:')) fail('old status width remains');
if (!table.includes('export function formatSize')) fail('formatSize moved');
if (!table.includes('export function formatTime')) fail('formatTime moved');
if (!detail.includes(\"from './FlowTable'\")) fail('detail must keep importing formatSize');
const sizeFn = table.slice(table.indexOf('function formatSize'), table.indexOf('function formatTime'));
if (!sizeFn.includes(' B') || !sizeFn.includes('kB') || !sizeFn.includes('MB')) fail('size units');
const timeFn = table.slice(table.indexOf('function formatTime'), table.indexOf('const formats'));
if (!timeFn.includes(' ms') || !timeFn.includes(' s')) fail('time units');
console.log('table ok');
"
```

Expected: exit non-zero. Output includes `App must pass visible` or `table must use active columns`.

- [ ] **Step 2: Pass `visible` from `App`**

In `web/src/App.tsx`, replace:

```tsx
          <FlowTable flows={flows} selectedId={selectedId} onSelect={setSelectedId} />
```

with:

```tsx
          <FlowTable flows={flows} selectedId={selectedId} onSelect={setSelectedId} visible={visible} />
```

- [ ] **Step 3: Replace `FlowTable`**

Replace `web/src/components/FlowTable.tsx` with:

```tsx
import { useEffect, useRef, type CSSProperties } from 'react'
import { activeColumns, flowCell, tableMinWidth, type ColumnDef } from '../columns'
import type { FlowSummary } from '../types'

interface Props {
  flows: FlowSummary[]
  selectedId: string | null
  onSelect: (id: string) => void
  visible: string[]
}

export function formatSize(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} kB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

export function formatTime(ms: number): string {
  if (ms < 1000) return `${ms} ms`
  return `${(ms / 1000).toFixed(2)} s`
}

const formats = { size: formatSize, time: formatTime }

function colStyle(column: ColumnDef): CSSProperties {
  if (column.width === 'auto') return { minWidth: 160, textAlign: column.align }
  return { width: column.width, textAlign: column.align }
}

export default function FlowTable({ flows, selectedId, onSelect, visible }: Props) {
  const bodyRef = useRef<HTMLDivElement>(null)
  const stick = useRef(true)
  const cols = activeColumns(visible)

  useEffect(() => {
    const el = bodyRef.current
    if (el && stick.current) el.scrollTop = el.scrollHeight
  }, [flows.length])

  const onScroll = () => {
    const el = bodyRef.current
    if (el) stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 24
  }

  return (
    <div className="table-wrap" ref={bodyRef} onScroll={onScroll}>
      <table className="flow-table" style={{ minWidth: tableMinWidth(visible) }}>
        <thead>
          <tr>
            {cols.map((c) => (
              <th key={c.id} style={colStyle(c)}>{c.label}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {flows.map((f) => {
            const status = flowCell('status', f, formats)
            return (
              <tr
                key={f.id}
                className={f.id === selectedId ? 'selected' : status.statusClass === 'st-err' ? 'row-err' : ''}
                onClick={() => onSelect(f.id)}
              >
                {cols.map((c) => {
                  const cell = c.id === 'status' ? status : flowCell(c.id, f, formats)
                  const statusCls = c.id === 'status' && cell.statusClass ? ` ${cell.statusClass}` : ''
                  return (
                    <td key={c.id} className={statusCls.trim()} style={colStyle(c)} title={cell.title}>
                      {c.id === 'name' ? <span className="name-path">{cell.text}</span> : cell.text}
                    </td>
                  )
                })}
              </tr>
            )
          })}
          {flows.length === 0 && (
            <tr className="empty">
              <td colSpan={cols.length}>尚未捕获请求。将客户端指向该代理后即可开始记录。</td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  )
}
```

Do not give 「名称」 a fixed `width`. `minWidth: 160` plus the table `minWidth` keeps the floor, and the column without a width receives the extra space when the table is wider than that floor. `table-layout: fixed` and `width: 100%` stay in CSS. `.table-wrap` already has `overflow: auto`. The header rule already has `position: sticky; top: 0`.

The row class still prefers `selected` over `row-err`. Status is computed even when that column is hidden, so a failed row stays `row-err`.

- [ ] **Step 4: Remove the obsolete column CSS**

Delete these rules from `web/src/styles.css`:

```css
.col-name { width: auto; }
.col-status { width: 80px; }
.col-method { width: 72px; }
.col-version { width: 72px; }
.col-size { width: 80px; text-align: right; }
.col-time { width: 80px; text-align: right; }
.name-host { margin-left: 8px; color: var(--text-muted); }
tr.selected .name-host { color: rgba(255, 255, 255, 0.8); }
```

Keep `.name-path`, `.st-err`, `.st-redirect`, `.st-pending`, the selected-row rules, and the `row-err` rule.

- [ ] **Step 5: Run the structural check and the column tests**

Run the Step 1 command again from the repo root.

Expected: prints `table ok` and exits 0.

Then run:

```bash
node --experimental-strip-types --test web/columns.test.ts
```

Expected: exit 0 and `fail 0`.

- [ ] **Step 6: Build**

Run: `npm run build` in `web/`

Expected: exit 0.

- [ ] **Step 7: Check the list in a browser**

Start `npm run dev` in `web/` and open the printed local URL. `make dev-web` from the repo root is the same server.

1. Delete `httpproxy.columns` and reload. Visible headers are 名称、主机、状态、方法、版本、大小、耗时. The name cell is only the path. Hovering it shows `scheme://host` plus that path. An empty path shows `/` in both the cell and the hover URL.
2. Open 「列」. 「名称」 is checked and cannot be cleared. Check 请求大小、开始时间、客户端地址、错误. Those columns appear immediately, in catalog order, and the menu stays open. A start time looks like `09:08:07.006`. Long host, client address, and error values ellipsize, and their hover text is the full value. Reload: the same columns remain.
3. Clear every optional checkbox and reload. Only 「名称」 remains.
4. Set `httpproxy.columns` to `{` and reload. The default columns return. Set it to `["host","nope"]` and reload: only 名称 and 主机. Set it to `[]` and reload: only 名称.
5. Turn every column on. The list scrolls horizontally. Vertical scrolling keeps the header stuck to the top of the list.
6. The detail pane, host/path/status filtering, and both themes still behave as they do today. The open 「列」 button uses the accent text and border. The menu uses the existing background, text, and border variables.

- [ ] **Step 8: Commit**

```bash
git add web/src/App.tsx web/src/components/FlowTable.tsx web/src/styles.css
git commit -m "$(cat <<'EOF'
feat: show the selected columns on the flow list

The name cell is only the path. Extra fields stay on their own columns.
EOF
)"
```

Do not `git add` `internal/ui/embed_test.go`.
