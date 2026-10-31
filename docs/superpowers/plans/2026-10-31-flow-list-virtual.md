# Flow List Virtualization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Keep the capture list visually stable by virtualizing its rows, pinning stripes to each request id, and correcting scroll position before paint.

**Architecture:** `web/src/flowWindow.ts` owns the stripe book, the visible window, and the pre-paint scroll math. `App` keeps the book and a `listEpoch` that remounts the table on clear. `FlowTable` mounts only the visible rows inside the existing table, with spacer rows for the rest. No virtual-list library and no backend change.

**Tech Stack:** React 19, TypeScript, Vite, one hand-written `styles.css`. No frontend test framework. Pure logic is tested with Node's built-in `node:test` and type stripping. `web/tsconfig.json` includes only `src`, so `npm run build` typechecks `flowWindow.ts` and does not typecheck `web/flowWindow.test.ts`.

**Spec:** `docs/superpowers/specs/2026-10-31-flow-list-virtual-design.md`

## Global Constraints

- Web flow list only. Do not change Go, REST, WebSocket, `FlowSummary`, the detail panel, column definitions, README, or the CLI.
- Do not change which rows match a filter.
- Do not add a virtual-list dependency or a browser test framework.
- Do not virtualize horizontally, do not measure individual row heights, and do not use smooth scrolling.
- `ROW_HEIGHT` is 22, `HEADER_HEIGHT` is 22, `OVERSCAN` is 10, and `STICK_PX` is 24. Distance to the bottom is stuck only when it is strictly less than 24.
- Stripe bit 0 uses the table background. Stripe bit 1 uses `var(--bg-subtle)` via the class `row-stripe`. Hover and selected rules stay after that class and cover it. Error rows still use `row-err` for text color only. Selected rows stay white text.
- Empty-list copy stays `尚未捕获请求。将客户端指向该代理后即可开始记录。`
- `formatSize` and `formatTime` stay exported from `FlowTable.tsx`. `FlowDetail.tsx` keeps `import { formatSize } from './FlowTable'`.
- The worktree already has unrelated modifications. Do not stage `internal/**`, `web/src/capture.ts`, `web/capture.test.ts`, or `internal/ui/dist/**`. Do not revert those edits.
- `web/src/App.tsx` is already dirty. Task 4 edits it further. Do not revert existing hunks. Do not use `git add -p`. If `git diff web/src/App.tsx` still contains hunks that are not the virtual-list wiring, do not commit `web/src/App.tsx`; leave that file in the worktree and say so.

---

## File Structure

| Path | Responsibility |
| --- | --- |
| `web/src/flowWindow.ts` | Stripe book, visible range, stuck check, pre-paint `scrollTop` |
| `web/flowWindow.test.ts` | `node:test` coverage for that module |
| `web/package.json` | Adds `flowWindow.test.ts` to the `test` script |
| `web/src/components/FlowTable.tsx` | Scroll container, spacer rows, memoized data rows |
| `web/src/styles.css` | `row-stripe` and `row-pad`; deletes `nth-child(even)` |
| `web/src/App.tsx` | Stripe book ref, `listEpoch`, passes `stripes` and the remount key |

`FlowTable` props become `{ flows: FlowSummary[]; stripes: number[]; selectedId: string | null; onSelect: (id: string) => void; visible: string[] }`. `listEpoch` is the React `key` on that element, not a prop.

---

### Task 1: Stripe book

**Files:**
- Create: `web/src/flowWindow.ts`
- Create: `web/flowWindow.test.ts`
- Modify: `web/package.json` (the `test` script)
- Test: `web/flowWindow.test.ts`

**Interfaces:**
- Consumes: nothing
- Produces:
  - `interface StripeBook { next: number; bits: Map<string, 0 | 1> }`
  - `createStripeBook(): StripeBook`
  - `resetStripes(book: StripeBook): void`
  - `syncStripes(book: StripeBook, ids: readonly string[]): number[]`

- [ ] **Step 1: Write the failing test**

Create `web/flowWindow.test.ts`:

```ts
import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { createStripeBook, resetStripes, syncStripes } from './src/flowWindow.ts'

describe('syncStripes', () => {
  it('assigns alternating bits and keeps them', () => {
    const book = createStripeBook()
    assert.deepEqual(syncStripes(book, ['a', 'b', 'c', 'd']), [0, 1, 0, 1])
    assert.equal(book.next, 4)

    assert.deepEqual(syncStripes(book, ['a', 'b', 'c', 'd']), [0, 1, 0, 1])
    assert.equal(book.next, 4)

    assert.deepEqual(syncStripes(book, ['a', 'b', 'c', 'd', 'e']), [0, 1, 0, 1, 0])
    assert.equal(book.next, 5)

    assert.deepEqual(syncStripes(book, ['a', 'c', 'd', 'e']), [0, 0, 1, 0])
    assert.equal(book.bits.get('b'), 1)
    assert.equal(book.next, 5)
    assert.equal(book.bits.size, 5)

    assert.deepEqual(syncStripes(book, []), [])
    assert.equal(book.next, 5)
    assert.equal(book.bits.size, 5)

    resetStripes(book)
    assert.equal(book.next, 0)
    assert.equal(book.bits.size, 0)
    assert.deepEqual(syncStripes(book, ['a']), [0])
    assert.equal(book.next, 1)
  })
})
```

In `web/package.json`, change the `test` script from:

```json
"test": "node --experimental-strip-types --test columns.test.ts settings.test.ts capture.test.ts"
```

to:

```json
"test": "node --experimental-strip-types --test columns.test.ts settings.test.ts capture.test.ts flowWindow.test.ts"
```

- [ ] **Step 2: Run the test to verify it fails**

Run from `web/`:

```bash
npm test
```

Expected: FAIL. Node cannot find `./src/flowWindow.ts`.

- [ ] **Step 3: Write the minimal implementation**

Create `web/src/flowWindow.ts`:

```ts
export interface StripeBook {
  next: number
  bits: Map<string, 0 | 1>
}

export function createStripeBook(): StripeBook {
  return { next: 0, bits: new Map() }
}

export function resetStripes(book: StripeBook): void {
  book.next = 0
  book.bits.clear()
}

export function syncStripes(book: StripeBook, ids: readonly string[]): number[] {
  const out: number[] = []
  for (const id of ids) {
    let bit = book.bits.get(id)
    if (bit === undefined) {
      bit = (book.next % 2) as 0 | 1
      book.next += 1
      book.bits.set(id, bit)
    }
    out.push(bit)
  }
  return out
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run from `web/`:

```bash
npm test
```

Expected: PASS, including the existing column, settings, and capture tests.

- [ ] **Step 5: Commit**

```bash
git add web/src/flowWindow.ts web/flowWindow.test.ts web/package.json
git commit -m "$(cat <<'EOF'
feat: add the flow list stripe book

EOF
)"
```

Do not stage any other dirty path.

---

### Task 2: Visible window

**Files:**
- Modify: `web/src/flowWindow.ts`
- Modify: `web/flowWindow.test.ts`
- Test: `web/flowWindow.test.ts`

**Interfaces:**
- Consumes: nothing from Task 1's functions; same module
- Produces:
  - `ROW_HEIGHT = 22`
  - `HEADER_HEIGHT = 22`
  - `OVERSCAN = 10`
  - `STICK_PX = 24`
  - `interface WindowRange { start: number; end: number; top: number; bottom: number }`
  - `visibleRange(scrollTop: number, clientHeight: number, count: number): WindowRange`
  - `start` and `end` are a half-open interval. `top` and `bottom` are spacer pixels.

- [ ] **Step 1: Write the failing test**

Append this import to the existing import in `web/flowWindow.test.ts`:

```ts
import { createStripeBook, resetStripes, syncStripes, visibleRange } from './src/flowWindow.ts'
```

Append this suite:

```ts
describe('visibleRange', () => {
  it('windows the rows and pads the rest', () => {
    assert.deepEqual(visibleRange(0, 200, 100), { start: 0, end: 19, top: 0, bottom: 1782 })
    assert.deepEqual(visibleRange(500, 200, 100), { start: 12, end: 41, top: 264, bottom: 1298 })
    assert.deepEqual(visibleRange(2022, 200, 100), { start: 81, end: 100, top: 1782, bottom: 0 })
    assert.deepEqual(visibleRange(0, 200, 0), { start: 0, end: 0, top: 0, bottom: 0 })
    assert.deepEqual(visibleRange(0, 0, 100), { start: 0, end: 0, top: 0, bottom: 0 })
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run from `web/`:

```bash
npm test
```

Expected: FAIL. `visibleRange` is not exported.

- [ ] **Step 3: Write the minimal implementation**

Append to `web/src/flowWindow.ts`:

```ts
export const ROW_HEIGHT = 22
export const HEADER_HEIGHT = 22
export const OVERSCAN = 10
export const STICK_PX = 24

export interface WindowRange {
  start: number
  end: number
  top: number
  bottom: number
}

export function visibleRange(scrollTop: number, clientHeight: number, count: number): WindowRange {
  if (count <= 0 || clientHeight <= 0) return { start: 0, end: 0, top: 0, bottom: 0 }
  const start = Math.min(count, Math.max(0, Math.floor(scrollTop / ROW_HEIGHT) - OVERSCAN))
  let end = Math.ceil((scrollTop + clientHeight - HEADER_HEIGHT) / ROW_HEIGHT) + OVERSCAN
  end = Math.min(count, Math.max(start, end))
  return {
    start,
    end,
    top: start * ROW_HEIGHT,
    bottom: (count - end) * ROW_HEIGHT,
  }
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run from `web/`:

```bash
npm test
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/flowWindow.ts web/flowWindow.test.ts
git commit -m "$(cat <<'EOF'
feat: compute the flow list window

EOF
)"
```

---

### Task 3: Scroll anchoring

**Files:**
- Modify: `web/src/flowWindow.ts`
- Modify: `web/flowWindow.test.ts`
- Test: `web/flowWindow.test.ts`

**Interfaces:**
- Consumes: `HEADER_HEIGHT`, `ROW_HEIGHT`, `STICK_PX` from Task 2
- Produces:
  - `isStuck(scrollTop: number, scrollHeight: number, clientHeight: number): boolean`
  - `interface ScrollInput { stick: boolean; clientHeight: number; ids: readonly string[]; prevIds: readonly string[]; anchorId: string | null; anchorOffset: number }`
  - `interface ScrollTarget { scrollTop: number; anchorId: string | null; anchorOffset: number }`
  - `nextScrollTop(input: ScrollInput): ScrollTarget`

`nextScrollTop` clamps with `maxScroll = max(0, HEADER_HEIGHT + ids.length * ROW_HEIGHT - clientHeight)`. An id's index is its first occurrence. When the anchor id is gone, search the previous id list downward first, then upward. The replacement offset is `anchorOffset + (itsOldIndex - anchorOldIndex) * ROW_HEIGHT`. Stick, an empty id list, and `clientHeight <= 0` return `{ scrollTop: 0 or the pinned bottom, anchorId: null, anchorOffset: 0 }` as specified below. The caller does not clear a stored anchor just because this result has a null id.

- [ ] **Step 1: Write the failing test**

Change the import in `web/flowWindow.test.ts` to:

```ts
import {
  createStripeBook,
  isStuck,
  nextScrollTop,
  resetStripes,
  syncStripes,
  visibleRange,
} from './src/flowWindow.ts'
```

Append:

```ts
function seq(n: number): string[] {
  return Array.from({ length: n }, (_, i) => String(i))
}

describe('nextScrollTop', () => {
  it('pins to the bottom while following', () => {
    assert.deepEqual(
      nextScrollTop({ stick: true, clientHeight: 200, ids: seq(100), prevIds: [], anchorId: '10', anchorOffset: 22 }),
      { scrollTop: 2022, anchorId: null, anchorOffset: 0 },
    )
    assert.deepEqual(
      nextScrollTop({ stick: true, clientHeight: 200, ids: seq(1), prevIds: [], anchorId: null, anchorOffset: 0 }),
      { scrollTop: 0, anchorId: null, anchorOffset: 0 },
    )
  })

  it('returns zero without an anchor when the list or viewport is empty', () => {
    assert.deepEqual(
      nextScrollTop({ stick: true, clientHeight: 200, ids: [], prevIds: ['a'], anchorId: 'a', anchorOffset: 22 }),
      { scrollTop: 0, anchorId: null, anchorOffset: 0 },
    )
    assert.deepEqual(
      nextScrollTop({ stick: false, clientHeight: 200, ids: [], prevIds: ['a'], anchorId: 'a', anchorOffset: 22 }),
      { scrollTop: 0, anchorId: null, anchorOffset: 0 },
    )
    assert.deepEqual(
      nextScrollTop({ stick: true, clientHeight: 0, ids: seq(10), prevIds: [], anchorId: null, anchorOffset: 0 }),
      { scrollTop: 0, anchorId: null, anchorOffset: 0 },
    )
  })

  it('keeps the anchor row when a row above it is removed', () => {
    const prev = seq(20)
    assert.deepEqual(
      nextScrollTop({
        stick: false,
        clientHeight: 200,
        ids: prev.slice(1),
        prevIds: prev,
        anchorId: '10',
        anchorOffset: 22,
      }),
      { scrollTop: 198, anchorId: '10', anchorOffset: 22 },
    )
  })

  it('pins the next lower row when the anchor is gone', () => {
    const prev = seq(12)
    assert.deepEqual(
      nextScrollTop({
        stick: false,
        clientHeight: 200,
        ids: prev.filter((id) => id !== '10'),
        prevIds: prev,
        anchorId: '10',
        anchorOffset: 22,
      }),
      { scrollTop: 198, anchorId: '11', anchorOffset: 44 },
    )
  })

  it('pins the next upper row when nothing lower survives', () => {
    const prev = seq(11)
    assert.deepEqual(
      nextScrollTop({
        stick: false,
        clientHeight: 200,
        ids: prev.filter((id) => id !== '10'),
        prevIds: prev,
        anchorId: '10',
        anchorOffset: 22,
      }),
      { scrollTop: 220, anchorId: '9', anchorOffset: 0 },
    )
  })

  it('uses the first index when an id is repeated', () => {
    assert.deepEqual(
      nextScrollTop({
        stick: false,
        clientHeight: 200,
        ids: ['c', 'b', 'c'],
        prevIds: ['a', 'b', 'c'],
        anchorId: 'a',
        anchorOffset: 0,
      }),
      { scrollTop: 22, anchorId: 'b', anchorOffset: 22 },
    )
  })

  it('drops the anchor when it cannot be resolved', () => {
    assert.deepEqual(
      nextScrollTop({
        stick: false,
        clientHeight: 200,
        ids: seq(5),
        prevIds: seq(5),
        anchorId: 'missing',
        anchorOffset: 22,
      }),
      { scrollTop: 0, anchorId: null, anchorOffset: 0 },
    )
  })

  it('clamps scrollTop into the legal range', () => {
    assert.deepEqual(
      nextScrollTop({
        stick: false,
        clientHeight: 200,
        ids: seq(100),
        prevIds: seq(100),
        anchorId: '99',
        anchorOffset: 0,
      }),
      { scrollTop: 2022, anchorId: '99', anchorOffset: 0 },
    )
    assert.deepEqual(
      nextScrollTop({
        stick: false,
        clientHeight: 200,
        ids: seq(10),
        prevIds: seq(10),
        anchorId: '0',
        anchorOffset: 100,
      }),
      { scrollTop: 0, anchorId: '0', anchorOffset: 100 },
    )
  })
})

describe('isStuck', () => {
  it('treats a gap under 24 as stuck', () => {
    assert.equal(isStuck(777, 1000, 200), true)
    assert.equal(isStuck(776, 1000, 200), false)
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run from `web/`:

```bash
npm test
```

Expected: FAIL. `nextScrollTop` is not exported.

- [ ] **Step 3: Write the minimal implementation**

Append to `web/src/flowWindow.ts`:

```ts
export function isStuck(scrollTop: number, scrollHeight: number, clientHeight: number): boolean {
  return scrollHeight - scrollTop - clientHeight < STICK_PX
}

export interface ScrollInput {
  stick: boolean
  clientHeight: number
  ids: readonly string[]
  prevIds: readonly string[]
  anchorId: string | null
  anchorOffset: number
}

export interface ScrollTarget {
  scrollTop: number
  anchorId: string | null
  anchorOffset: number
}

function maxScroll(count: number, clientHeight: number): number {
  return Math.max(0, HEADER_HEIGHT + count * ROW_HEIGHT - clientHeight)
}

function clampScroll(value: number, count: number, clientHeight: number): number {
  return Math.min(maxScroll(count, clientHeight), Math.max(0, value))
}

function resolveAnchor(
  ids: readonly string[],
  prevIds: readonly string[],
  anchorId: string | null,
  anchorOffset: number,
): { id: string; index: number; offset: number } | null {
  if (!anchorId) return null
  const direct = ids.indexOf(anchorId)
  if (direct >= 0) return { id: anchorId, index: direct, offset: anchorOffset }
  const oldIndex = prevIds.indexOf(anchorId)
  if (oldIndex < 0) return null
  const found = (from: number, to: number, step: number) => {
    for (let j = from; step > 0 ? j < to : j > to; j += step) {
      const id = prevIds[j]
      const index = ids.indexOf(id)
      if (index >= 0) return { id, index, offset: anchorOffset + (j - oldIndex) * ROW_HEIGHT }
    }
    return null
  }
  return found(oldIndex + 1, prevIds.length, 1) ?? found(oldIndex - 1, -1, -1)
}

export function nextScrollTop(input: ScrollInput): ScrollTarget {
  const none: ScrollTarget = { scrollTop: 0, anchorId: null, anchorOffset: 0 }
  if (input.clientHeight <= 0 || input.ids.length === 0) return none
  if (input.stick) {
    return { scrollTop: maxScroll(input.ids.length, input.clientHeight), anchorId: null, anchorOffset: 0 }
  }
  const resolved = resolveAnchor(input.ids, input.prevIds, input.anchorId, input.anchorOffset)
  if (!resolved) return none
  return {
    scrollTop: clampScroll(
      HEADER_HEIGHT + resolved.index * ROW_HEIGHT - resolved.offset,
      input.ids.length,
      input.clientHeight,
    ),
    anchorId: resolved.id,
    anchorOffset: resolved.offset,
  }
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run from `web/`:

```bash
npm test
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/flowWindow.ts web/flowWindow.test.ts
git commit -m "$(cat <<'EOF'
feat: anchor the flow list scroll position

EOF
)"
```

---

### Task 4: Virtual table and stripe wiring

**Files:**
- Modify: `web/src/components/FlowTable.tsx`
- Modify: `web/src/styles.css` (replace the `nth-child(even)` rule)
- Modify: `web/src/App.tsx` (book, `listEpoch`, `FlowTable` props)
- Test: structural check below, then `npm test` and `npm run build` from `web/`

**Interfaces:**
- Consumes: `createStripeBook`, `resetStripes`, `syncStripes`, `HEADER_HEIGHT`, `ROW_HEIGHT`, `isStuck`, `nextScrollTop`, `visibleRange` from `web/src/flowWindow.ts`. `activeColumns`, `flowCell`, `tableMinWidth`, `ColumnDef` from `web/src/columns.ts`. `setSelectedId` from `App`, passed through as `onSelect` and omitted from the row memo comparison.
- Produces: `FlowTable` props `{ flows, stripes, selectedId, onSelect, visible }`. `App` holds `stripeBook` and `listEpoch`, remounts the table with `key={listEpoch}` on clear, and passes `stripes` aligned with `flows`.

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
if (!table.includes('useLayoutEffect')) fail('table must correct scroll before paint');
if (!table.includes('visibleRange(')) fail('table must window rows');
if (!table.includes('nextScrollTop(')) fail('table must anchor scroll');
if (!table.includes('isStuck(')) fail('table must use isStuck');
if (!table.includes('stripes[')) fail('table must read stripes');
if (!table.includes('?? 0')) fail('missing stripe paints as 0');
if (!table.includes('memo(')) fail('rows must be memoized');
if (!table.includes('ResizeObserver')) fail('table must watch viewport height');
if (!table.includes('freezeStick')) fail('programmatic scroll must not change stick');
if (table.includes('scrollLeft')) fail('table must not assign scrollLeft');
if (!table.includes('export function formatSize')) fail('formatSize moved');
if (!table.includes('export function formatTime')) fail('formatTime moved');
if (!table.includes('尚未捕获请求。将客户端指向该代理后即可开始记录。')) fail('empty copy changed');
if (!css.includes('.row-stripe')) fail('stripe class missing');
if (!css.includes('.row-pad')) fail('pad class missing');
if (css.includes('nth-child(even)')) fail('nth-child stripe remains');
if (!app.includes('syncStripes(')) fail('App must sync stripes');
if (!app.includes('resetStripes(')) fail('App must reset stripes on clear');
if (!app.includes('createStripeBook(')) fail('App must own the stripe book');
if (!app.includes('key={listEpoch}')) fail('clear must remount the table');
if (!app.includes('stripes={stripes}')) fail('App must pass stripes');
if (!app.includes('onSelect={setSelectedId}')) fail('onSelect must stay the stable setter');
if (!detail.includes(\"from './FlowTable'\")) fail('detail must keep importing formatSize');
console.log('virtual list ok');
"
```

Expected: exit non-zero. Output includes `table must correct scroll before paint`.

- [ ] **Step 2: Replace `FlowTable`**

Replace `web/src/components/FlowTable.tsx` with:

```tsx
import { memo, useLayoutEffect, useRef, useState, type CSSProperties } from 'react'
import { activeColumns, flowCell, tableMinWidth, type ColumnDef } from '../columns'
import { HEADER_HEIGHT, ROW_HEIGHT, isStuck, nextScrollTop, visibleRange } from '../flowWindow'
import type { FlowSummary } from '../types'

interface Props {
  flows: FlowSummary[]
  stripes: number[]
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

function sameColumnIds(a: ColumnDef[], b: ColumnDef[]): boolean {
  if (a.length !== b.length) return false
  for (let i = 0; i < a.length; i++) {
    if (a[i].id !== b[i].id) return false
  }
  return true
}

const FlowRow = memo(function FlowRow({
  flow,
  stripe,
  selected,
  columns,
  onSelect,
}: {
  flow: FlowSummary
  stripe: number
  selected: boolean
  columns: ColumnDef[]
  onSelect: (id: string) => void
}) {
  const status = flowCell('status', flow, formats)
  const stateClass = selected ? 'selected' : status.statusClass === 'st-err' ? 'row-err' : ''
  const className = [stripe === 1 ? 'row-stripe' : '', stateClass].filter(Boolean).join(' ')
  return (
    <tr className={className} onClick={() => onSelect(flow.id)}>
      {columns.map((c) => {
        const cell = c.id === 'status' ? status : flowCell(c.id, flow, formats)
        const statusCls = c.id === 'status' && cell.statusClass ? ` ${cell.statusClass}` : ''
        return (
          <td key={c.id} className={statusCls.trim()} style={colStyle(c)} title={cell.title}>
            {c.id === 'name' ? <span className="name-path">{cell.text}</span> : cell.text}
          </td>
        )
      })}
    </tr>
  )
}, (prev, next) =>
  prev.flow === next.flow &&
  prev.stripe === next.stripe &&
  prev.selected === next.selected &&
  sameColumnIds(prev.columns, next.columns),
)

function PadRow({ height, cols }: { height: number; cols: number }) {
  if (height <= 0) return null
  return (
    <tr className="row-pad" aria-hidden="true">
      <td colSpan={cols} style={{ height }} />
    </tr>
  )
}

export default function FlowTable({ flows, stripes, selectedId, onSelect, visible }: Props) {
  const bodyRef = useRef<HTMLDivElement>(null)
  const stick = useRef(true)
  const freezeStick = useRef(false)
  const anchor = useRef<{ id: string; offset: number } | null>(null)
  const prevIds = useRef<string[]>([])
  const seenHeight = useRef(0)
  const seenFlows = useRef(flows)
  const flowsNow = useRef(flows)
  const [clientHeight, setClientHeight] = useState(0)
  const [scrollTop, setScrollTop] = useState(0)
  const cols = activeColumns(visible)
  flowsNow.current = flows
  if (seenFlows.current !== flows) {
    seenFlows.current = flows
    freezeStick.current = true
  }

  const onScroll = () => {
    const el = bodyRef.current
    if (!el) return
    setScrollTop(el.scrollTop)
    if (freezeStick.current) return
    if (el.clientHeight !== seenHeight.current) {
      seenHeight.current = el.clientHeight
      return
    }
    stick.current = isStuck(el.scrollTop, el.scrollHeight, el.clientHeight)
    const list = flowsNow.current
    if (list.length === 0) return
    const index = Math.min(list.length - 1, Math.max(0, Math.floor(el.scrollTop / ROW_HEIGHT)))
    anchor.current = {
      id: list[index].id,
      offset: HEADER_HEIGHT + index * ROW_HEIGHT - el.scrollTop,
    }
  }

  useLayoutEffect(() => {
    const el = bodyRef.current
    if (!el) return
    const observer = new ResizeObserver(() => {
      const height = el.clientHeight
      setClientHeight((prev) => {
        if (prev === height) return prev
        freezeStick.current = true
        return height
      })
    })
    observer.observe(el)
    return () => observer.disconnect()
  }, [])

  useLayoutEffect(() => {
    const el = bodyRef.current
    if (!el) return
    const height = el.clientHeight
    if (height <= 0) {
      freezeStick.current = false
      return
    }
    if (height !== clientHeight) setClientHeight(height)
    freezeStick.current = true
    const ids = flows.map((flow) => flow.id)
    const next = nextScrollTop({
      stick: stick.current,
      clientHeight: height,
      ids,
      prevIds: prevIds.current,
      anchorId: anchor.current?.id ?? null,
      anchorOffset: anchor.current?.offset ?? 0,
    })
    if (el.scrollTop !== next.scrollTop) el.scrollTop = next.scrollTop
    if (!stick.current && ids.length > 0 && next.anchorId) {
      anchor.current = { id: next.anchorId, offset: next.anchorOffset }
    }
    if (ids.length > 0) prevIds.current = ids
    seenHeight.current = height
    setScrollTop(el.scrollTop)
    freezeStick.current = false
  }, [flows, clientHeight])

  const range = visibleRange(scrollTop, clientHeight, flows.length)

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
          {flows.length === 0 ? (
            <tr className="empty">
              <td colSpan={cols.length}>尚未捕获请求。将客户端指向该代理后即可开始记录。</td>
            </tr>
          ) : (
            <>
              <PadRow height={range.top} cols={cols.length} />
              {flows.slice(range.start, range.end).map((flow, offset) => {
                const index = range.start + offset
                return (
                  <FlowRow
                    key={flow.id}
                    flow={flow}
                    stripe={stripes[index] ?? 0}
                    selected={flow.id === selectedId}
                    columns={cols}
                    onSelect={onSelect}
                  />
                )
              })}
              <PadRow height={range.bottom} cols={cols.length} />
            </>
          )}
        </tbody>
      </table>
    </div>
  )
}
```

This writes only `scrollTop`. A user scroll updates the stuck flag only when `freezeStick` is false and the viewport height is unchanged. A flow update sets `freezeStick` during render, before the browser clamps the scroll. A height change sets it inside the resize observer. The layout effect clears it after writing `scrollTop`. An empty non-clear list sets `scrollTop` to 0 and leaves `anchor` and `prevIds` as they were. Clearing remounts the component, so those refs start over. The first mount starts stuck. Opening settings unmounts the capture view; coming back mounts a new table, which starts stuck, while the stripe book stays in `App`.

- [ ] **Step 3: Replace the zebra rule**

In `web/src/styles.css`, replace:

```css
.flow-table tbody tr:nth-child(even) { background: var(--bg-subtle); }
```

with:

```css
.flow-table tbody tr.row-stripe { background: var(--bg-subtle); }
.flow-table tbody tr.row-pad { pointer-events: none; }
.flow-table tbody tr.row-pad td {
  height: auto;
  padding: 0;
  border: 0;
  line-height: 0;
}
```

Leave the existing `tr:hover` and `tr.selected` rules after this block.

- [ ] **Step 4: Wire the book in `App`**

Add this import beside the other `./` imports in `web/src/App.tsx`:

```tsx
import { createStripeBook, resetStripes, syncStripes } from './flowWindow'
```

Immediately after `const [flows, setFlows] = useState<FlowSummary[]>([])`, add:

```tsx
  const stripeBook = useRef(createStripeBook())
  const [listEpoch, setListEpoch] = useState(0)
```

Immediately after `maxFlowsRef.current = maxFlows`, add:

```tsx
  const stripes = syncStripes(stripeBook.current, flows.map((flow) => flow.id))
  const clearCaptured = () => {
    resetStripes(stripeBook.current)
    setListEpoch((epoch) => epoch + 1)
    setFlows([])
    setSelectedId(null)
  }
```

In the websocket handler, replace:

```tsx
        if (ev.type === 'flows.cleared') {
          setFlows([])
          setSelectedId(null)
          return
        }
```

with:

```tsx
        if (ev.type === 'flows.cleared') {
          clearCaptured()
          return
        }
```

Replace `onClear` with:

```tsx
  const onClear = async () => {
    await clearFlows()
    clearCaptured()
  }
```

Replace the `FlowTable` element with:

```tsx
                <FlowTable
                  key={listEpoch}
                  flows={flows}
                  stripes={stripes}
                  selectedId={selectedId}
                  onSelect={setSelectedId}
                  visible={visible}
                />
```

Do not change the eviction handler, filter refetch, or detail loading. `clearCaptured` closes over stable setters and a ref, so the websocket effect can keep its existing `[refetch]` dependency.

- [ ] **Step 5: Run the structural check**

Run the Step 1 command again from the repo root.

Expected: prints `virtual list ok` and exits 0.

- [ ] **Step 6: Run tests and the production build**

Run from `web/`:

```bash
npm test
npm run build
```

Expected: both PASS. `tsc -b` typechecks `web/src/flowWindow.ts` and the new `FlowTable` props.

- [ ] **Step 7: Manual check**

Start the proxy if it is not already listening, then Vite:

```bash
go run ./cmd/httpproxy
```

```bash
cd web && npm run dev
```

Open the Vite URL. Point a client at the proxy and confirm:

1. While the list is at the bottom, new requests keep the newest row at the bottom. Stripe colors do not flip, and the list does not draw one frame and then jump.
2. After scrolling up, dropping the oldest row and appending a new one leaves the row you are reading in place.
3. Scrolling back to within 24px of the bottom follows the newest row again.
4. One row changing from `(等待)` to a status code does not repaint the rows beside it.
5. Clear, then capture again. The new batch starts alternating at the first row and follows the bottom.
6. The header stays sticky. Horizontal scroll, the column menu, selection, and the detail pane still work.

`(等待)` is the existing status-cell text. A row that is not selected and has an error still uses the danger text color.

- [ ] **Step 8: Commit**

```bash
git add web/src/components/FlowTable.tsx web/src/styles.css
git diff -- web/src/App.tsx
```

If that diff contains only the virtual-list wiring from Step 4, also stage and commit it:

```bash
git add web/src/App.tsx
git commit -m "$(cat <<'EOF'
feat: virtualize the capture flow list

EOF
)"
```

If `web/src/App.tsx` still has unrelated hunks, commit only `FlowTable.tsx` and `styles.css`, leave `App.tsx` unstaged, and report that. Do not stage the other dirty paths listed in Global Constraints.
