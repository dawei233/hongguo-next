# hncore-server —— PC 端的 stdio JSON 服务端

`core/cmd/hncore-server` 把共享 Go 内核发给 PC 端：**apps/desktop 的 Electron 主进程
拉起这个子进程，用 stdin/stdout 交换行分隔 JSON**（ARCHITECTURE.md §2.2 的
`apps/desktop → core/cmd/hncore-server`、§2.3 的 `bridge/ts 子进程 JSON-RPC`、
§2.4 的时序图）。安卓端不变，仍走 `bridge/go` 的 2 个 C 符号。

```
Electron 主进程 ──stdin(JSON 行)──▶ hncore-server ──▶ core.NativeRequest(string) string
              ◀──stdout(JSON 行)──
```

本程序不含业务逻辑：唯一业务入口是 `core.NativeRequest`，服务端只负责帧定界、
并发、超时/取消、panic 隔离与优雅退出。

## 1. 帧定界：行分隔（NDJSON），不用长度前缀

一条消息 = 一行 UTF-8 JSON，以 `\n` 结束（最后一帧可省略结尾换行）。

选它的理由：

1. **JSON 里不可能出现裸换行**：Go 的 `encoding/json` 会把字符串中的 U+000A 转义成
   `\n`，所以编码后的载荷内不会出现裸 `0x0A`，行分隔天然无歧义 —— 不需要长度前缀
   来消除歧义。
2. **两端都只有一行代码**：Electron 侧 `readline`，Go 侧 `bufio.ReadSlice('\n')`；
   不需要长度前缀的半包状态机，日志也能直接肉眼读。
3. **长度前缀的唯一硬优势（允许载荷内出现裸换行）在本仓用不上**：请求是机器生成的
   `nativeInput`，响应是机器生成的 `{ok,data}` / `{ok,error,code,retryAt}` 信封。
4. **与既有 ABI 心智一致**：安卓侧 2 个 C 符号传的就是一整串 JSON，PC 侧只是把它
   按行切开，语义面相同。

约束与容忍（都由 `readFrame`/`normalizeFrame` 实现，均有单测）：

| 情形 | 行为 |
|---|---|
| 载荷内含**未转义**裸换行 | 会被当成帧边界（两半各自判为 `invalid_request`）；转义形态 `\n` 不受影响 |
| CRLF（Windows 文本模式） | 行尾 `\r` 剥除 |
| UTF-8 BOM | 行首 BOM 剥除 |
| 空行 / 纯空白行 | 跳过，不产生响应 |
| 首尾空白、行首缩进 | 剥除后交给内核 |
| 最后一帧无结尾换行 | EOF 时按整帧处理 |
| 非法 UTF-8 / JSON 语法错误 / 非对象 JSON | 一条 `invalid_request` 信封；**该行已整行消费，流保持同步** |
| 单帧超过上限 | 同上（默认 1 MiB，见下） |

## 2. 载荷上限

`-max-bytes` 默认 **1 MiB**，与内核自身的上限对齐（`core/app_runtime.go` 的
`NativeRequest`：`len(raw) > 1<<20` 即以「请求格式无效」拒绝）。比它更大的帧内核必然
拒绝，收下来只会白占内存；提前在服务端拒绝还能把「超限」与「JSON 语法错误」区分开。
超限帧的剩余字节被丢弃到行尾为止（不再累积），因此超长行不会把服务端撑爆。

## 3. 请求与响应

### 3.1 请求

除下面 4 个**桥接层**字段外，整帧逐字节交给内核（不改写、不重排、不补默认值）：

| 字段 | 含义 |
|---|---|
| `id` | 字符串或数字。原样回显到对应响应，用于把乱序返回的响应与请求对上 |
| `server` | 非空表示**服务端控制帧**，不转发给内核（见 3.3） |
| `targetId` | 仅 `server:"cancel"` 用，指向要放弃等待的请求 `id` |
| `timeoutMs` | 本请求的服务端等待上限（毫秒）；缺省用 `-timeout` |

内核用 `encoding/json` 的宽松解码（忽略未知字段），因此多带这几个字段不影响解析。
请求示例（与 §2.4 时序一致）：

```json
{"id":"1","action":"initialize","directory":"C:\\Users\\me\\AppData\\Roaming\\hongguo"}
{"id":"2","action":"catalog","source":"hongguo","category":"推荐","page":1}
```

### 3.2 响应（contracts/bridge-api.schema.json 的信封）

```json
{"id":"2","ok":true,"data":{…}}
{"id":"2","ok":false,"error":"内核服务繁忙：在途请求已达上限 32，请稍后重试","code":"upstream_error"}
```

- `ok` / `data` / `error` / `code` / `retryAt` 的形状与必填性完全按契约；
  `id` 属于契约允许的**扩展字段**（两个信封都没有 `additionalProperties:false`，
  契约原文：「允许出现契约未列的扩展字段（向前兼容）」）。
- 内核的响应**逐字节透传**：回显 `id` 时只在最外层对象的第一个 `{` 后插入
  `"id":…,`，不重新编码 —— 这样 PC 端与安卓端（FFI 直接拿内核返回的字符串）看到的
  响应字节完全一致（重新 `json.Marshal` 会把 `<` `>` `&` 转成 `\u003c` 之类）。
- 契约 `$defs/intent`（站源层 8 个意图）不在本服务的范围内：本服务转发的是内核
  `nativeInput.action` 面（`$comment` 已明确该面不在契约范围），两者是同一进程内不同
  的层，互不冲突。

### 3.3 控制帧（服务端本地处理，不转发内核）

| 帧 | 响应 |
|---|---|
| `{"id":…,"server":"ping"}` | `{ok:true,data:{pong,serverVersion,protocolVersion,pid,uptimeMs,workers,maxFrameBytes,requestTimeout,inflight,stopping}}` |
| `{"id":…,"server":"shutdown"}` | 先回 `{ok:true,data:{stopping:true}}`，再走优雅退出 |
| `{"id":…,"server":"cancel","targetId":…}` | 回 `{ok:true,data:{canceled:true,targetId}}`；目标请求随后收到「已取消」信封 |
| 其它 `server` 值 | `invalid_request`（不支持的服务端指令） |

## 4. 并发与顺序

```
读循环（单 goroutine，永不阻塞在业务上）
  └─ 每个请求一个监督 goroutine（在途数 ≤ -max-inflight，默认 max(32, workers*8)）
       └─ 每个内核调用一个执行 goroutine（并发数 ≤ -workers，默认 4）
```

- **响应可能乱序**：请一律带 `id` 并按 `id` 配对。要严格顺序就设 `-workers 1`：
  额度按**票号**（= 帧到达顺序）升序发放，且占到「响应写完」才释放，因此
  响应顺序 = 请求到达顺序，与 goroutine 调度无关（`gate.go` 的 `ticketGate`；
  用 `chan struct{}` 当信号量做不到这点 —— 实测会出现 `0,1,3,2` 这种顺序）。
  并发度 >1 时保证的是「额度按到达顺序发放」，不保证各次内核调用的实际开始时刻
  先后（这是并发的本义）。
- **并发安全**：`core.NativeRequest` 可并发调用（内核用 `nativeState` 与各子结构
  的互斥锁、`sync.Once` 保护）；服务端自身的 stdout 写入由互斥锁串行化，一帧一次
  `Flush`，响应不会互相截断。`go test -race` 覆盖了 12 个真内核本地动作 4 路并发
  （见 `TestRealKernelConcurrentCallsAreSafe`）。
  注意：这只证明**本地动作**并发无竞争；联网动作（catalog/resolve 等）内核侧另有
  限流器（3 并发 + 250ms 间隔），服务端不去也不能替它放宽。
- **重复 id**：同一 `id` 的请求在途时再来一条 → `invalid_request`（否则 `id` 无法
  唯一配对）。取消之后可以立刻复用同一个 `id`。
- **在途上限**：超过 `-max-inflight` 立即回「繁忙」（借用兜底码 `upstream_error`），
  不排队、不无限堆 goroutine。

## 5. 超时与取消

- **超时**：`-timeout`（默认 `0` = 关闭）或每请求 `timeoutMs`。超时只让**调用方**
  不再等待并回 `{"ok":false,"error":"请求超时：…","code":"upstream_error"}`；
  内核调用无法从外部中止（见下），其晚到结果会被丢弃且**不会**补发第二条响应。
- **取消**：`server:"cancel"` 放弃等待某条在途请求；真正的内核侧取消仍走内核自己的
  动作（`cancelRead` / `cancelPlayback` / `cancelSourceJob`），服务端原样转发。
- **为什么不能真正中止内核调用**：`core.NativeRequest(raw string) string` 没有
  `context` 参数，进去之后没有任何外部中断点；内核内部按动作自设期限
  （通用 60s、`moveDownloads` 10min、`danmaku` 10s、`preload` 15s）。因此默认
  `-timeout 0`，避免与内核自身期限互相打架；需要兜底时就设成比 10min 略大的值。
- 超时/取消后，工作槽**仍被占用**直到那次内核调用真正返回，否则内核侧并发会突破
  `-workers`。

## 6. 健壮性与退出

- **panic 隔离**：读循环、监督、内核调用三层各自 `recover`。内核自己已对请求做
  `recover`，服务端这层是第二道保险：任何一层 panic 都只转成一条错误信封，进程不倒。
- **优雅退出**（三种触发：stdin EOF / `server:shutdown` / SIGINT·SIGTERM）：
  停止接收新帧 → 在途请求在 `-grace`（默认 5s）内收尾 → 冲刷 stdout → 退出。
  超期只记一条警告（残留 goroutine 随进程消失）。`-grace 0` 表示不等待。
- **下游提前关管**：Unix 上忽略 `SIGPIPE`（`sigpipe_unix.go`），写 stdout 得到的是
  普通 `EPIPE` 错误，服务端据此停止接收、收尾、以退出码 1 结束；否则会被信号打死
  （默认行为，退出码 141）。
- **退出码**：0 正常收尾；1 stdout 写失败或不可恢复的读错误；2 参数错误。
- stdout **只用于协议帧**，全部诊断日志走 stderr（`-quiet` 可静默）。

## 7. 契约一致性：三处已发现的不一致（只记录，未改契约）

1. **内核业务错误不带 `code`，契约里的机读码目前没人产出**。契约的 code 说明把
   「请求格式无效 / 不支持的应用操作」对到 `invalid_request`、把「应用核心尚未就绪」
   对到 `not_ready`，但 vendored core 实际只回**文案**：实测
   `{"ok":false,"error":"请求格式无效"}`、`{"ok":false,"error":"应用核心尚未就绪，请重新打开应用"}`
   —— 都不带 `code`（契约自己也写明「本仓 vendored core 当前只产生 local_media 与
   source_blocked 两个码，其余为契约预留」）。**服务端刻意不做「文案 → 码」的翻译**：
   翻译属于跨端共享的适配层，只在 PC 服务端补会让 PC 与安卓（FFI 直取内核字符串）
   对同一失败给出不同信封，正好破坏契约要守的那条线。因此服务端只在**自己判定**的
   帧级错误上给出 `invalid_request`（畸形 JSON、超限、坏 id、未知控制指令）。
   要观察内核这一行为，需把 `-max-bytes` 放宽到 1 MiB 以上让超限帧到达内核
   （`TestCoreOwnOversizeRejectionHasNoCode`）；`not_ready` 的行为见
   `TestRealKernelFreshProcessBehavior`。**建议**：若 M4 需要这两个机读码，应在
   `core/source` 的薄适配层（两端共用）里做映射，并同步 ADR。
2. **错误码枚举缺 bridge 层的三档**：`busy`（服务繁忙）、`timeout`（等待超时）、
   `internal`（服务端内部错误）。服务端的取舍：超时与繁忙借用兜底码
   `upstream_error`（契约原文：「upstream_error=站源返回异常（兜底）」），
   取消与 panic **省略 `code`**（前者是调用方自己发起的，后者与内核 `recover` 的
   输出形态保持一致：`{"ok":false,"error":"本地核心处理失败，请重试"}` 同样无码）。
   建议后续 ADR 里评估是否扩枚举。
3. **`resolve` 的载荷形态三处不一致**：契约 `resolve` 意图的载荷是 `$defs/Stream`
   （`url/encrypted/headers/quality/container/needsSanitize/expiresAt`），§2.4 时序图
   写的是 `Playable{url, local, needsSeek}`（`needsSeek` 在本仓 Go 代码与 contracts 里
   **都不存在**，只出现在 §2.4 的图里），而内核现状是 `nativePlan`
   （`url/headers/decryptionKey/quality/qualities/local/expiresAt/prefetchedBytes/
   session/routeIndex/routeCount`）。逐字段看：契约 `quality` 是字符串枚举，内核是
   `int`（像素高度，契约自己也写了「core 内部以像素高度表达清晰度，适配层负责换算」，
   这条**不算**冲突）；但 `expiresAt` 契约要求 RFC3339 字符串，内核给的是
   **Unix 毫秒整数**（`app_playback_routes.go` 的 `credentials.expires.UnixMilli()`），
   且契约的 `encrypted/container/needsSanitize` 三个必填字段在内核载荷里**完全不存在**。
   本服务按 §2.4 与冻结 ABI 的要求**原样透传内核载荷**，不做字段翻译 —— 站源层
   （`core/source` + `core/media`）的 `Stream` 适配属于 M3/M4 的适配层，塞进服务端就是
   把 PC 端特有语义混进跨端稳定面。

## 8. 用法

```
hncore-server [-max-bytes N] [-workers N] [-max-inflight N] [-timeout D] [-grace D] [-quiet] [-version]
```

对 Electron 主进程的建议接线：`spawn(binary, [], {stdio:['pipe','pipe','pipe']})`，
stdout 用 `readline` 逐行 JSON 解析，**任何解析失败都要丢弃该行并记录，绝不能让一行
坏数据带崩主进程**；退出时先发 `{"server":"shutdown"}` 再 `SIGTERM`，最后 `SIGKILL` 兜底。

**启动顺序是硬要求**：第一条必须发
`{"id":"init","action":"initialize","directory":…}` 并**等它的响应**再发业务请求。
内核的 `initialize` 是「谁先到谁建引擎」，并发之下晚到的业务请求会拿到
「应用核心尚未就绪」（实测：真进程管道里 `initialize` 与 `resourceSettings` 同批发出，
后者先返回且失败）。`-workers 1` 天然满足这个顺序。

## 9. 验证

```
cd hongguo-next/core
go vet ./...
go test -count=1 ./...                     # 既有 310 个测试函数（零改动）+ 本包 37 个
go test -race -count=5 ./cmd/hncore-server/ # 顺序/并发/退出类用例重复跑，防偶发
go test -race -count=1 ./cmd/hncore-server/
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./cmd/hncore-server   # linux/darwin 同
```

单测全部用 `io.Pipe` 假造 stdin/stdout 驱动服务端（不依赖真实进程与网络），覆盖：
正常请求与字节级透传、畸形 JSON/非对象/非法 UTF-8、超大载荷与边界、
并发乱序、串行保序、每请求超时、`server:cancel`、重复 id、在途上限、
panic 隔离、EOF/`shutdown`/信号优雅退出、下游提前关管、CRLF 与 BOM、
末帧无换行、帧读取边界、票号闸门（保序/并发上限/取消跳过/500 票压测），以及与
**真内核**的握手（`initialize`）与并发调用。

「未 initialize 时的行为」这类断言依赖内核的**进程级**状态（`nativeState` 无法重置），
`-count>1` 时同进程第二次起内核已就绪，故用 helper process 起一个干净子进程来钉死
（`TestRealKernelFreshProcessBehavior`）；子进程日志里的「失败 2」即两条断言都真的跑了。
