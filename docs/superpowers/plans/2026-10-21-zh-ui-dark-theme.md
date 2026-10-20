# Chinese UI and Dark Theme Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show the capture UI in fixed Simplified Chinese, with a GitHub dark theme by default and a top-bar toggle that remembers light or dark.

**Architecture:** CSS variables on `:root` are the dark palette. `[data-theme="light"]` overrides them with the existing light palette. A blocking script in `index.html` `<head>` sets `data-theme` from `localStorage` before the module loads. The top bar toggles the attribute and writes the same key. Chinese strings live in the component JSX. No i18n library.

**Tech Stack:** React 19, TypeScript, Vite, one hand-written `styles.css`. No test runner. Automated check is `npm run build` plus `node` assertions over source files.

**Spec:** `docs/superpowers/specs/2026-10-20-zh-ui-dark-theme-design.md`

## Global Constraints

- Web UI only. Do not change Go, README, or the CLI.
- Simplified Chinese only. No language switch, no i18n library, no separate copy file.
- Do not translate HTTP methods, protocol versions, URLs, header names, status-code digits, or server `error` strings.
- Keep units `B` / `kB` / `MB` and `ms` / `s`.
- Do not follow the operating-system theme.
- Storage key is `httpproxy.theme`. Legal values are only `dark` and `light`. Missing, illegal, or thrown storage access means `dark`.
- `:root` is dark. `[data-theme="light"]` is light.
- Theme script is synchronous in `index.html` `<head>`, before the module script. `main.tsx` does not read the theme.
- Toggle sits immediately before 「下载 CA」. Dark mode label is 「浅色」. Light mode label is 「黑色」. Update `data-theme` first; storage write failure leaves the stored value unchanged.
- `lang` is `zh-CN`. Document title stays `httpproxy`.
- Selected row uses `--selected-bg`, text `#ffffff`, host `rgba(255, 255, 255, 0.8)`.
- Column widths stay 80 / 72 / 72 / 80 / 80.
- `--danger-bg` is defined in both themes and is not referenced by a rule.

## File Structure

| Path | Responsibility |
| --- | --- |
| `web/src/styles.css` | Dark `:root`, light override, tokenized row line, selected row, truncation notice |
| `web/index.html` | `lang="zh-CN"` and the blocking theme script |
| `web/src/components/TopBar.tsx` | Chinese top-bar copy and the theme button |
| `web/src/components/FilterBar.tsx` | Chinese filter copy |
| `web/src/components/FlowTable.tsx` | Chinese table headers, status words, empty state |
| `web/src/components/FlowDetail.tsx` | Chinese tabs, fields, body messages |

`web/src/main.tsx`, `web/src/App.tsx`, and `web/src/types.ts` stay unchanged.

---

### Task 1: Dark default palette

**Files:**
- Modify: `web/src/styles.css`

**Interfaces:**
- Consumes: nothing
- Produces: custom properties `--bg`, `--bg-subtle`, `--bg-hover`, `--border`, `--row-line`, `--text`, `--text-muted`, `--accent`, `--accent-bg`, `--selected-bg`, `--danger`, `--danger-bg`, `--warn`, `--ok`, `--notice-bg`, `--notice-border`. `:root` holds the dark values. `[data-theme="light"]` holds the light values. `.flow-table tbody tr.selected` uses `--selected-bg`. `.flow-table td` right border uses `--row-line`. `.notice` uses `--notice-bg` and `--notice-border`.

- [ ] **Step 1: Confirm the current stylesheet is still light-only**

Run from the repo root:

```bash
node --input-type=module -e "
import { readFileSync } from 'node:fs';
const css = readFileSync('web/src/styles.css', 'utf8');
const fail = (m) => { console.error(m); process.exit(1); };
if (css.includes('[data-theme=\"light\"]')) fail('light override already exists');
if (!css.includes('--bg: #ffffff')) fail('expected light --bg');
if (!css.includes('border-right: 1px solid #eaeef2')) fail('expected hardcoded row line');
if (!css.includes('background: var(--accent)')) fail('expected selected row on --accent');
if (!css.includes('background: #fff8c5')) fail('expected hardcoded notice');
console.log('baseline ok');
"
```

Expected: prints `baseline ok` and exits 0. If it fails, the file drifted; re-read `web/src/styles.css` before editing.

- [ ] **Step 2: Replace the `:root` block and add the light override**

Replace the existing `:root { ... }` block with both blocks below. Leave `--mono`, `--sans`, and `--row-h` on `:root` only.

```css
:root {
  --bg: #0d1117;
  --bg-subtle: #161b22;
  --bg-hover: #21262d;
  --border: #30363d;
  --row-line: #21262d;
  --text: #e6edf3;
  --text-muted: #8b949e;
  --accent: #58a6ff;
  --accent-bg: #1f3a5f;
  --selected-bg: #1f6feb;
  --danger: #f85149;
  --danger-bg: #490202;
  --warn: #d29922;
  --ok: #3fb950;
  --notice-bg: #2d2305;
  --notice-border: #9e6a03;
  --mono: ui-monospace, SFMono-Regular, 'SF Mono', Menlo, Consolas, 'Liberation Mono', monospace;
  --sans: -apple-system, BlinkMacSystemFont, 'Segoe UI', 'Noto Sans', Helvetica, Arial, sans-serif;
  --row-h: 22px;
}

[data-theme="light"] {
  --bg: #ffffff;
  --bg-subtle: #f6f8fa;
  --bg-hover: #eaeef2;
  --border: #d0d7de;
  --row-line: #eaeef2;
  --text: #1f2328;
  --text-muted: #656d76;
  --accent: #0969da;
  --accent-bg: #ddf4ff;
  --selected-bg: #0969da;
  --danger: #cf222e;
  --danger-bg: #ffebe9;
  --warn: #9a6700;
  --ok: #1a7f37;
  --notice-bg: #fff8c5;
  --notice-border: #eac54f;
}
```

- [ ] **Step 3: Point the three hardcoded rules at tokens**

In `.flow-table td`, replace `border-right: 1px solid #eaeef2` with `border-right: 1px solid var(--row-line)`.

Replace the selected-row rules with:

```css
.flow-table tbody tr.selected { background: var(--selected-bg); color: #ffffff; }
.flow-table tbody tr.selected td { color: #ffffff; }
```

Replace `.notice` with:

```css
.notice { padding: 4px 12px; color: var(--warn); background: var(--notice-bg); border-bottom: 1px solid var(--notice-border); }
```

Do not change column widths, fonts, or layout rules. Do not add a rule that uses `--danger-bg` or `--ok`.

- [ ] **Step 4: Assert the tokens**

Run:

```bash
node --input-type=module -e "
import { readFileSync } from 'node:fs';
const css = readFileSync('web/src/styles.css', 'utf8');
const fail = (m) => { console.error(m); process.exit(1); };
const lightAt = css.indexOf('[data-theme=\"light\"]');
if (lightAt < 0) fail('missing light override');
const root = css.slice(0, lightAt);
const dark = {
  '--bg': '#0d1117', '--bg-subtle': '#161b22', '--bg-hover': '#21262d',
  '--border': '#30363d', '--row-line': '#21262d', '--text': '#e6edf3',
  '--text-muted': '#8b949e', '--accent': '#58a6ff', '--accent-bg': '#1f3a5f',
  '--selected-bg': '#1f6feb', '--danger': '#f85149', '--danger-bg': '#490202',
  '--warn': '#d29922', '--ok': '#3fb950', '--notice-bg': '#2d2305',
  '--notice-border': '#9e6a03',
};
const light = {
  '--bg': '#ffffff', '--bg-subtle': '#f6f8fa', '--bg-hover': '#eaeef2',
  '--border': '#d0d7de', '--row-line': '#eaeef2', '--text': '#1f2328',
  '--text-muted': '#656d76', '--accent': '#0969da', '--accent-bg': '#ddf4ff',
  '--selected-bg': '#0969da', '--danger': '#cf222e', '--danger-bg': '#ffebe9',
  '--warn': '#9a6700', '--ok': '#1a7f37', '--notice-bg': '#fff8c5',
  '--notice-border': '#eac54f',
};
for (const [name, value] of Object.entries(dark)) {
  if (!root.includes(name + ': ' + value)) fail('dark ' + name);
}
const lightCss = css.slice(lightAt);
for (const [name, value] of Object.entries(light)) {
  if (!lightCss.includes(name + ': ' + value)) fail('light ' + name);
}
if (css.includes('border-right: 1px solid #eaeef2')) fail('hardcoded row line');
if (!css.includes('border-right: 1px solid var(--row-line)')) fail('row line token');
if (!css.includes('background: var(--selected-bg)')) fail('selected token');
if (!css.includes('color: #ffffff')) fail('selected text');
if (!css.includes('rgba(255, 255, 255, 0.8)')) fail('selected host');
if (css.includes('tr.selected { background: var(--accent)')) fail('selected row still uses --accent');
if (!css.includes('background: var(--notice-bg)')) fail('notice bg');
if (!css.includes('border-bottom: 1px solid var(--notice-border)')) fail('notice border');
if (css.includes('var(--danger-bg)') || css.includes('var(--ok)')) fail('unused token referenced');
if (css.includes('#fff8c5') && !lightCss.includes('--notice-bg: #fff8c5')) fail('notice color escaped the token');
console.log('tokens ok');
"
```

Expected: prints `tokens ok` and exits 0.

- [ ] **Step 5: Build**

Run: `npm run build` in `web/`

Expected: exit 0. `tsc -b` and `vite build` both succeed.

- [ ] **Step 6: Commit**

```bash
git add web/src/styles.css
git commit -m "$(cat <<'EOF'
style: default the capture UI to the GitHub dark palette

Keep the previous light colors behind an explicit light theme so the page no longer paints white first.
EOF
)"
```

---

### Task 2: Persist the theme and add the toggle

**Files:**
- Modify: `web/index.html`
- Modify: `web/src/components/TopBar.tsx`
- Do not modify: `web/src/main.tsx`

**Interfaces:**
- Consumes: the CSS variables from Task 1. No JavaScript import.
- Produces: `document.documentElement` attribute `data-theme` with value `dark` or `light`, and `localStorage` key `httpproxy.theme` with the same two legal values. Top bar button calls `setAttribute` before `setItem`.

- [ ] **Step 1: Confirm the theme script and toggle are absent**

Run from the repo root:

```bash
node --input-type=module -e "
import { readFileSync } from 'node:fs';
const html = readFileSync('web/index.html', 'utf8');
const top = readFileSync('web/src/components/TopBar.tsx', 'utf8');
const fail = (m) => { console.error(m); process.exit(1); };
if (html.includes('httpproxy.theme')) fail('script already present');
if (html.includes('lang=\"zh-CN\"')) fail('lang already zh-CN');
if (top.includes('浅色')) fail('toggle already present');
console.log('baseline ok');
"
```

Expected: prints `baseline ok` and exits 0.

- [ ] **Step 2: Write `web/index.html`**

```html
<!doctype html>
<html lang="zh-CN">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>httpproxy</title>
    <script>
      (function () {
        var theme = 'dark'
        try {
          var stored = localStorage.getItem('httpproxy.theme')
          if (stored === 'dark' || stored === 'light') theme = stored
        } catch (e) {}
        document.documentElement.setAttribute('data-theme', theme)
      })()
    </script>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

- [ ] **Step 3: Replace `web/src/components/TopBar.tsx`**

```tsx
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
```

The theme button is the sibling immediately before the download link. Do not edit `main.tsx`.

- [ ] **Step 4: Assert the script and top bar**

Run:

```bash
node --input-type=module -e "
import { readFileSync } from 'node:fs';
const html = readFileSync('web/index.html', 'utf8');
const top = readFileSync('web/src/components/TopBar.tsx', 'utf8');
const main = readFileSync('web/src/main.tsx', 'utf8');
const fail = (m) => { console.error(m); process.exit(1); };
if (!html.includes('lang=\"zh-CN\"')) fail('lang');
if (!html.includes('<title>httpproxy</title>')) fail('title');
const scriptAt = html.indexOf('httpproxy.theme');
const moduleAt = html.indexOf('type=\"module\"');
if (scriptAt < 0 || moduleAt < 0 || scriptAt > moduleAt) fail('script must precede the module');
if (!html.includes(\"var theme = 'dark'\")) fail('default dark');
if (!html.includes(\"stored === 'dark' || stored === 'light'\")) fail('legal values');
if (!html.includes(\"setAttribute('data-theme', theme)\")) fail('attribute');
if (main.includes('httpproxy.theme') || main.includes('data-theme')) fail('main.tsx must not read theme');
for (const s of ['将客户端的 HTTP(S) 代理指向此地址', '代理 ', '{count} 条请求', '下载 CA', '清空', \"theme === 'dark' ? '浅色' : '黑色'\"]) {
  if (!top.includes(s)) fail('missing ' + s);
}
const downloadAt = top.indexOf('下载 CA');
const lightAt = top.indexOf('浅色');
if (lightAt < 0 || downloadAt < 0 || lightAt > downloadAt) fail('toggle must be before 下载 CA');
if (!top.includes(\"setAttribute('data-theme', theme)\")) fail('apply attribute');
const attrAt = top.indexOf(\"setAttribute('data-theme', theme)\");
const storeAt = top.indexOf(\"setItem('httpproxy.theme', theme)\");
if (attrAt < 0 || storeAt < 0 || attrAt > storeAt) fail('attribute before storage');
console.log('theme ok');
"
```

Expected: prints `theme ok` and exits 0.

- [ ] **Step 5: Build**

Run: `npm run build` in `web/`

Expected: exit 0.

- [ ] **Step 6: Commit**

```bash
git add web/index.html web/src/components/TopBar.tsx
git commit -m "$(cat <<'EOF'
feat: remember the capture UI theme from the top bar

Set data-theme before the module loads so a saved light theme does not flash dark.
EOF
)"
```

---

### Task 3: Simplified Chinese for the rest of the UI

**Files:**
- Modify: `web/src/components/FilterBar.tsx`
- Modify: `web/src/components/FlowTable.tsx`
- Modify: `web/src/components/FlowDetail.tsx`

**Interfaces:**
- Consumes: no new exports. `formatSize` and `formatTime` keep their current signatures: `(n: number) => string` and `(ms: number) => string`.
- Produces: no new exports. Tab ids stay `'headers' | 'request' | 'response'`. Visible labels change.

- [ ] **Step 1: Confirm the English UI strings are still present**

Run from the repo root:

```bash
node --input-type=module -e "
import { readFileSync } from 'node:fs';
const files = [
  'web/src/components/FilterBar.tsx',
  'web/src/components/FlowTable.tsx',
  'web/src/components/FlowDetail.tsx',
].map((p) => readFileSync(p, 'utf8')).join('\n');
const fail = (m) => { console.error(m); process.exit(1); };
for (const s of ['Filter host', 'Filter path', 'placeholder=\"Status\"', '>All<', '>Name<', '(failed)', '(pending)', 'No requests captured', 'Select a request', 'Request URL:', 'Body not captured', 'Body truncated']) {
  if (!files.includes(s)) fail('missing baseline ' + s);
}
console.log('baseline ok');
"
```

Expected: prints `baseline ok` and exits 0.

- [ ] **Step 2: Replace the visible strings in `FilterBar.tsx`**

Set the host placeholder to `筛选主机`, the path placeholder to `筛选路径`, the status placeholder to `状态码`, and the status input title to:

```text
1 位表示类别（2 → 2xx），2 位表示前缀（40 → 40x），3 位表示精确匹配
```

Replace the `All` button label with `全部`. Leave the `2xx`–`5xx` chip labels as `` `${s}xx` ``.

- [ ] **Step 3: Replace the visible strings in `FlowTable.tsx`**

Keep `formatSize` and `formatTime` byte-for-byte, including `B`, `kB`, `MB`, `ms`, and `s`.

In `statusCell`, use `(失败)` for `f.error` and `(等待)` when there is no status code.

Replace the header row with:

```tsx
<th className="col-name">名称</th>
<th className="col-status">状态</th>
<th className="col-method">方法</th>
<th className="col-version">版本</th>
<th className="col-size">大小</th>
<th className="col-time">耗时</th>
```

Replace the empty-state cell text with:

```text
尚未捕获请求。将客户端指向该代理后即可开始记录。
```

Do not change `col-status` through `col-time` widths.

- [ ] **Step 4: Replace the visible strings in `FlowDetail.tsx`**

Add this map next to the `Tab` type and render `TAB_LABEL[t]` instead of `t[0].toUpperCase() + t.slice(1)`:

```tsx
const TAB_LABEL: Record<Tab, string> = {
  headers: '标头',
  request: '请求',
  response: '响应',
}
```

Empty selection: `选择一条请求以查看详情。`

Loading: `加载中…`

General heading, exactly:

```tsx
<h4>常规</h4>
```

Field names, including the fullwidth colon:

```tsx
<div className="hdr-row"><dt>请求 URL：</dt><dd>{`${flow.scheme}://${flow.host}${flow.path}`}</dd></div>
<div className="hdr-row"><dt>请求方法：</dt><dd>{flow.method}</dd></div>
<div className="hdr-row">
  <dt>状态码：</dt>
  <dd>{flow.status_code || (flow.error ? '(失败)' : '(等待)')}</dd>
</div>
<div className="hdr-row"><dt>协议：</dt><dd>{flow.http_version}</dd></div>
<div className="hdr-row"><dt>远程地址：</dt><dd>{flow.client_addr}</dd></div>
```

Error field name: `错误：`. The `flow.error` value stays as returned by the server.

Header groups: `响应标头` and `请求标头`.

`Body`: when `size > 0` and there is no text, `未捕获正文。`. Otherwise `(无正文)`.

Truncation notice, with `formatSize(size)` in place of the size placeholder:

```tsx
正文已截断——仅保存有限预览；原始传输大小 {formatSize(size)}。
```

Do not change `decodeBase64` or `prettyBody`.

- [ ] **Step 5: Assert every approved string and the untouched data**

Run from the repo root. The heredoc keeps the template literals literal.

```bash
cat > /tmp/check-zh-ui.mjs << 'EOF'
import { readFileSync } from 'node:fs'
const filter = readFileSync('web/src/components/FilterBar.tsx', 'utf8')
const table = readFileSync('web/src/components/FlowTable.tsx', 'utf8')
const detail = readFileSync('web/src/components/FlowDetail.tsx', 'utf8')
const css = readFileSync('web/src/styles.css', 'utf8')
const all = filter + table + detail
const fail = (m) => { console.error(m); process.exit(1) }
const want = [
  '筛选主机', '筛选路径', '状态码',
  '1 位表示类别（2 → 2xx），2 位表示前缀（40 → 40x），3 位表示精确匹配',
  '全部', '>名称<', '>状态<', '>方法<', '>版本<', '>大小<', '>耗时<',
  '(失败)', '(等待)',
  '尚未捕获请求。将客户端指向该代理后即可开始记录。',
  "headers: '标头'", "request: '请求'", "response: '响应'",
  '选择一条请求以查看详情。', '加载中…', '>常规<',
  '请求 URL：', '请求方法：', '状态码：', '协议：', '远程地址：', '错误：',
  '响应标头', '请求标头', '(无正文)', '未捕获正文。',
  '正文已截断——仅保存有限预览；原始传输大小 {formatSize(size)}。',
]
for (const s of want) if (!all.includes(s)) fail('missing ' + s)
for (const s of ['Filter host', 'Filter path', '>All<', '>Name<', '(failed)', '(pending)', 'No requests captured', 'Loading…', 'Select a request', 'Request URL:', 'Request Method:', 'Status Code:', 'Remote Address:', 'Response Headers', 'Request Headers', 'Body not captured', '(no body)', 'Body truncated', '>General<']) {
  if (all.includes(s)) fail('english remains: ' + s)
}
const sizeFn = table.slice(table.indexOf('function formatSize'), table.indexOf('function formatTime'))
const timeFn = table.slice(table.indexOf('function formatTime'), table.indexOf('function statusCell'))
if (!sizeFn.includes(' B') || !sizeFn.includes('kB') || !sizeFn.includes('MB')) fail('size units')
if (!timeFn.includes(' ms') || !timeFn.includes(' s')) fail('time units')
for (const rule of ['.col-status { width: 80px', '.col-method { width: 72px', '.col-version { width: 72px', '.col-size { width: 80px', '.col-time { width: 80px']) {
  if (!css.includes(rule)) fail(rule)
}
if (!detail.includes('flow.error')) fail('server error must still render')
if (!detail.includes('flow.method') || !detail.includes('flow.http_version')) fail('protocol fields')
console.log('copy ok')
EOF
node /tmp/check-zh-ui.mjs
rm -f /tmp/check-zh-ui.mjs
```

Expected: prints `copy ok` and exits 0. `/tmp/check-zh-ui.mjs` is gone before the commit.

- [ ] **Step 6: Build**

Run: `npm run build` in `web/`

Expected: exit 0.

- [ ] **Step 7: Check the UI in a browser**

Start the UI with `npm run dev` in `web/` and open the printed local URL. `make dev-web` from the repo root is the same server.

1. Delete `httpproxy.theme` in the page's local storage and reload. The page is dark. Top bar, filters, table headers, and the detail placeholder are the Chinese strings from the spec. The title remains `httpproxy`.
2. Click 「浅色」 and reload. The page stays light. The button reads 「黑色」.
3. Click 「黑色」 and reload. The page stays dark. The button reads 「浅色」.
4. Set `httpproxy.theme` to `nope` and reload. The page is dark.
5. Captured method, URL, header names, and status digits stay in their original form. `(失败)` and `(等待)` appear only for the UI status words. With no captured traffic, the empty cell reads `尚未捕获请求。将客户端指向该代理后即可开始记录。`

- [ ] **Step 8: Commit**

```bash
git add web/src/components/FilterBar.tsx web/src/components/FlowTable.tsx web/src/components/FlowDetail.tsx
git commit -m "$(cat <<'EOF'
feat: show the capture UI in simplified Chinese

Leave protocol fields, status digits, and size units in their original form.
EOF
)"
```
