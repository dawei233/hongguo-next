# core/danmaku —— 弹幕内核（M2）

本包是 `docs/ARCHITECTURE.md` §2.1 目录结构里 `core/danmaku` 的落地，边界见 §2.2：
允许依赖 `core/source`（契约类型）与 `core/danmaku/sign`；不做 UI，不做渲染。
管线见 §5.2，签名接口见 §5.3。

## 文件与来源的对应关系

| 本包文件 | 来源 | 说明 |
|---|---|---|
| `danmaku.go` | 本仓新增 | 类型与合并契约：`Item` / `Track` / `Merger` / `Report` / `Validate` |
| `parse.go` | C `src/native/danmaku.js`（parsePage 段） | 响应解析、双分页信号、颜色/模式归一、文本清洗 |
| `collect.go` | C `src/native/danmaku.js`（fetchDanmaku 段） | 分片循环、失败语义、降级触发条件 |
| `official.go` | C `src/native/danmaku.js`（requestWindow 段） | 签名请求的组装（query、nonce、头部、请求体） |
| `layout.go` | C `src/components/danmakuOfficial.mjs` + `danmakuModel.mjs` | 轨道布局与屏蔽词（两端共享，保证视觉一致） |
| `webfallback.go` | C `src/native/episode-comments.js` + `danmakuModel.mjs`（appendCommentPage） | 无签名公开评论降级源（无时间轴） |
| `sign/` | C `src/native/danmaku-sign.js` + `sm3.js` | X-Gorgon / X-Ladon / X-Argus / SM3 / AES-128-CBC |
| `testdata/vectors.json` | C `scripts/fixtures/danmaku-sign-vectors.json` | **黄金向量，原样搬入**（见下） |
| `sign/sign_test.go` | C `src/native/danmaku-sign.test.cjs`（169 行） | 向量逐字节回归 + 两条硬约束断言 |
| `sign/parity_test.go` | 本仓新增 | 与 `core` 扁平包既有实现的交叉回归（迁移期） |

C 为 `hongguo-drama`（GPL-3.0，与本仓同许可）；A 为自有仓 `nas-backend/danmaku.py`（向量的真正来源）；
B 为 vendor 进 `core/` 的扁平包实现（M2 **不改动**它，两者由 `core/danmaku_bridge.go` 交叉回归）。

## 两条硬约束（改坏 = 签名失效）

来自 C 的工程注释，已在本仓写成断言（§5.1 的要求）：

1. **不得"顺手修正"参考实现里像 bug 的写法。** X-Gorgon 的 RC4 类密钥调度是
   `box[i] = box[j]` 赋值而非交换 —— 服务端接受它，改成标准交换会让签名失效。
   测试：`TestXGorgonKSAMustNotSwap`。
2. **不得改变位运算的宽度与截断位置。** X-Gorgon 的时间戳按大端 32 位写进 seed
   （第 33 位必须被丢弃），而 X-Ladon / X-Argus 走 64 位路径（第 33 位有效）。
   测试：`Test64BitTruncation`。

## 去重键：播放时间 + 文本

上游接口按游标分批返回子集，**同一条弹幕会在不同批次里带着不同的 `comment_id`
再次出现**（B 的参考实现实测：同一集几十秒内会给出 71~104 条不同结果）。所以

- 跨分片去重按 `(TimeMS, Text)` 判定（`Merger` / `Identity`）；
- `Item` 里**根本没有** `comment_id` 字段（契约同此约定）——没有可依赖的字段就不会写错；
- 第二层防护是 `withinWindow`：上游若把上一批的条目重新下发，会因时间落在本片窗口之外被挡住。

## 黄金向量

`testdata/vectors.json` 从 C 的 `scripts/fixtures/danmaku-sign-vectors.json` **原样搬入**：

```
sha256  c7308e2da6c7484e52747f6c1eeb02ab9f1ef6679dadc49075f077c3e5653a86
```

文件的 `source` 字段记录生成来源为 `nas-backend/danmaku.py`（A），因此它覆盖的是 A 的
Python 实现路径，用来与新实现做**跨实现**回归。测试会先断言这个 `source` 字段，避免
有人塞进来路不明的期望值。

**不要自造向量**：自己算出来的期望值只能证明"实现与自己一致"。

## 验证命令

```bash
cd hongguo-next/core
export GOMODCACHE=/workspace/红果/.gomodcache GOCACHE=/workspace/红果/.gocache
export PATH=/workspace/红果/.toolchain/bin:$PATH   # 不要设 GOFLAGS

go test ./danmaku/... -count=1 -v     # 本内核（含黄金向量、交叉回归）
go test . -count=1                    # core 扁平包既有测试（M2 不改动它）
```

- 离线：`go test ./danmaku/...` 不需要网络，也不需要 Python/Node。
- `sign/parity_test.go` 是**外部测试包**（`sign_test`）：`core` 通过 `core/danmaku_bridge.go`
  import 了本包，因此本包的测试要 import `core` 只能走外部测试包 —— Go 明确允许这种形状。
- 联网冒烟测试不在本包内（§6 的 nightly 巡检负责）。

## 与 core 扁平包的接线（M3 会用到的入口）

`core/danmaku_bridge.go` 是唯一的接线面，只**新增**标识符、不改动既有文件：

- `NewDanmakuFetcher(downloader, seriesID, episodeID)` → 组装生产形态的
  `danmaku.Fetcher`（内核负责签名/请求体/解析，`core` 负责 TLS 指纹、代理、限速、重试）；
- `danmaku.Collect(ctx, danmaku.CollectOptions{Fetcher: ...})` → 分片抓取 + 合并去重；
- `DanmakuLegacy*` 三个函数把扁平包的既有实现暴露出来，供迁移期交叉回归，M3 切换完成后删除。

降级源（`CapWebCommentFallback`）的接线在 M3：`danmaku.CollectWithFallback` 已经实现
"官方源为空/失败 → 公开评论"的取舍与文案纪律。

## 未完成 / 已知边界

- **联网验证**：本包的测试全离线。切到生产链路前需要跑一次联网冒烟（§6 nightly），
  确认服务端对 `X-SS-Req-Ticket` 的亚秒位不敏感（见 `sign/sign_test.go` 的
  `TestXSSReqTicketFollowsReferenceRule`）。
- **`core` 扁平包尚未改用本包**：`core/ui_playback_danmaku.go` 与
  `core/provider_hongguo_comment_sign.go` 仍走各自的实现（M2 的硬约束是不改既有文件）。
- **core/media 与站源 provider 是并行线**，与本包无文件交集。
