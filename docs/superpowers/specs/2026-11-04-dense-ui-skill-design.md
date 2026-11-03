# dense-ui 个人技能设计规格

**日期：** 2026-11-04  
**状态：** 待审阅  
**范围：** 个人技能 `~/.cursor/skills/dense-ui/`。不改本仓库的前端、后端、README 或命令行。

## 1. 目标

1. 把当前界面的深浅色令牌和通用组件样式做成一份与仓库脱钩的个人技能，供其他项目在被点名时复用。
2. 技能内带一份可直接打开的示例页。之后改技能里的样式或对外 class 时，同一次改动必须更新这份额示例页。

## 2. 非目标

- 不把活动栏、顶栏、筛选条布局、分栏、详情布局或业务界面放进技能。
- 不规定目标项目的语言或文案。
- 不随本仓库样式自动更新。只有明确要求改技能时，才改 `~/.cursor/skills/dense-ui/`。
- 不在本仓库的 `web/` 里增加预览页。
- 不新增第 5 节以外的颜色变量，也不把第 6 节以外的组件写进 `components.css`。

## 3. 触发

目录名和技能名都是 `dense-ui`。`SKILL.md` 设置 `disable-model-invocation: true`。

只有用户说出「dense-ui」「用 dense-ui」或「按 dense-ui」时才加载。普通做界面，以及单独说「用这套主题」，都不使用这份技能。

技能文件（`SKILL.md`、`tokens.css`、`components.css`、`example.html`）不出现本仓库的产品名 `httpproxy`。

## 4. 文件

```text
~/.cursor/skills/dense-ui/
├── SKILL.md
├── tokens.css
├── components.css
└── example.html
```

`SKILL.md` 写触发条件、接入步骤、编辑时同步示例的规则，以及禁止事项。数值在 CSS 里。`example.html` 用相对路径引用两份 CSS，是标记的参照。

`SKILL.md` 正文少于 500 行，只直接链接同目录的 `tokens.css`、`components.css`、`example.html`。

### 4.1 SKILL.md 全文

```markdown
---
name: dense-ui
description: 把 dense-ui 的深浅色变量和按钮、输入框、芯片、菜单、标签页、数据表、提示条、设置行样式接到界面上。仅当用户明确说出「dense-ui」「用 dense-ui」或「按 dense-ui」时使用。普通界面工作不要使用。
disable-model-invocation: true
---

# dense-ui

## 何时使用

仅当用户说出「dense-ui」「用 dense-ui」或「按 dense-ui」时读取本技能。普通界面工作不要使用。

## 接入

1. 读取 [tokens.css](tokens.css)、[components.css](components.css) 和 [example.html](example.html)。
2. 把要套用的区域放进带 `dense-ui` class 的根元素。整页使用时，把这个 class 放在 `body` 上。
3. 目标项目使用普通 CSS 时，原样拷贝两份 CSS，保留 `.dense-ui` 前缀。标记按 example.html。
4. 目标项目使用 Tailwind、CSS Modules 或 CSS-in-JS 时，按两份 CSS 的变量和尺寸翻译。颜色使用变量或与变量相同的色值，例如 `bg-[var(--bg)]`。不用调色板里相近的颜色代替。
5. 默认外观是深色。浅色由祖先或根元素的 `data-theme="light"` 决定。只有用户要求切换时，才在目标项目里加切换控件。
6. 只改用户点名的那一块。目标项目里已有的其他样式保持不动。
7. 用户要的控件不在 components.css 里时，不修改本技能，也不新增颜色变量。用已有变量来搭：12px 字、6px 圆角、1px `var(--border)`、与 `.btn:focus-visible` 相同的焦点环。
8. 用户要求改目标项目的颜色或尺寸时，只改目标项目。用户明确要求改本技能时，才改本目录，并遵守下一节。

## 编辑本技能

修改 `tokens.css`、`components.css` 或对外 class 时，在同一次改动中更新 `example.html`。example.html 必须继续引用这两份 CSS，并且下面每个 class 至少出现一次：`dense-ui`、`btn`、`btn-icon`、`open`、`seg-btn`、`active`、`input`、`multiline`、`chips`、`chip`、`menu-anchor`、`menu`、`menu-item`、`tabs`、`tab`、`table-wrap`、`data-table`、`is-danger`、`selected`、`empty`、`cell-mono`、`is-warn`、`is-pending`、`is-ok`、`muted`、`notice`、`settings`、`settings-row`、`align-start`、`settings-label`、`settings-field`、`settings-note`、`settings-error`。示例里的主题切换只存在于示例页。

## 禁止

- 不要因为用户在做界面就套用本技能。
- 不要去掉 `.dense-ui` 前缀后把样式写进目标项目的全局选择器。
- 不要把 example.html 拷进目标项目，除非用户要求一份示例页。
```

## 5. 令牌

`tokens.css` 的选择器顺序固定为下面四段。颜色值只出现在这四段的变量定义里。`.dense-ui` 上的 `color` 和 `background` 使用变量，所以切换变量后字色和背景会跟着变。

`box-sizing` 只作用于 `.dense-ui` 子树。`.dense-ui` 另设 `margin: 0`，因此 class 放在 `body` 上时页边距与当前界面一致。不设置 `height: 100%`。

```css
.dense-ui,
.dense-ui *,
.dense-ui *::before,
.dense-ui *::after { box-sizing: border-box; }

.dense-ui {
  color-scheme: dark;
  margin: 0;
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
  --selected-text: #ffffff;
  --danger: #f85149;
  --danger-bg: #490202;
  --warn: #d29922;
  --ok: #3fb950;
  --notice-bg: #2d2305;
  --notice-border: #9e6a03;
  --mono: ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, "Liberation Mono", monospace;
  --sans: -apple-system, BlinkMacSystemFont, "Segoe UI", "Noto Sans", Helvetica, Arial, sans-serif;
  --row-h: 22px;
  font: 12px/1.4 var(--sans);
  color: var(--text);
  background: var(--bg);
  -webkit-font-smoothing: antialiased;
}

.dense-ui code,
.dense-ui pre { font-family: var(--mono); }

[data-theme="light"] .dense-ui {
  color-scheme: light;
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
  --selected-text: #ffffff;
  --danger: #cf222e;
  --danger-bg: #ffebe9;
  --warn: #9a6700;
  --ok: #1a7f37;
  --notice-bg: #fff8c5;
  --notice-border: #eac54f;
}

.dense-ui[data-theme="dark"] {
  color-scheme: dark;
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
  --selected-text: #ffffff;
  --danger: #f85149;
  --danger-bg: #490202;
  --warn: #d29922;
  --ok: #3fb950;
  --notice-bg: #2d2305;
  --notice-border: #9e6a03;
}

.dense-ui[data-theme="light"] {
  color-scheme: light;
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
  --selected-text: #ffffff;
  --danger: #cf222e;
  --danger-bg: #ffebe9;
  --warn: #9a6700;
  --ok: #1a7f37;
  --notice-bg: #fff8c5;
  --notice-border: #eac54f;
}
```

`--danger-bg` 没有组件引用，仍然保留。元素自己的 `data-theme` 写在祖先规则后面，因此优先于外层。

## 6. 组件

`components.css` 的每条选择器都以 `.dense-ui` 开头。菜单在打开时由调用方渲染，样式不负责显隐。`tr.is-danger` 写在 `tr.selected` 之前；同一行若两个 class 都在，选中样式生效。

芯片组只有 `gap: 4px`。多行输入的默认宽度是 280px。

### 6.1 对外 class

`btn`、`btn-icon`、`open`、`seg-btn`、`active`、`input`、`multiline`、`chips`、`chip`、`menu-anchor`、`menu`、`menu-item`、`tabs`、`tab`、`table-wrap`、`data-table`、`is-danger`、`selected`、`empty`、`cell-mono`、`is-warn`、`is-pending`、`is-ok`、`muted`、`notice`、`settings`、`settings-row`、`align-start`、`settings-label`、`settings-field`、`settings-note`、`settings-error`。

根元素另有 class `dense-ui`。`disabled` 是属性，不是 class。

### 6.2 components.css 全文

```css
.dense-ui .btn:focus-visible,
.dense-ui .input:focus-visible,
.dense-ui .chip:focus-visible,
.dense-ui .tab:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: -1px;
}
.dense-ui .btn {
  display: inline-flex; align-items: center;
  height: 24px; padding: 0 10px;
  font: inherit; font-weight: 500; color: var(--text);
  background: var(--bg); border: 1px solid var(--border); border-radius: 6px;
  cursor: pointer; text-decoration: none;
}
.dense-ui .btn:hover { background: var(--bg-hover); }
.dense-ui .btn-icon { width: 24px; padding: 0; justify-content: center; }
.dense-ui .btn.open { color: var(--accent); border-color: var(--accent); }
.dense-ui .btn:disabled { opacity: 0.55; cursor: default; }
.dense-ui .btn:disabled:hover { background: var(--bg); }
.dense-ui .seg-btn.active { color: var(--text); border-color: var(--accent); }

.dense-ui .input {
  flex: none; height: 22px; width: 180px; padding: 0 8px;
  font: inherit; color: var(--text);
  background: var(--bg); border: 1px solid var(--border); border-radius: 6px;
}
.dense-ui .input:focus { border-color: var(--accent); }
.dense-ui .input.multiline {
  height: auto; width: 280px; min-height: 68px;
  padding: 4px 8px; line-height: 18px; resize: vertical;
}

.dense-ui .chips { display: flex; flex: none; gap: 4px; }
.dense-ui .chip {
  height: 20px; padding: 0 8px; font: inherit; color: var(--text-muted);
  background: transparent; border: 1px solid transparent; border-radius: 10px; cursor: pointer;
}
.dense-ui .chip:hover { background: var(--bg-hover); }
.dense-ui .chip.active { color: var(--accent); background: var(--accent-bg); border-color: var(--accent); }

.dense-ui .menu-anchor { position: relative; flex: none; }
.dense-ui .menu {
  position: absolute; top: calc(100% + 4px); right: 0; z-index: 2;
  min-width: 140px; max-height: min(320px, calc(100vh - 80px)); overflow: auto;
  padding: 4px 0;
  background: var(--bg); color: var(--text);
  border: 1px solid var(--border); border-radius: 6px;
}
.dense-ui .menu-item {
  display: flex; align-items: center; gap: 8px;
  height: 24px; padding: 0 10px;
  cursor: pointer; white-space: nowrap;
}
.dense-ui .menu-item:not(:has(input:disabled)):hover { background: var(--bg-hover); }
.dense-ui .menu-item:has(input:disabled) { cursor: default; }
.dense-ui .menu-item input { margin: 0; }

.dense-ui .tabs {
  display: flex; align-items: center; flex: none;
  height: auto; min-height: 28px; flex-wrap: nowrap; min-width: 0; overflow-x: auto;
  background: var(--bg-subtle); border-bottom: 1px solid var(--border);
}
.dense-ui .tab {
  padding: 0 14px; font: inherit; color: var(--text-muted);
  background: transparent; border: 0; border-bottom: 2px solid transparent; cursor: pointer;
}
.dense-ui .tab:hover { color: var(--text); }
.dense-ui .tab.active { color: var(--text); border-bottom-color: var(--accent); font-weight: 600; }

.dense-ui .table-wrap { flex: 1; overflow: auto; }
.dense-ui .data-table { width: 100%; border-collapse: collapse; table-layout: fixed; }
.dense-ui .data-table th {
  position: sticky; top: 0; z-index: 1;
  height: 22px; padding: 0 8px; text-align: left; font-weight: 500; color: var(--text-muted);
  background: var(--bg-subtle);
  border-bottom: 1px solid var(--border); border-right: 1px solid var(--border);
}
.dense-ui .data-table td {
  height: var(--row-h); padding: 0 8px;
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  border-right: 1px solid var(--row-line);
}
.dense-ui .data-table tbody tr:nth-child(even) { background: var(--bg-subtle); }
.dense-ui .data-table tbody tr { cursor: default; }
.dense-ui .data-table tbody tr:hover { background: var(--bg-hover); }
.dense-ui .data-table tbody tr.is-danger td { color: var(--danger); }
.dense-ui .data-table tbody tr.selected { background: var(--selected-bg); color: var(--selected-text); }
.dense-ui .data-table tbody tr.selected td { color: var(--selected-text); }
.dense-ui .data-table tbody tr.empty { background: transparent; cursor: default; }
.dense-ui .data-table tbody tr.empty td { color: var(--text-muted); padding: 16px; white-space: normal; }
.dense-ui .cell-mono { font-family: var(--mono); font-size: 11px; }
.dense-ui .is-danger { color: var(--danger); }
.dense-ui .is-warn { color: var(--warn); }
.dense-ui .is-pending { color: var(--text-muted); }
.dense-ui .is-ok { color: var(--ok); }
.dense-ui .muted { color: var(--text-muted); }

.dense-ui .notice {
  padding: 4px 12px; color: var(--warn);
  background: var(--notice-bg); border-bottom: 1px solid var(--notice-border);
}

.dense-ui .settings { flex: 1; min-width: 0; overflow: auto; padding: 16px 20px; }
.dense-ui .settings h1 { margin: 0 0 12px; font-size: 13px; font-weight: 600; }
.dense-ui .settings-row {
  display: flex; align-items: center; gap: 8px;
  min-height: 28px; margin-bottom: 4px;
}
.dense-ui .settings-row.align-start { align-items: flex-start; }
.dense-ui .settings-row.align-start .settings-label { padding-top: 4px; }
.dense-ui .settings-label { width: 88px; flex: none; color: var(--text-muted); }
.dense-ui .settings-field { display: flex; flex-direction: column; align-items: flex-start; gap: 4px; }
.dense-ui .settings-note { color: var(--warn); }
.dense-ui .settings-error { color: var(--danger); margin: 8px 0 0; }
```

## 7. 作用域与越界

目标项目已有的 `.btn` 等 class 不被覆盖，因为组件选择器都带 `.dense-ui` 前缀。

接到其他写法时，翻译同一套变量和尺寸，不用相近色代替。

控件不在第 6 节时，不往技能里加组件，也不新增颜色变量。用已有变量来搭：12px 字、6px 圆角、1px `var(--border)`、相同焦点环。

用户明确说改技能时，才修改 `~/.cursor/skills/dense-ui/`，并同时更新 `example.html`。否则改动只留在目标项目。

## 8. 示例页

`example.html` 是单文件示例，不经过构建。`body` 的 class 是 `dense-ui`，初始 `data-theme="dark"`。先链接 `tokens.css`，再链接 `components.css`。

页面用现有的 `.btn.seg-btn` 切换主题：点击把 `body` 的 `data-theme` 设为 `dark` 或 `light`，并把 `active` 放在对应按钮上。这个切换只写在示例页的脚本里，不进入 `components.css`。

示例按这个顺序展示，并且每个第 6.1 节的 class 至少出现一次：

1. 主题切换：一个带 `active` 的 `.btn.seg-btn`，一个不带 `active` 的 `.btn.seg-btn`。
2. 按钮：`.btn`、`.btn.btn-icon`、带 `disabled` 属性的 `.btn`。
3. 输入：一个单行 `.input`。
4. 芯片：`.chips` 里一个 `.chip.active` 和一个 `.chip`。
5. 标签页：`.tabs` 里一个 `.tab.active` 和一个 `.tab`。
6. 数据表：`.table-wrap` > `.data-table`。普通行上分别放 `is-ok`、`is-warn`、`is-pending`、`is-danger`，这样不会被选中行或危险行的颜色盖住。另有一行 `selected`（单元格使用 `cell-mono`）和一行 `is-danger`。另一张 `.data-table` 只有一行 `empty`。
7. 提示：`.notice`。各小节标题使用 `.muted`。
8. 设置：`.settings` 含 `h1`、一行 `.settings-row`（`.settings-label` 加 `.input`）、一行 `.settings-row.align-start`（`.settings-label` 加 `.settings-field`，字段里是 `.input.multiline`、`.settings-note`、`.settings-error`）。
9. 菜单放在最后：`.menu-anchor` 里一个 `.btn.open`，以及始终渲染出来的 `.menu`。菜单含一个普通 `.menu-item`，和一个内部有 `disabled` 输入框的 `.menu-item`。`.menu-anchor` 上允许示例页唯一的内联样式 `style="min-height: 132px"`，用来给打开的菜单留出位置。

示例文案只用于展示，不规定目标项目的语言。示例页不内联第 5、6 节的样式。

### 8.1 example.html 全文

```html
<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <title>dense-ui</title>
  <link rel="stylesheet" href="tokens.css">
  <link rel="stylesheet" href="components.css">
</head>
<body class="dense-ui" data-theme="dark">
  <div class="settings">
    <h1>dense-ui</h1>
    <div class="settings-row">
      <span class="settings-label">主题</span>
      <button type="button" class="btn seg-btn active" data-set-theme="dark">深色</button>
      <button type="button" class="btn seg-btn" data-set-theme="light">浅色</button>
    </div>

    <p class="muted">按钮</p>
    <div class="settings-row">
      <button type="button" class="btn">按钮</button>
      <button type="button" class="btn btn-icon" aria-label="图标">×</button>
      <button type="button" class="btn" disabled>不可用</button>
    </div>

    <p class="muted">输入</p>
    <div class="settings-row">
      <input class="input" value="单行">
    </div>

    <p class="muted">芯片</p>
    <div class="chips">
      <button type="button" class="chip active">选中</button>
      <button type="button" class="chip">未选</button>
    </div>

    <p class="muted">标签</p>
    <div class="tabs">
      <button type="button" class="tab active">第一项</button>
      <button type="button" class="tab">第二项</button>
    </div>

    <p class="muted">表格</p>
    <div class="table-wrap">
      <table class="data-table">
        <thead>
          <tr><th>名称</th><th>状态</th></tr>
        </thead>
        <tbody>
          <tr>
            <td class="cell-mono">普通行</td>
            <td class="is-ok">正常</td>
          </tr>
          <tr>
            <td>警告</td>
            <td class="is-warn">警告</td>
          </tr>
          <tr>
            <td>等待</td>
            <td class="is-pending">等待</td>
          </tr>
          <tr class="selected">
            <td class="cell-mono">选中行</td>
            <td>选中</td>
          </tr>
          <tr class="is-danger">
            <td>危险行</td>
            <td>危险行</td>
          </tr>
          <tr>
            <td>状态</td>
            <td class="is-danger">危险</td>
          </tr>
        </tbody>
      </table>
    </div>
    <div class="table-wrap">
      <table class="data-table">
        <tbody>
          <tr class="empty"><td>没有数据</td></tr>
        </tbody>
      </table>
    </div>

    <p class="notice">这是一条提示</p>

    <h1>设置</h1>
    <div class="settings-row">
      <span class="settings-label">名称</span>
      <input class="input" value="示例">
    </div>
    <div class="settings-row align-start">
      <span class="settings-label">说明</span>
      <div class="settings-field">
        <textarea class="input multiline">多行</textarea>
        <span class="settings-note">注记</span>
        <p class="settings-error">错误</p>
      </div>
    </div>

    <p class="muted">菜单</p>
    <div class="menu-anchor" style="min-height: 132px">
      <button type="button" class="btn open">已打开</button>
      <div class="menu">
        <div class="menu-item">普通项</div>
        <div class="menu-item"><input type="checkbox" disabled>不可用</div>
      </div>
    </div>
  </div>
  <script>
    document.querySelectorAll("[data-set-theme]").forEach((button) => {
      button.addEventListener("click", () => {
        const theme = button.getAttribute("data-set-theme");
        document.body.setAttribute("data-theme", theme);
        document.querySelectorAll("[data-set-theme]").forEach((item) => {
          item.classList.toggle("active", item === button);
        });
      });
    });
  </script>
</body>
</html>
```

菜单放在页面底部，并保留那一处 `min-height`，避免盖住前面的区块。`btn open` 出现在菜单示例上；主题按钮不使用 `open`。

## 9. 验证

实现完成后检查技能目录，不启动本仓库前端。

1. 四个文件都在 `~/.cursor/skills/dense-ui/`，内容与第 4.1、5、6.2、8.1 节一致。
2. `SKILL.md` 的 `name` 是 `dense-ui`，`disable-model-invocation` 为 `true`。描述用第三人称，包含三个触发说法，并写明普通界面工作不要使用。正文少于 500 行。
3. 四个文件都不含 `httpproxy`。
4. `components.css` 不含十六进制颜色。每条选择器都以 `.dense-ui` 开头。`tr.is-danger` 出现在 `tr.selected` 之前。
5. `tokens.css` 的主题规则顺序是：`.dense-ui`、`[data-theme="light"] .dense-ui`、`.dense-ui[data-theme="dark"]`、`.dense-ui[data-theme="light"]`。
6. `example.html` 先链接 `tokens.css`，再链接 `components.css`。`body` 同时具有 class `dense-ui` 和 `data-theme="dark"`。第 6.1 节每个 class 都能在该文件中搜到。存在带 `disabled` 属性的按钮，以及菜单项里带 `disabled` 属性的输入框。除 `menu-anchor` 的 `min-height: 132px` 外，没有其他内联样式。
7. 本仓库的 `web/` 没有为这份技能新增文件。
