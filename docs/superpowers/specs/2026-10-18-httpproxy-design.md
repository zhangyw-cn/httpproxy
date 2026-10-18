# HTTP/HTTPS 代理抓包工具（httpproxy）设计规格

**日期：** 2026-10-18  
**状态：** 已批准（对话确认）  
**参考：** [mitmproxy](https://github.com/mitmproxy/mitmproxy)（概念参考，非移植）

## 1. 目标与非目标

### 1.1 目标（v1）

实现本机/局域网可用的 **HTTP/HTTPS 开发调试代理**：拦截并 **只读查看** 流量，提供嵌入式 Web UI，最终交付 **单个 Go 可执行文件**。

- 后端：Go  
- 前端：React + TypeScript（Vite），构建后通过 `go:embed` 嵌入  
- 协议：HTTP/1.1 与 HTTP/2（HTTPS 经 MITM 解密后查看明文）  
- 实时：WebSocket 推送 Flow 创建/更新  
- 存储：进程内环形缓冲（默认约 1000 条，可配置）  
- HTTPS：启动时生成或复用本地 CA，导出证书文件，用户手动安装信任  

### 1.2 非目标（v1 明确不做）

- 改包、拦截编辑、重放  
- 脚本化规则 / addon 生态  
- 流量落盘（SQLite 等）  
- HTTP/3、透明代理、SOCKS  
- WebSocket 帧内容解析（仅未来可选）  
- 上游再代理（企业代理链式）  
- 证书钉扎 / 双向 TLS 应用的强制解密  

## 2. 约束与成功标准

### 2.1 约束

| 项 | 选择 |
|---|---|
| 定位 | 开发调试工具，先仅支持查看 |
| 证书 | 自动生成 CA，导出文件，用户手动安装 |
| 实时通道 | WebSocket |
| 存储 | 纯内存环形缓冲 |
| 协议 | HTTP/1.1 + HTTP/2 |
| 端口 | 双端口：代理 + Web UI |
| UI | 流量列表 + 详情 + 简单过滤（host / path / status） |
| UI 视觉 | 壳层参考 VS Code / GitHub；抓包工作区参考 Chrome DevTools Network |
| 交付 | 单文件可执行程序 |

### 2.2 成功标准

1. 单二进制启动后，用户安装 CA，将系统或浏览器代理指向代理端口，可在 UI 中查看 HTTP 与 HTTPS（含 HTTP/2）请求的方法、URL、状态码、头、body（在体积限制内）。  
2. 过滤与清空可用；超过 `max-flows` 时淘汰最旧 Flow。  
3. 不提供改包或重放能力。  

## 3. 架构

单进程、两个监听口：

```text
客户端 ──► :8080  Proxy (MITM)
               ├─ CertManager（CA + 按 SNI 签叶子证）
               ├─ HTTP/1.1 & HTTP/2 终结与上游转发
               └─ FlowStore ──► WebSocket Hub

浏览器 ──► :8081  UI Server
               ├─ go:embed 静态前端
               ├─ REST API
               └─ /ws/flows
```

默认端口可配置；建议默认代理 `:8080`、UI `:8081`。

### 3.1 模块边界

| 包 | 职责 | 对外接口要点 |
|---|---|---|
| `internal/cert` | CA 生成/持久化、按 host/SNI 签叶子证并短缓存 | `GetCertificate(ClientHello) (*tls.Certificate, error)`；导出 CA PEM |
| `internal/proxy` | 监听、CONNECT、TLS 终结、上下游转发 | `ListenAndServe`；通过 `FlowSink` 回调吐事件 |
| `internal/store` | 环形缓冲、按 id 查询、过滤、清空 | `Add` / `Update` / `List` / `Get` / `Clear` |
| `internal/api` | REST + WebSocket | 挂到 UI HTTP 服务器 |
| `internal/ui` | `go:embed` 前端产物 | `Handler() http.Handler` |
| `cmd/httpproxy` | flag 解析与组装 | `main` |

开发期允许前端 Vite dev server 与后端分端口热更新；发布时 embed 合成单二进制。

## 4. 数据模型与生命周期

### 4.1 Flow

一条逻辑上的「请求–响应」记为一条 `Flow`：

| 字段 | 说明 |
|---|---|
| `id` | UUID |
| `scheme` | `http` 或 `https` |
| `method` | HTTP 方法 |
| `host` | 目标主机（含端口若非默认） |
| `path` | path + query |
| `http_version` | `"1.1"` 或 `"2"` |
| `status_code` | 响应未到为 `0` |
| `req_headers` / `resp_headers` | 头列表 |
| `req_body` / `resp_body` | 受限字节；超出截断 |
| `req_size` / `resp_size` | 截断前原始大小 |
| `req_truncated` / `resp_truncated` | 是否截断 |
| `started_at` / `finished_at` | 时间戳 |
| `duration_ms` | 完成时计算 |
| `error` | 上游/TLS 等失败文案；成功为空 |
| `client_addr` | 客户端地址 |

### 4.2 生命周期

1. 接受连接：明文 HTTP，或 `CONNECT` 后 TLS 终结（动态证书）。  
2. 读完请求头与受限 body → `store` 写入 pending Flow → WS 推送 `flow.created`（摘要，不含 body）。  
3. 连接上游并转发（连接尽量复用；HTTP/2 每 stream 一条 Flow）→ 读响应 → 更新同一 Flow → WS 推送 `flow.updated`。  
4. 环满淘汰最旧；`DELETE /api/flows` 清空并推送 `flows.cleared`。  

### 4.3 Body 与列表策略

- 默认每侧最多保留 **1 MiB**（`-max-body` 可配）。  
- `GET /api/flows` 与 WS 事件只带摘要；`GET /api/flows/:id` 返回详情（含 body）。  

### 4.4 过滤

服务端过滤参数：`host`、`path` 子串匹配；`status` 精确匹配或前缀（如 `4` 表示 4xx）。过滤只影响 `List`，不改变环内存储内容。

## 5. 代理与 TLS / HTTP2

### 5.1 明文 HTTP

按绝对 URI 或 `Host` 解析目标，作为客户端请求上游，记录 Flow。

### 5.2 HTTPS MITM

1. `CONNECT host:port` → 回复 `200 Connection Established`。  
2. 在隧道上作为 TLS 服务端握手：`GetCertificate` 按 SNI（缺失则用 CONNECT host）用本地 CA 签发叶子证书（短时缓存）。  
3. ALPN 提供 `h2`、`http/1.1`。  
4. 解密后在进程内看到明文 HTTP/1.1 或 HTTP/2 请求。  
5. 作为 TLS 客户端连接真实上游：使用系统根证书校验；ALPN 尽量与客户端侧一致。  
6. HTTP/2：客户端每个 stream 对应一条独立 Flow。  

### 5.3 证书文件

- 路径示例：`~/.httpproxy/ca.crt`、`~/.httpproxy/ca.key`（首次生成，之后复用）。  
- UI 提供 `GET /api/ca.crt` 下载；README 说明如何导入系统/浏览器信任库。  
- 证书钉扎或 mTLS 场景：v1 记为无法完整解密，Flow 可带 `error` 或仅 CONNECT 级元数据，不强行伪装成功。  

### 5.4 错误

DNS、连接、TLS、上游协议错误：写入 `flow.error`，Flow 仍出现在列表，进程继续服务其他连接。

## 6. API 与前端

### 6.1 REST（UI 端口）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/flows?host=&path=&status=&limit=` | 摘要列表，默认新→旧 |
| GET | `/api/flows/:id` | 详情（含 body） |
| DELETE | `/api/flows` | 清空 |
| GET | `/api/ca.crt` | 下载 CA 证书 |
| GET | `/api/health` | 健康检查 |

### 6.2 WebSocket

- 路径：`/ws/flows`  
- 下行：`flow.created`、`flow.updated`（摘要）、`flows.cleared`  
- v1 不上行控制消息；过滤通过 REST 查询参数完成。  
- 客户端断开后自动重连，重连后拉一次全量列表对齐。  

### 6.3 前端

- 栈：Vite + React + TypeScript。  
- 嵌入：`web/` 构建产物复制或输出到 `internal/ui/dist`，由 `go:embed` 服务；非 API/WS 路径回退到 `index.html`（SPA）。  

#### 6.3.1 视觉与交互参考（强制对齐）

分两层，避免「整页模仿 Chrome」或「整页模仿 VS Code」混成一种产品：

| 层 | 参考 | 对齐什么 | 不对齐什么 |
|---|---|---|---|
| **应用壳** | VS Code + GitHub | 整体气质：克制、偏工具/开发者；顶栏与侧栏密度、边框分隔、列表行高、字体层级；浅色为主（可用与 GitHub 接近的中性灰表面 + 清晰分割线）；强调色克制（类似 GitHub 蓝作链接/选中，而非炫光） | 不复刻 VS Code Activity Bar 全套、不嵌入 Monaco、不做 GitHub 营销站风格大留白 |
| **抓包工作区** | Chrome DevTools → Network | 主工作流：上方/主区 **瀑布式请求表**（列：Name/Path、Status、Method、Type 或 Version、Size、Time 等）；选中一行后 **下方（或右侧可切换）详情面板**，Tabs 为 Headers / Payload(Request) / Response / 可选 Timing；过滤条贴近 Network 的 filter 体验（文本过滤 + status 类快捷） | 不实现 Initiator、完整 Waterfall 时间条、HAR 导入导出、Override 等高级能力（v1） |

#### 6.3.2 布局（v1）

```text
┌─ 顶栏（VS Code/GitHub 气质）: 标题 · 代理地址 · CA 下载 · 清空 ─┐
├─ 过滤条（Chrome Network 气质）: host/path 文本 · status ────────┤
├─────────────────────────────┬──────────────────────────────────┤
│  Flow 表格（Chrome Network） │  详情 Tabs（Headers / Request /  │
│  行选中高亮；新流量追加      │  Response）；JSON 可格式化文本   │
└─────────────────────────────┴──────────────────────────────────┘
```

默认采用 **上表 + 下详情**（更贴近 Chrome Network）；若视口较宽，允许实现为 **左表 + 右详情**，但表格列与详情 Tabs 语义仍按 Network，不按 mitmweb 随意命名。

#### 6.3.3 内容展示

- Body：优先纯文本；检测到 JSON 则格式化展示（等宽字体，类似 DevTools；v1 不做完整语法主题高亮）。  
- 错误 Flow：Status 列显示失败态（如 `(failed)` 或红色标记 + `error` 文案可在详情中看到）。  
- 清空：对应 Network 的「清除」心智，一键 `DELETE /api/flows`。  

### 6.4 CLI

示例：

```bash
httpproxy -proxy :8080 -ui :8081 -max-flows 1000 -max-body 1048576
```

可选：`-ca-dir` 指定 CA 目录。

## 7. 错误处理与进程行为

- 单条 Flow 失败不终止监听。  
- CA 目录不可写：启动失败，打印明确路径与原因。  
- 代理或 UI 任一监听失败：进程退出（v1 简化）。  
- WS 重连策略由前端实现。  

## 8. 测试计划

| 范围 | 内容 |
|---|---|
| `cert` | 生成 CA、签叶子证、用该 CA 信任完成 TLS 握手 |
| `store` | 环形淘汰、过滤、Clear、并发安全基本覆盖 |
| `proxy` | 经代理访问测试上游的 HTTP；HTTPS 在信任生成 CA 后断言 Flow 状态码与 body |
| HTTP/2 | 至少一条强制 ALPN `h2` 的集成测试 |
| `api` | 列表、详情、清空、CA 下载 handler 测试 |

## 9. 仓库结构

```text
httpproxy/
  cmd/httpproxy/main.go
  internal/cert/
  internal/proxy/
  internal/store/
  internal/api/
  internal/ui/          # embed + dist
  web/                  # React + TS (Vite)
  docs/superpowers/specs/
  README.md
  go.mod
```

## 10. 实现路径说明

采用 **自研 MITM 核心**（标准库 + `golang.org/x/net/http2`），辅以开发期前后端分离、发布 embed 的工程方式。不依赖以 HTTP/1.1 为主的第三方代理库作为核心，以免 HTTP/2 与 Flow 模型受制于外部抽象。

## 11. 后续演进（不在 v1 范围）

- 请求/响应修改与重放  
- 持久化与导出 HAR  
- WebSocket 抓帧、HTTP/3  
- 上游代理、更细过滤与内容高亮  

## 12. 批准记录

- 方案：自研 MITM + 开发期热更新 / 发布 embed（对话确认）  
- 设计 §1–§5：用户逐节确认 OK（2026-10-18）  
- UI 修订：壳层参考 VS Code/GitHub，抓包工作区参考 Chrome DevTools Network（2026-10-18，待用户确认本文档修订）  
