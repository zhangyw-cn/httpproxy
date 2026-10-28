# 抓包开关与主机白名单设计规格

**日期：** 2026-10-28  
**状态：** 待审阅  
**范围：** 抓包开关、主机白名单，以及未记录 HTTPS 的解密或隧道。列表过滤、已有记录的保留、上游连接复用和证书签发保持原样。

## 1. 目标

用户可以暂停抓包，也可以只记录命中主机白名单的流量。代理始终转发。要记录的流量始终解密。这次不记录的 HTTPS 可以继续解密，也可以改为隧道透传，默认继续解密。

成功标准：

- 顶栏可以立即开关抓包，开关写入 `config.yaml`。
- 白名单命中任一主机才记录。列表为空时，按配置抓全部或什么都不抓。
- 未记录的 HTTPS 按配置解密或隧道。隧道不签发叶子证书。
- 已在列表里的记录保留，直到用户点清空。已经开始的 CONNECT 保持它开始时的结论。
- 抓包视图用一行只读摘要显示空规则策略、未抓 HTTPS 的处理方式和主机规则。

初次默认与现在的行为一致：`capture: true`，`capture_empty: all`，`capture_https: decrypt`，`capture_hosts: []`。

## 2. 非目标

- 不按路径、方法、状态码或 SNI 决定抓不抓。
- 不在关闭抓包或修改规则时清空已有记录。
- 不把未记录的流量写成一条 Flow。
- 不为这四项增加命令行覆盖。
- 不新增 WebSocket 事件。
- 不改列表过滤参数，不改列选择。
- 记录以及「解密但不记录」仍走现有的上游连接复用。隧道是单独的 TCP 对拷，不进入这个连接池。
- 不新增浏览器自动化测试。

## 3. 架构

每条新的 CONNECT，或每条明文 HTTP 请求，在开始时取一次策略快照。结论是三种之一：

| 结论 | 行为 |
|---|---|
| 记录 | 沿现有路径转发。HTTPS 解密并写入列表 |
| 解密但不记录 | 转发和加密路径与现在相同。不调用记录，不分配正文捕获 |
| 隧道 | 只用于 HTTPS。先连接上游，成功后再回 `200`，两边对拷。不签发叶子证书，不记 Flow |

`internal/capture` 把配置收成上述结论。三个 Go 值是 `Record`、`DecryptOnly`、`Tunnel`。`Compile(capture, empty, httpsMode, hosts)` 返回 `Policy` 或第 8 节的英文错误。`Decide(https bool, authority string)` 返回结论。`config.Values` 保存原始四项，解析时调用 `Compile`。代理只保存 `Policy` 并调用 `Decide`。设置页保存整份配置。顶栏开关在服务端只改 `capture`，再写回同一个文件。

```text
PUT /api/config 或 PUT /api/capture
  Session.SaveAndApply
    写入 config.yaml
    更新内存中的 file 与 runtime
    proxy.SetPolicy
下一跳 CONNECT 或明文请求
  Policy.Decide → 记录 | 解密但不记录 | 隧道
```

进程开始接受连接之前，用读到的运行配置调用 `SetPolicy`。`Proxy.New` 在尚未调用 `SetPolicy` 时使用默认策略，也就是全部记录。

## 4. 配置与主机匹配

四项写在现有 `config.yaml` 里。`README` 的默认字段列表加上这四项。

```yaml
capture: true
capture_empty: all
capture_https: decrypt
capture_hosts: []
```

`capture` 是布尔开关。YAML 和 JSON 都只接受布尔，不接受字符串 `"true"`。`capture_empty` 只在白名单为空时使用，取值 `all` 或 `none`。`capture_https` 只作用于这次不记录的 HTTPS，取值 `decrypt` 或 `tunnel`。`capture_hosts` 是字符串列表，顺序保留，最多 100 条，每条去掉首尾空白后最长 253 个字符。

文件读取时，这四项缺键或为空值（YAML null）则用默认值，并且不改写文件。原有五项仍然必填。未知字段仍然拒绝。保存和新建文件时，`MarshalYAML` 总是写出这四项。这四项没有命令行覆盖，`file` 与 `runtime` 始终相同。

`PUT /api/config` 的 JSON 必须带上这四项，缺了拒绝。YAML 读取可以缺省，JSON 保存不能缺省。

一条规则去掉首尾空白后，只接受 `host`、`host:port`、`*.host`、`*.host:port`。IPv6 写成 `[::1]` 或 `[::1]:443`。`*` 只能作为开头的 `*.`，后面必须是域名，不能是 IP，不能单独出现，也不能出现在中间或末尾。端口是 1 到 65535。带有协议、路径、用户信息、空白、空标签、首尾点或非 ASCII 字符的规则拒绝。保存时整份配置拒绝，文件保持原样。

重复指规范化之后完全相同：去掉首尾空白、主机忽略大小写、通配前缀和端口集合都相同。`example.com` 与 `example.com:443` 可以同时存在。列表中保留提交时的大小写和顺序。

比较时忽略大小写。不写端口只匹配 80 和 443。写了端口只匹配该端口。`*.example.com` 匹配 `a.example.com` 和 `a.b.example.com`，不匹配 `example.com`，也不匹配 `notexample.com`。

HTTPS 用 CONNECT authority 判断一次。authority 取请求行里的主机，没有则用 `Host`。这条隧道上之后的请求，包括 HTTP/2 流，都沿用这个结论，不再看内部的 Host 或 SNI。明文 HTTP 用这一条请求的目标主机判断。目标没写端口时，HTTP 按 80，HTTPS 按 443。

开关关闭时不看白名单，全部不记录，HTTPS 使用 `capture_https`。开关打开且列表为空时，按 `capture_empty`。列表非空时，命中任一规则才记录。明文 HTTP 不会得到隧道；不记录时的结论是「解密但不记录」，含义是转发并且不写入。

## 5. 代理数据流

`SetPolicy` 替换代理上的策略快照，替换方式和 `SetMaxBody` 相同。只影响之后新进入判断的 CONNECT 和明文请求。

`handleConnect` 在回写任何响应、做 TLS 之前判断一次。

隧道：用 `net.Dialer{Timeout: 30s, KeepAlive: 30s}` 连接 authority 里写明的 `host:port`。与现有逻辑一样，拒绝连回代理自己的监听地址。拨号失败，或 authority 没有可拨号的端口，回 502，不回 `200`，不记 Flow。拨号成功才回 `200 Connection Established`，然后从已经缓冲过的连接两边对拷。对拷前清掉读截止时间，不使用 120 秒的「下一条请求」超时。任一边结束或出错，就关闭两边。这条路径不使用 CA。

记录或解密但不记录：仍然要求 CA。没有 CA 时维持现在的 502，正文为 `HTTPS interception requires a CA\n`。记录沿现有 HTTP/1.1 和 HTTP/2 路径，包括失败的 TLS 握手。解密但不记录走同一条解密和转发路径，不调用正文捕获，不调用 sink。TLS 握手失败也不记。

明文 HTTP 在 `handleRequest` 入口判断一次。同一条 keep-alive 连接上的下一条明文请求会重新判断。已经进入 `handleRequest` 的请求保持本次结论。不记录时照常转发，错误仍回给客户端，不写入列表。

## 6. API

`GET /api/config` 的 `file` 和 `runtime` 都带上这四项。

`PUT /api/config` 保存整份配置。成功后写回 `config.yaml`，在会话锁里调用 `SetPolicy`，响应体仍是 `{file, runtime}`。任一次成功保存都会按新的四项设置策略，包括只改了主题的保存。

`PUT /api/capture` 的正文只能是 `{"capture": true}` 或 `{"capture": false}`。服务端读取当前文件里的其余字段，只改 `capture`，再走同一次 `SaveAndApply`。成功时响应体同样是 `{file, runtime}`。

不新增 WebSocket 事件。发出请求的页面用响应更新自己的配置。`GET /api/flows` 的过滤参数不变。

## 7. 界面

顶栏在「下载 CA」左边加一个按钮。抓包开着时文字是「暂停抓包」，关着时是「开始抓包」。点击调用 `PUT /api/capture`。配置还没载入，或另一项配置写入还在进行时，按钮不可点。失败时按钮停在原来的状态，旁边显示接口返回的 `error`，下一次成功后清掉。

抓包视图里，过滤条下面加一行只读摘要，不参与筛选。配置还没载入时显示「正在读取配置」。载入后按这个顺序拼接，段之间用 ` · `：

1. 仅当 `capture` 为 false 时，第一段是「抓包已暂停」。
2. `空规则：抓全部` 或 `空规则：不抓`。
3. `未抓 HTTPS：解密` 或 `未抓 HTTPS：隧道`。
4. 主机段。没有规则时是 `主机：无`。有 1 到 3 条时，按保存顺序用 `、` 连接。超过 3 条时列出前 3 条，后面加 ` 等 N 条`，N 是规则总数。

示例：`抓包已暂停 · 空规则：抓全部 · 未抓 HTTPS：解密 · 主机：example.com、*.example.com、a.b.example.com 等 12 条`

这一行的 `title` 是完整主机列表，一行一条。没有规则时 `title` 为「无」。

设置页在正文上限下面加三项。表单不编辑开关。保存时带上点击当时快照里的 `capture`。

- 空规则：「抓全部」和「不抓」。
- 未抓 HTTPS：「解密」和「隧道」。
- 主机规则：多行文本框，一行一条。占位示例为 `example.com`、`*.example.com`、`example.com:8443` 三行。保存时去掉空行和每行首尾空白。控件下注明「保存后立即生效」。

文本框和两个选择只在初次载入，以及 `capture_empty`、`capture_https`、`capture_hosts` 变化时从快照刷新。只改变 `capture` 的响应不覆盖这三项里尚未保存的编辑。

顶栏开关和设置保存同时只有一个可以发出。列表过滤、列选择和已有记录的展示保持不变。

## 8. 错误处理

配置文件里这四项缺键或为空值时，用默认值启动，不改写文件。值存在但不合法时，`config.Open` 失败，进程以状态 1 退出，向标准错误输出 `config:` 和英文原因，不开始监听。

英文原因：

| 原因 | 文本 |
|---|---|
| 开关类型不对 | `capture must be a boolean` |
| 空规则取值不对 | `capture_empty must be all or none` |
| 未抓 HTTPS 取值不对 | `capture_https must be decrypt or tunnel` |
| 主机规则不是字符串列表 | `capture_hosts must be a list of strings` |
| 超过 100 条 | `capture_hosts must contain at most 100 rules` |
| 单条超过 253 个字符 | `a capture host rule must be at most 253 characters` |
| 规则语法不合法 | `invalid capture host rule` |
| 规则重复 | `duplicate capture host rule` |

`PUT` 失败时文件和代理策略都保持原样。沿用现有文案：「配置缺少字段」「配置含有未知字段」「无法写入配置」。新增的 HTTP 400 按第一次发现的问题返回：

- `抓包开关必须是布尔值`
- `空规则必须是 all 或 none`
- `未抓 HTTPS 必须是 decrypt 或 tunnel`
- `主机规则必须是字符串列表`
- `主机规则最多 100 条`
- `主机规则不能超过 253 个字符`
- `主机规则不合法`
- `主机规则重复`

`PUT /api/capture` 缺少 `capture` 时返回「配置缺少字段」。值不是布尔时返回「抓包开关必须是布尔值」。含有其他字段时返回「配置含有未知字段」。

规则先去掉首尾空白再检查。检查顺序是：类型、条数、单条长度、语法、重复。

隧道连回代理自己时，回 502，正文为 `refusing to proxy to the proxy's own listen address\n`，不记 Flow。目标无法解析出可拨号的端口，或上游拨号失败时，回 502，正文为 `Bad Gateway\n`，不回 `200`，不记 Flow。对拷中途断开时关闭两边，不记 Flow。解密但不记录时，上游失败和 TLS 握手失败仍把错误回给客户端，不记 Flow。正在记录时，这些失败仍按现在的方式写入列表。

界面上，保存或开关失败时保留用户已经填写的内容，并显示接口返回的 `error`。

## 9. 测试

`internal/capture` 的纯函数测试覆盖：精确主机、任意深度的 `*.`、自身不匹配、省略端口只含 80 和 443、写明端口、大小写、IPv6、非法规则、重复规则、超过 100 条、开关关闭、空列表配 `all` 和 `none`。明文请求不会得到隧道。

配置测试覆盖：文件缺这四项时用默认值、保存时把四项写回、非法值拒绝且不改文件。API 测试覆盖：`PUT /api/config` 成功后策略生效；缺字段或规则不合法时返回第 8 节的 400，且代理策略不变；`PUT /api/capture` 只改开关并拒绝多余字段。

代理测试用本地上游，并检查列表里有没有 Flow：

- 默认策略仍记录 HTTP 和 HTTPS。
- 不匹配且为解密：客户端拿到完整响应，列表里没有这条，这条路径不调用正文捕获。
- 不匹配且为隧道：不签发叶子证书，请求和响应字节原样通过。拨号失败或连回代理自己时得到 502，并且没有先收到 `200`。
- 一条隧道已经开始后更换策略，这条连接保持原来的结论。明文 keep-alive 的下一条请求使用新策略。
- TLS 握手失败只在「记录」时出现在列表里。

前端在 `web/src/capture.ts` 放两个纯函数，由 `web/capture.test.ts` 用现有的 `node --test` 覆盖，并把该文件加入 `web/package.json` 的 `test` 脚本。摘要函数检查暂停前缀、三项顺序、最多 3 条主机、`等 N 条` 的 N 为总数、空列表写成「无」。提交体函数带上当时的 `capture`，并去掉空行和首尾空白。现有抓包测试在不调用 `SetPolicy` 时仍全部记录。
