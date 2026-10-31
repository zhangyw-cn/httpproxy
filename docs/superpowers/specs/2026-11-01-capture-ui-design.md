# 抓包界面优化设计规格

**日期：** 2026-11-01  
**状态：** 待审阅  
**范围：** 网页抓包页的顶栏开关、规则状态、详情方位，以及标头、请求、响应的展示。不改 Go 后端、REST、WebSocket、配置字段或 README。

这份规格替换 2026-10-28 抓包策略规格里的两处界面描述：顶栏文字按钮，以及过滤条下面的一行只读摘要。策略的含义、保存方式和默认值保持那份规格。

## 1. 目标

用户在抓包页上完成三件事：

- 用一个图标立即开始或暂停抓包。悬停说明即将执行的动作。
- 一眼看出抓包规则是否偏离默认。悬停看到空规则、未抓 HTTPS 和全部主机。
- 把详情放在列表下方或列表右侧，并在现有三个页签里看清查询参数、状态颜色，以及美化或原始的正文。

成功标准：

- 正在抓包时，顶栏图标是暂停，悬停和 `aria-label` 都是「暂停抓包」。已暂停时，图标是开始，文案是「开始抓包」。配置尚未返回或正在写入时，按钮不可点。
- 主机非空、空规则为「不抓」，或未抓 HTTPS 为「隧道」时，规则图标用强调色。三项都是默认值，以及抓包已暂停时，图标为灰色。悬停或聚焦时列出三项的当前值。
- 详情可在下方和右侧之间切换。选择写入 `localStorage`，刷新后恢复。默认在下方。
- 标头页单独列出查询参数。状态码按与列表相同的分类着色。每个页签可以复制当前可见内容。请求和响应可以在「美化 / 原始」之间切换。

## 2. 非目标

- 不把规则编辑从设置页拆出去，不增加活动栏图标。
- 不把详情停靠到左侧，不弹出独立窗口，不做可拖拽分割条。
- 窄窗口不自动改回下方。
- 不加 JSON 树、图片预览、Cookie、计时或「复制为 cURL」。
- 不把美化或原始的选择写入 `localStorage`。
- 不改 `PUT /api/capture`、`PUT /api/config` 和 `config.yaml` 的字段。
- 不新增运行时依赖，不新增浏览器自动化测试，不改 Go 测试。
- 不改 README。它没有描述文字按钮或那行摘要。

## 3. 布局

页面骨架不变：顶栏、左侧活动栏，右侧是抓包页或设置页。

顶栏从左到右：名称、代理地址、空白、请求条数、抓包图标、下载 CA、清空。下载 CA 和清空仍是文字按钮。

抓包页过滤条右侧、列选择左边，放规则图标。过滤条下面的 `.capture-summary` 删除，对应样式一并删除。设置页不显示规则图标。

工作区两种排列：

```text
下方（默认）                 右侧
┌──────────────────┐      ┌────────────┬─────────┐
│ 请求列表          │      │ 请求列表    │ 详情     │
├──────────────────┤      │            │ 标头     │
│ 详情              │      │            │ 请求     │
│ 标头 | 请求 | 响应 │      │            │ 响应     │
└──────────────────┘      └────────────┴─────────┘
```

两种排列都是列表约 55%、详情约 45%。分界不能拖。列表容器的类名改为 `pane pane-list`，详情容器改为 `pane pane-detail`，不再使用 `pane-top` / `pane-bottom`。下方时工作区为纵向，列表底边有分隔线。右侧时工作区为横向，列表右边有分隔线。两个窗格都保持 `min-width: 0` 和 `min-height: 0`。

## 4. 抓包开关

顶栏只留一个抓包按钮，沿用 `.btn`，宽高 24px，内边距为 0。里面是 16px、`currentColor` 的实心图标。正在抓包时画两条竖线。已暂停时画三角形。

`capture == null` 或 `writeLocked` 时按钮禁用。`capture == null` 时仍显示开始图标，`aria-label` 和 `title` 都是「开始抓包」。已载入且 `capture` 为 true 时，两者都是「暂停抓包」。为 false 时，两者都是「开始抓包」。

点击仍调用现有的 `PUT /api/capture`，写入取反后的 `capture`。失败时按钮停在原状态，旁边继续显示现在的 `.topbar-error`。下一次成功后清掉错误。

## 5. 规则图标

新建 `web/src/components/RulesMark.tsx`，只在抓包页渲染。它是 24×24 的按钮，`type="button"`，没有边框，背景透明。`aria-label` 是「抓包规则」。图标是 16px 线条画的三条横线滑块。点击不导航，也不打开设置。

颜色只看三条策略。`capture` 为 false 不改变颜色。

| 条件 | 颜色 |
|---|---|
| 配置尚未返回，或读取失败 | 灰色 `--text-muted` |
| 主机为空，且空规则为 `all`，且未抓 HTTPS 为 `decrypt` | 灰色 `--text-muted` |
| 主机非空，或空规则为 `none`，或未抓 HTTPS 为 `tunnel` | 强调色 `--accent` |

按钮包在一个 `position: relative` 的容器里。悬停或容器 `:focus-within` 时，在按钮下方显示浮层。浮层与按钮之间没有空隙，指针移进浮层后保持打开。浮层 `position: absolute`，`z-index` 高于列表，`max-height: 240px`，超出后内部滚动。最多 100 条主机时不撑开页面。焦点离开且指针也不在容器上时，浮层关闭。

`web/src/capture.ts` 删除 `captureSummary`，改为：

```ts
export function rulesMark(
  file: ConfigValues | null,
  failed = false,
): { active: boolean; lines: string[] }
```

返回值：

- `failed` 为 true：`active` 为 false，`lines` 为 `['无法读取配置']`。此时忽略 `file`。
- `file == null`：`active` 为 false，`lines` 为 `['正在读取配置']`。
- 已载入：前两行固定为 `空规则：抓全部` 或 `空规则：不抓`，以及 `未抓 HTTPS：解密` 或 `未抓 HTTPS：隧道`。主机为空时，第三行是 `主机：无`。有主机时，第三行是 `主机：`，其后每个主机单独占一行，顺序和配置里一致，不截断，不加「等 N 条」。`active` 为 true 当且仅当主机非空、空规则为 `none` 或未抓 HTTPS 为 `tunnel`。

示例：四台主机且其余为默认时，`active` 为 true，`lines` 为：

```text
空规则：抓全部
未抓 HTTPS：解密
主机：
example.com
*.example.com
a.b.example.com
example.com:8443
```

`capturePayload` 保持不变。设置页的字段、校验和「保存后立即生效」保持不变。

## 6. 详情方位

方位状态放在 `App`，因为类名加在工作区上。存储键是 `httpproxy.detailDock`，与 `httpproxy.columns` 分开。读写注入 `Storage`，方式和列显示相同。

`web/src/detailView.ts` 提供：

```ts
export function normalizeDock(raw: unknown): 'bottom' | 'right'
export function loadDock(storage?: Pick<Storage, 'getItem'>): 'bottom' | 'right'
export function saveDock(dock: 'bottom' | 'right', storage?: Pick<Storage, 'setItem'>): void
```

`normalizeDock` 只接受字符串 `bottom` 和 `right`。其他值，包括缺键和损坏的 JSON，都返回 `bottom`。切换时立刻 `saveDock`。

方位按钮放在详情页签行的最右侧，没有选中请求时也在。当前为 `bottom` 时，图标表示右侧栏，`title` 和 `aria-label` 是「详情放到右侧」，点击后变为 `right`。当前为 `right` 时，图标表示底栏，文案是「详情放到下方」，点击后变为 `bottom`。图标是 16px 线条，外框加一条表示目标边缘的粗线。点击不改变选中请求、当前页签，以及美化或原始。

## 7. 详情内容

页签仍是「标头」「请求」「响应」。页签行不换行。左侧是三个页签。右侧从左到右是：美化或原始、复制、方位。美化或原始只在请求页和响应页出现。

### 7.1 查询参数

标头页在「常规」下面增加一节「查询参数」。请求 URL 仍显示完整的 `` `${scheme}://${host}${path}` ``，查询串保留在 URL 里。

`queryParams(path: string): { name: string; value: string }[]` 放在 `detailView.ts`。

- `path` 里没有 `?`，或 `?` 后面是空串：返回空数组。界面这一节显示「(无)」。
- 只取第一个 `?` 之后的文本，按 `&` 拆开，丢掉空段。
- 每段在第一个 `=` 处拆成名称和值。没有 `=` 时，值是空串。
- 名称和值先把 `+` 换成空格，再 `decodeURIComponent`。某一侧抛错时，这一侧保留替换 `+` 之后、解码之前的文本。
- 同名参数逐条保留，不合并。

`/search?q=a+b&x=%20&novalue&a=1&` 解析为 `q=a b`、`x` 为一个空格、`novalue` 的值为空、`a=1`。

### 7.2 状态颜色

`columns.ts` 导出已有的 `statusCell`。详情里的状态值调用它，不另写一套分类。因此：

- `error` 存在时显示「(失败)」，类名 `st-err`，颜色 `--danger`。
- 状态码为 0 时显示「(等待)」，类名 `st-pending`，颜色 `--text-muted`。
- 3xx 使用 `st-redirect`，颜色 `--warn`。
- 4xx 和 5xx 使用 `st-err`，颜色 `--danger`。
- 其余已完成状态，包括 1xx 和 2xx，使用 `st-ok`。

补上 `.st-ok { color: var(--ok); }`。列表状态列已经带有 `st-ok`，所以列表里的 1xx 和 2xx 也会变成绿色。选中行现有的白色文字规则优先级更高，选中后状态列仍是白色。

### 7.3 复制

每个页签有一个文字按钮，常态文案是「复制」。标头页始终有「常规」，复制按钮保持可点。

标头页调用 `headersCopyText`。节与节之间用一个空行分隔，节内用换行分隔，结果末尾不加空行。有 `error` 时，「常规」在远程地址下面增加 `错误：` 行；没有时省略这一行。标头计数与界面一致，`null` 视为 0。查询参数为空时，该节只有一行 `(无)`。查询参数和标头行使用半角冒号 `name: value`，值为空时冒号后没有字符。常规字段保持全角冒号。

```text
常规
请求 URL：https://example.com/a?q=1
请求方法：GET
状态码：200
协议：HTTP/1.1
远程地址：127.0.0.1:54321

查询参数
q: 1

响应标头 (1)
Content-Type: text/plain

请求标头 (1)
Host: example.com
```

请求页和响应页复制当前视图的正文，不包含截断提示。`displayBody` 的结果为空串时，复制按钮禁用。这同时覆盖「(无正文)」和「未捕获正文」。有预览文本时，复制预览文本。

复制调用 `navigator.clipboard.writeText`。成功后按钮显示「已复制」1.5 秒，然后恢复「复制」。新的复制会重计这 1.5 秒。失败，或 `clipboard` 不存在时，按钮旁显示「无法复制」，直到同页签下一次成功，或离开该页签。组件卸载时取消计时器。

### 7.4 美化与原始

请求页和响应页共用一个模式，默认 `pretty`。它放在 `FlowDetail` 的状态里，不写入 `localStorage`。切换请求或页签后保持，整页刷新后回到美化。

把现有 `prettyBody` 从 `FlowDetail.tsx` 移到 `detailView.ts`。`FlowDetail` 改为从那里导入。

```ts
export function displayBody(
  text: string,
  headers: { name: string; value: string }[] | null,
  mode: 'pretty' | 'raw',
): string
```

`pretty` 调用 `prettyBody`。`raw` 返回 `text`，不再判断 JSON。两种模式都保留现在的截断提示。控件是两个分段按钮，样式沿用设置页的 `.seg-btn`。当前项带 `active` 和 `aria-pressed="true"`。

## 8. 模块边界

| 模块 | 职责 | 依赖 |
|---|---|---|
| `capture.ts` 的 `rulesMark` | 由配置算出图标是否点亮，以及浮层行 | `ConfigValues` |
| `RulesMark` | 画图标和浮层 | `rulesMark` 的结果 |
| `detailView.ts` | 方位存储、查询参数、标头复制文本、正文显示 | 无 React |
| `App` | 持有方位，把类名加到工作区，把切换函数传给详情 | `loadDock` / `saveDock` |
| `FlowDetail` | 页签、方位按钮、复制、美化或原始 | `detailView.ts`、导出的 `statusCell` |
| `TopBar` | 画开始或暂停图标 | 现有 `onToggle` |

`App` 不再调用 `captureSummary`。规则浮层的数据来自 `snapshot?.file ?? null` 和现有的 `loadError`。

## 9. 错误处理

- 配置读取失败：抓包按钮禁用并显示开始图标；规则图标为灰色，浮层只有「无法读取配置」。
- 抓包写入失败：保持现有顶栏错误，不改图标所代表的已保存状态。
- `localStorage` 方位值非法：使用 `bottom`，不抛错。
- 查询参数某一侧解码失败：这一侧保留原文，其他参数继续解析。
- 剪贴板失败：显示「无法复制」，不改页签内容。

## 10. 测试

继续用 `node --test`。`web/package.json` 的 `test` 脚本加入 `detailView.test.ts`。

`web/capture.test.ts` 删除 `captureSummary` 用例，改为覆盖 `rulesMark`：

- `rulesMark(null)` 是读取中，不点亮。
- `rulesMark(null, true)` 是读取失败，不点亮。
- 四台主机且 `capture` 为 false 时点亮，行里包含全部主机，不出现「抓包已暂停」或「等 4 条」。
- 主机为空、空规则为 `none`、未抓 HTTPS 为 `tunnel`、`capture` 为 true 时点亮，第三行是「主机：无」。
- 三项都是默认值时不点亮，即使 `capture` 为 false。
- `capturePayload` 的现有用例保留。

`web/detailView.test.ts` 覆盖：

- 第 7.1 节的查询串示例，以及没有 `?`、只有 `?`、`%ZZ` 解码失败。
- 第 7.3 节的标头复制示例，以及省略错误行、空查询、`null` 标头计为 `(0)`。
- `displayBody` 在 `pretty` 下缩进 JSON，在 `raw` 下保留原文。
- `normalizeDock` 接受 `bottom` 和 `right`，拒绝其他值。
- `loadDock` / `saveDock` 用注入的存储读写 `httpproxy.detailDock`。

不增加浏览器自动化测试。不改 Go 测试。

## 11. 验收

- 顶栏不再出现「开始抓包」或「暂停抓包」的文字按钮。悬停能读到这两个句子之一。
- 抓包页不再有摘要文字行。规则偏离默认时图标为强调色，悬停能看到全部主机。
- 方位在下方和右侧之间切换，刷新后保持。列表和详情各占约 55% 和 45%。
- 带查询串的请求在标头页分行列出参数。1xx 和 2xx 在详情和列表里都是绿色。复制结果与第 7.3 节的格式一致。原始模式不重新缩进 JSON。
