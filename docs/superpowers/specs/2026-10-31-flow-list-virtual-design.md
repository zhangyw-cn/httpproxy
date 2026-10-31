# 流量列表虚拟化设计规格

**日期：** 2026-10-31  
**状态：** 待审阅  
**范围：** 仅网页流量列表的挂载、条纹和滚动。不改 Go 后端、摘要字段、筛选请求、详情面板、列定义、README、命令行。

## 1. 目标

抓包列表在持续更新时保持稳定：

- 条纹颜色跟每一行走。顶部旧行被丢掉，或中间某一行被筛选掉时，其余行不变色。
- 默认跟着最新一行。用户往上翻后停住；滚动位置回到距底部小于 24px 时重新跟着。
- 跟着的时候，视口在绘制前钉在最新一行，包括删一条又加一条、总条数不变的情况。
- 停住的时候，正在看的行留在原地。
- 某一行从「等待」变成状态码时，只更新这一行。旁边的行不重绘。

列表只挂载视口里的行，上下各多留 10 行。不引入虚拟列表库。行高固定 22px。

## 2. 非目标

- 不改后端、REST、WebSocket 或 `FlowSummary`。
- 不改筛选条件的含义，也不改列的定义、存储和选择器。
- 不改详情面板，也不改选中行被淘汰时取消选中的现有逻辑。
- 不做横向虚拟化，不测量每一行的实际高度，不用平滑滚动。
- 不新增浏览器测试框架，不新增运行时依赖。

## 3. 架构

虚拟化只发生在流量列表。`App` 仍持有从旧到新的 `flows`。筛选、WebSocket、清空、选中和详情面板的数据流保持现在的行为。

滚动仍是现在这一个容器，横向和纵向都在里面。表头继续吸顶，列宽仍用现有的 `tableMinWidth` 和 `table-layout: fixed`。表体总高度为表头 22px 加上 `行数 × 22px`。只挂载窗口内的数据行；窗口外的高度用上下两块空白垫出。空白高度为 0 时不渲染该空白行。

行高和表头高都是常量 22px，与 `--row-h` 和表头 CSS 的 22px 一致。贴底和停住后的位置补偿都在 `useLayoutEffect` 里、浏览器绘制之前写入 `scrollTop`。不写 `scrollLeft`。

`flows.length === 0` 时仍渲染现在那句空列表提示，不走虚拟窗口，也不渲染空白垫片。

## 4. 条纹

条纹账放在 `App` 的 ref 里，页面不刷新就一直在。结构是 `next: number` 加 `Map<id, 0 | 1>`。

`syncStripes(book, ids)` 按 `ids` 的顺序走一遍：

1. id 已在账本里：沿用原来的 0 或 1。
2. id 不在账本里：编号为 `next % 2`，然后 `next` 加 1。

同一份 id 再同步不会再次取号，因此渲染被执行两次时编号不变。返回的数组与 `ids` 等长，并传给 `FlowTable`。

同步一份空列表时不改账本。滚出视口、被筛选掉、被淘汰、切到设置再回来，都不从账本删 id，也不改已有编号。账本只在清空时丢掉。清空包括顶栏清空和 WebSocket 的 `flows.cleared`：把 `next` 置 0 并清空 map。下一条新请求从 0 开始。筛选结果为空时不清账。

编号 0 用表格底色，编号 1 用现在偶数行的 `--bg-subtle`。因此一份新列表的第一行不带条纹，第二行带条纹。筛选掉中间一行后，上下两行可以同色。悬停和选中仍盖住条纹底色。失败行仍只改文字颜色。

渲染时某一行没有对应编号，按 0 画。

## 5. 窗口

纯计算放在 `web/src/flowWindow.ts`，不读 DOM。常量：

| 名称 | 值 |
| --- | --- |
| `ROW_HEIGHT` | 22 |
| `HEADER_HEIGHT` | 22 |
| `OVERSCAN` | 10 |
| `STICK_PX` | 24 |

`visibleRange(scrollTop, clientHeight, count)` 返回半开区间 `[start, end)`，以及上、下空白的像素高度。`count <= 0` 或 `clientHeight <= 0` 时，四个值都是 0。否则：

```
start = min(count, max(0, floor(scrollTop / ROW_HEIGHT) - OVERSCAN))
end   = ceil((scrollTop + clientHeight - HEADER_HEIGHT) / ROW_HEIGHT) + OVERSCAN
end   = min(count, max(start, end))
top    = start * ROW_HEIGHT
bottom = (count - end) * ROW_HEIGHT
```

取整与 JavaScript 的 `Math.floor`、`Math.ceil` 相同。锁定这些结果：

| scrollTop | clientHeight | count | start | end | top | bottom |
| --- | --- | --- | --- | --- | --- | --- |
| 0 | 200 | 100 | 0 | 19 | 0 | 1782 |
| 500 | 200 | 100 | 12 | 41 | 264 | 1298 |
| 2022 | 200 | 100 | 81 | 100 | 1782 | 0 |
| 0 | 200 | 0 | 0 | 0 | 0 | 0 |
| 0 | 0 | 100 | 0 | 0 | 0 | 0 |

表体只渲染 `flows[start .. end)`。上空白高度是 `top`，下空白高度是 `bottom`。

## 6. 滚动与跟随

`FlowTable` 用 ref 记住是否贴底，初次挂载为贴底。用户滚动时，距底部小于 24px 为贴底，大于等于 24px 为停住。距离是 `scrollHeight - scrollTop - clientHeight`，三个数都取滚动容器当时的值。

贴底标志只在用户滚动时按这个距离更新。程序写入 `scrollTop` 引发的滚动事件不更新它。`flows` 或视口高度变化的那一次提交里，先挡住标志更新，等绘制前的补偿写完再放开。内容变短、视口被挤进底部 24px 时，不因此重新跟随。

锚点是视口最上面那一条数据行：`index = min(count - 1, max(0, floor(scrollTop / ROW_HEIGHT)))`，`offset = HEADER_HEIGHT + index * ROW_HEIGHT - scrollTop`。用户滚动且列表非空时记下该行的 id 和 offset。

`nextScrollTop` 的输入是：是否贴底、`clientHeight`、新 id 列表、上一份 id 列表、锚点 id、锚点 offset。返回 `{ scrollTop, anchorId, anchorOffset }`。

- `clientHeight <= 0` 或 id 列表为空：`scrollTop` 为 0，锚点 id 为 `null`，offset 为 0。组件在还没量到高度时不写 `scrollTop`。
- 贴底且列表非空：忽略锚点。`scrollTop = max(0, HEADER_HEIGHT + count * ROW_HEIGHT - clientHeight)`，返回的锚点 id 为 `null`、offset 为 0。100 行、视口高 200 时为 2022。1 行、视口高 200 时为 0。
- 停住且锚点 id 还在新列表里：`scrollTop = HEADER_HEIGHT + newIndex * ROW_HEIGHT - offset`，再夹到 `[0, maxScroll]`。它上面被删掉一行时，`scrollTop` 减少 22。例：旧下标 10、offset 为 22，删掉它上面一行后新下标为 9，`scrollTop` 为 198。
- 停住且锚点 id 不在新列表里：在上一份 id 里找到它，先找更靠下、且仍在新列表中的第一条。该行原来的 offset 是 `锚点 offset + (它的旧下标 - 锚点旧下标) * ROW_HEIGHT`。id 在新列表中的下标取第一次出现的位置。再按同一公式钉住它。例：锚点旧下标 10、offset 22，下面那条旧下标 11；锚点被删后它的新下标为 10、原 offset 为 44，`scrollTop` 为 198。
- 下面没有，就找更靠上的第一条仍在的行，offset 用同一个式子（下标差为负）。例：锚点是最后一条、旧下标 10、offset 22，它被删掉后钉住旧下标 9 的那条，原 offset 为 0，`scrollTop` 为 220。
- 上下都没有，或上一份列表里也没有这个锚点：`scrollTop` 为 0，锚点 id 为 `null`。
- 贴底、列表为空或 `clientHeight <= 0` 时，返回的锚点 id 为 `null`、offset 为 0。调用方不靠这个返回值清掉已经记下的锚点。
- 算出的 `scrollTop` 小于 0 时取 0，大于 `maxScroll` 时取 `maxScroll`。`maxScroll = max(0, HEADER_HEIGHT + count * ROW_HEIGHT - clientHeight)`。例：新下标 99、offset 0、100 行、视口高 200 时，未夹紧的值是 2200，结果为 2022。10 行、视口高 200、新下标 0、offset 100 时，未夹紧的值是 -78，`maxScroll` 是 42，结果为 0。

组件只在「停住、列表非空、并且函数返回了锚点 id」时改记锚点。贴底时不覆盖已记下的锚点。列表变成空、且不是清空时，滚动到 0，但保留锚点和上一份非空 id，方便结果回来时按上面的规则找。清空时因为列表重新挂载，锚点和上一份 id 自然丢掉。

直接赋值 `scrollTop`，不用平滑滚动。容器尺寸变化时重新量高度并走同一套补偿：当时在跟随就再次钉底，已停住就保持锚点行的 offset。

## 7. 组件与数据流

| 单元 | 职责 |
| --- | --- |
| `web/src/flowWindow.ts` | 条纹同步、窗口、贴底判断、绘制前的 `scrollTop` |
| `web/src/App.tsx` | 持有条纹账和 `listEpoch`。清空时重置账本并把 `listEpoch` 加 1。把与 `flows` 对齐的条纹数组和 `listEpoch` 传给列表 |
| `web/src/components/FlowTable.tsx` | 滚动容器、绘制前补偿、空白垫片、只挂载窗口内的行 |
| `web/src/styles.css` | 用行上的条纹类代替按行号变色；空白行不撑高 |

`listEpoch` 从 0 开始。顶栏清空和 `flows.cleared` 都先重置条纹账，再把 `listEpoch` 加 1，并把 `flows` 清成空列表。`FlowTable` 的 `key` 用 `listEpoch`。清空会重新挂载列表：贴底为真，滚动在顶部。下一条新请求到来时钉在底部。筛选和重连不增加 `listEpoch`，跟随与否保持不变。

切到设置时列表卸载。回到抓包时重新挂载，从跟随底部开始。条纹账仍在 `App` 里。

行是 `FlowTable.tsx` 里单独的组件，用 `memo` 包住，身份是请求 id。比较 flow 引用、条纹数字、是否选中、可见列 id 序列。这四项相同就不重绘，即使列定义对象是新建的。点击回调不参与比较；列表在一次挂载里传入同一个函数。`App` 在更新某一行时只替换该行对象，其它行保持原引用。选中状态仍在 `App`。选中行滚出窗口后，选中和详情都保持。

数据行和空白行都在现有的 `<table>` 里。空白行是一个 `td`，`colSpan` 等于当前可见列数，无内边距、无边框，高度精确等于垫高像素，不接收点击，不带条纹类。

## 8. 边界

- 补偿用的内容高度是 `HEADER_HEIGHT + count * ROW_HEIGHT`，不读容器的 `scrollHeight`。滚动位置夹在 `[0, maxScroll]`。内容比视口矮时为 0。贴底判断仍用容器当时的 `scrollHeight`、`scrollTop` 和 `clientHeight`。
- 起止下标夹在 `0` 和行数之间，且 `end` 不小于 `start`。
- 行高锁死 22px。单元格仍单行省略。窗口不测量单行高度。
- 只虚拟化纵向。切换列不改变 `scrollLeft`、滚动位置和贴底标志。
- 视口高度用状态保存，在布局效果里测量。高度仍为 0 时不写 `scrollTop`。这次测量引起的再次布局发生在绘制前。
- 选中行被 `flow.evicted` 淘汰时，仍按现在的逻辑取消选中。它的条纹编号留在账本里，直到清空。

## 9. 样式

删除 `.flow-table tbody tr:nth-child(even)`。编号为 1 的行使用类 `row-stripe`，背景为 `var(--bg-subtle)`。悬停和选中的规则写在它后面，继续盖住条纹。选中行仍是白字。失败行仍用 `row-err` 只改文字颜色。

数据行继续用 `--row-h`。空白行用单独的类去掉 `.flow-table td` 的高度、内边距和右边框，再写上精确高度。

## 10. 测试

`web/flowWindow.test.ts` 使用现有的 `node --experimental-strip-types --test`，并加入 `web/package.json` 的 `test` 脚本。

条纹：

- 空账本对 `[a, b, c, d]` 得到 `[0, 1, 0, 1]`，`next` 为 4。
- 再同步同一组 id，编号不变，`next` 仍为 4。
- 末尾加上 `e` 得到 `[0, 1, 0, 1, 0]`，`next` 为 5。
- 去掉 `b` 后 `[a, c, d, e]` 得到 `[0, 0, 1, 0]`。
- 同步 `[]` 不改账本。
- 重置后同步 `[a]` 得到 `[0]`，`next` 为 1。

窗口：第 5 节的五组输入和输出。

滚动：

- 贴底、100 行、视口高 200，结果 2022。
- 贴底、1 行、视口高 200，结果 0。
- 列表为空，无论是否贴底，结果 0。
- `clientHeight` 为 0，结果 0。
- 停住，锚点还在，上面删一行：旧下标 10、offset 22，结果 198。
- 停住，锚点被删，钉住下面那条：旧下标 11 的原 offset 为 44，结果 198，返回的锚点是那一条。
- 停住，下面没有，钉住上面那条：旧下标 9 的原 offset 为 0，结果 220，返回的锚点是那一条。
- 贴底时即使锚点会算出别的位置，100 行、视口高 200 仍是 2022。
- 未夹紧的 2200 夹成 2022。10 行、视口高 200、新下标 0、offset 100 时，结果为 0。
- 距底部 23 为贴底，距底部 24 为停住。例：`scrollHeight` 1000、`clientHeight` 200、`scrollTop` 777 为贴底，`scrollTop` 776 为停住。

`npm test` 和 `web/` 下的 `npm run build` 都要通过。

## 11. 手动核对

构建之后看这六件事：

1. 贴底时连续来新请求：条纹不翻，最新行留在底部，没有先画再跳。
2. 往上翻后，顶部淘汰、底部追加：正在看的行不动。
3. 滚回距底部 24px 以内，又重新跟随。
4. 一行从「等待」变成状态码，旁边的行不闪。
5. 清空后，下一批从第一条重新交替，并重新跟随。
6. 表头吸顶、横向滚动、列切换、选中和详情仍可用。
