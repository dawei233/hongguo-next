# hongguo-next 架构设计

> 状态：草案 v3 · 日期：2026-10-15
> **本版修正（v3）**：① **安卓端技术栈判断错误已更正** —— `apps/android` = **B 的 Flutter 应用整端 vendor**（`lib/` 84 文件 / 30558 行 + `android/ios/windows` 平台层 + `bridge/dart` FFI 保留），**不是**"Media3 + Gradle 重建"；被废弃的是旧 monorepo（A）的 Java WebView + Media3 方案，不是 Flutter。② **PC 端播放器更正**为 `@videojs/react` 10.x 的 `createPlayer({features})` 组合式 API（npm 包 `video.js` 在 v10 已是墓碑包）。
> 适用仓库：`github.com/dawei233/hongguo-next`（本地为 `/workspace/红果/hongguo-next`）
> 前提：三个老仓（hongguo-monorepo / guoapp-android / hongguo-drama）**停止开发**，其代码按本仓许可策略**直接并入本仓统一维护**，老仓仓库仅作历史存档。

---

## 0. 一句话结论（先读这段）

1. **本仓不做"三合一仓库"的形态合并，但做"代码归一"**：共享 Go 内核 + 双端壳 + 站源插件 + 弹幕内核，三仓中可用的实现**直接并入** `core/` 统一维护，不再有"构建期外部 checkout"或"零代码复制"这类隔离层。
2. **全仓统一 GPL-3.0**。用户是三个仓库的版权人，三个老仓都已明确授权按 GPL-3.0 搬运；`hongguo-drama` 本身即 GPL-3.0，`hongguo-monorepo` 与 `guoapp-android` 是作者本人名下、`license=NONE`、`fork=false` 的自有仓，有权重新以 GPL-3.0 授权。`LICENSE` 已就位（仓库根）。
3. **决定性事实：本仓的 GPL-3.0 不是新引入的约束，而是与既有分发现实一致。** `hongguo-drama` 已捆绑 **ffmpeg 9.0.2 + x264**，构建为 **GPLv3-or-later**（`build/ffmpeg/WINDOWS-NOTICE.txt`、`THIRD-PARTY-NOTICES.md`），因含 x264 无法走 LGPL —— **现有分发物本来就是 GPLv3 二进制**。既然产品线已经在分发 GPLv3 二进制，把源码也统一为 GPL-3.0 不增加任何新义务，却消掉了全部许可隔离工作。
4. **搬运策略从"重写/零复制"整体改为"原样搬运或最小改造"**：B 的 24035 行 Go 内核（121 文件 / 27 测试）直接 vendor 进 `core/`（仅删 `buildAllSources` 门控）；C 的 `xifan.js`/`hema.js`（302 行）直接移植；C 的弹幕签名与其黄金向量测试直接搬用（不再自生成向量）。只有跨越语言边界的地方（A 的 Python → Go）才需要转写。
5. **不再有许可前置阻塞**：原 M0「补 LICENSE + 发许可邮件」整个消失，工期净减少约 5–7 人日的前置与隔离工作，且把最大的不确定性（上游不回信）直接归零。
6. **安卓端不是"重建"，而是"整端 vendor"**：安卓版**直接采用 B 的 Flutter 应用**（`github.com/dawei233/guoapp-android`，用户明确指示"安卓版用他的"）。其 `lib/`（**84 文件 / 30558 行 Dart**）+ `android/`/`ios/`/`windows/` 平台层**原样并入 `apps/android`**；B 的 Go 内核本就通过 `native/bridge/main.go` 的 FFI（2 个 C 符号）供该 Flutter 端调用，Dart 侧绑定即 `lib/core_bridge.dart`，所以桥接层原样保留即可。**被废弃的是旧 monorepo（A）的 Java WebView + Media3 方案，不是 Flutter。** 本文此前把 `apps/android` 写成"Media3 + Gradle 重建"是判断错误，已全文更正。
7. **PC 端播放器用 `@videojs/react` 10.x**：npm 包 `video.js` 在 v10 已是**墓碑包**（`videojs()` / `registerPlugin` / `registerComponent` 全部 `throwLegacyError('VJS8_LEGACY_*')`，README 指向 `@videojs/html` 与 `@videojs/react`）。本仓 PC 端采用 **`@videojs/react` 10.x 的 `createPlayer({features: [...]})` 组合式 API**（返回 `{ Player, usePlayer, useMedia }`），**禁止**按 video.js 8 教程写 `videojs('my-video')` 实例 + `registerPlugin` 全局注册。HLS 由 v10 自带的 `@videojs/spf` 引擎承担，hls.js 仅作可选 adapter（`<HlsJsVideo/>`）。详见 `DESIGN-SYSTEM.md` §1.3。

---

## 1. 命名与仓库边界

### 1.1 命名

| 项 | 取值 | 说明 |
|---|---|---|
| 仓库名 | `hongguo-next` | 已建，GitHub `dawei233/hongguo-next` |
| 产品名 | 红果 Next / `HongguoNext` | 不再用「漫剧 / 下载器 / 鉴」任何一个老名字，避免与老仓混淆 |
| Go module | `github.com/dawei233/hongguo-next/core` | 唯一内部可 import 前缀 |
| 包前缀 | `hncore/` 短名仅用于 `package` 内引用 | 不做 Go 包重命名，目录即 module 路径 |
| Flutter/Dart 包名 | `hongguo_next` | 与 B 的 `duanju_app` 区分 |
| Electron appId | `com.dawei233.hongguo.next` | 与 `com.hongguo.manju`（A）、B 的签名配置区分 |
| 版本线 | `0.x`（架构冻结前），`1.0` 定义为"三源同时可用且 CI 全绿" | |

### 1.2 本仓与老仓的关系

**结论：老仓代码直接并入本仓统一维护，不使用 submodule，也没有构建期外部 checkout。**

三个老仓都归属同一版权人，且都已授权按 GPL-3.0 搬运，因此"是否把老代码写进本仓 git 历史"不再是一个许可问题，只剩下纯粹的工程问题。

| 老仓 | 许可现状 | 版权人有权以 GPL-3.0 授权？ | 并入方式 |
|---|---|---|---|
| A `hongguo-monorepo` | `license=NONE`（作者本人名下，非 fork） | ✅ 是 | Python 逻辑 → 转写为 Go 进 `core/media`；实测结论作为 ADR 固化 |
| B `guoapp-android` | `license=NONE`（作者本人名下，非 fork） | ✅ 是 | Go 内核 **原样 vendor 进 `core/`**（仅删 edition 门控）；**Flutter 端（`lib/` + `android/ios/windows` 平台层）原样 vendor 进 `apps/android`** |
| C `hongguo-drama` | **GPL-3.0**（已是 GPL，无需再授权） | 已是 | xifan/hema、弹幕签名与向量测试 **直接搬运/移植** |

具体做法：

- **B（`guoapp-android`）** → 其 `native/core/*.go`（121 文件 / 24035 行 / 27 个测试文件）**整体 vendor 进 `core/`**，保留原文件形态与测试，仅做两处最小改造：① module 路径统一为 `github.com/dawei233/hongguo-next/core`；② **删除 `app_build.go` 的 `buildAllSources` 门控**（见 §4.5）。不再需要 `legacy.lock.json`、`tools/legacy/fetch.py`，也不再需要"发行包不内置 `.so`"这种规避动作 —— APK 可以直接内置 `libhncore_android.so`。**同时，B 的 Flutter 应用端**（`lib/` 84 文件 / 30558 行 Dart、`android/`+`ios/`+`windows/` 平台层、`test/`+`integration_test/`+`test_driver/`）**整体 vendor 进 `apps/android`**，同样只做最小改造：① `pubspec.yaml` 包名 `duanju_app` → `hongguo_next`（依赖原样保留，含 `media_kit` / `video_player` / `ffmpeg_kit_flutter_new_min_gpl`）；② 主题层改为 `ThemeExtension` 令牌镜像（见 `DESIGN-SYSTEM.md` §6.1）；③ FFI 侧随 `core/` 改名对齐。**安卓端 UI 不重写**：B 的 Flutter 端就是安卓端本体，`lib/core_bridge.dart` 保留为 `dart:ffi` 绑定（2 个 C 符号 ⇄ `bridge/go/main.go`）。被废弃的是**旧 monorepo（A）的 Java WebView + Media3 方案**，不是 Flutter。
- **C（`hongguo-drama`）** → 已是 GPL-3.0，与本仓同许可，**搬运即合规**。`xifan.js`/`hema.js`（135+167 行）移植进 `core/source/providers/`；`danmaku.js`/`danmaku-sign.js` 及其黄金向量测试（`danmaku-sign.test.cjs` + `scripts/fixtures/danmaku-sign-vectors.json`）直接搬用，作为本仓签名实现的**基准真值**。UI（`src/components/*`、`main.js`）仍然不搬 —— 这是产品决策（**PC 端要自己的 Electron/React UI**；安卓端 UI 沿用 B 的 Flutter 端，见 §3.2 #18），不是许可约束。
- **A（`hongguo-monorepo`）** → 作者本人自有仓，`mp4_sanitize.py`（300 行）与 `hongguo_core.py` 的 ffmpeg 解密段按原计划**转写为 Go**（这是语言迁移，不是许可绕行）；`liushen/flurl/*.py`（8.9 万行混淆产物）**仍然不搬** —— 因为 B 的 Go 签名实现已经可用，搬运混淆产物的收益为零。
- **老仓仓库本身**：保持只读冻结，作为历史参考与来源出处；新代码一律进 `hongguo-next`。
- **保留 `legacy/` 目录**，但语义从"锁文件 + 外部检出"降级为"**来源档案**"：记录每个老仓的 URL、参考 commit、搬了哪些文件、搬入路径。用于履行 GPL 的署名与来源可追溯义务，不再承担任何构建职责。

`docs/legacy/*.md` 与 `NOTICE` 的写法相应调整：不再是"未复制任何代码"的免责声明，而是**如实记录来源链路与上游署名**（见 §3.4）。

### 1.3 文档怎么写

```
docs/
├── ARCHITECTURE.md              # 本文：分层、依赖方向、边界
├── ADR/                          # 架构决策记录（每个 ADR 一个文件，编号递增）
│   ├── 0001-repo-boundary-and-legacy-integration.md
│   ├── 0002-source-plugin-model.md
│   ├── 0003-danmaku-signature-in-go.md
│   └── 0004-media-pipeline-decrypt-and-sanitize.md
├── legacy/                       # 老仓来源档案：记录 URL / commit / 搬入路径与署名
│   ├── A-hongguo-monorepo.md     # v64 实测能力清单 + 关键实测结论
│   ├── B-guoapp-android.md       # 源码规模、搬入 core/ 的 Go 清单 + 搬入 apps/android 的 Flutter 清单、buildAllSources 门控
│   └── C-hongguo-drama.md        # GPL-3.0，搬入清单（xifan/hema/弹幕签名与向量）
├── migration/
│   ├── mapping.md                # §3 的搬运映射表（可执行版）
│   └── phases.md                 # M1..M4 分批落地计划
└── contracts/
    ├── bridge-api.schema.json    # 主进程 ⇄ 内核 的 IPC 契约（唯一对外稳定面）
    └── danmaku.schema.json
```

`NOTICE`（仓库根）必须包含：来源链路、每个来源的许可状态与上游署名、搬入本仓的文件清单、以及捆绑 ffmpeg/x264 的对应源码提供义务说明（见 §3.4）。

---

## 2. 架构设计

### 2.1 目录结构

```
hongguo-next/
├── README.md
├── LICENSE                        # GPL-3.0（已就位）
├── NOTICE                         # 来源链路 + 上游署名 + 对应源码义务（见 §3.4）
├── legacy/                        # 老仓来源档案（无构建职责）
│   ├── SOURCES.md                 # 老仓 URL + 参考 commit + 搬入文件清单
│   └── README.md                  # 档案说明
│
├── contracts/                     # 跨端契约（唯一对外稳定面，无实现）
│   ├── bridge-api.schema.json
│   ├── danmaku.schema.json
│   └── source-caps.schema.json
│
├── core/                          # ★ 共享 Go 内核：PC 与 Android 共用（vendor B 的 24035 行）
│   ├── go.mod                     # module github.com/dawei233/hongguo-next/core
│   ├── go.sum
│   ├── cmd/
│   │   ├── hncore-server/         # stdio JSON-RPC 服务端（PC 端拉起子进程用）
│   │   ├── hncore-vectors/        # 弹幕签名向量校验器（对齐 C 的黄金向量）
│   │   └── hncore-fixture/        # 抓取本地 fixture，喂单测
│   ├── registry/                  # 插件注册表 + 能力查询 + 路由
│   ├── source/                    # ★ 站源层
│   │   ├── source.go              # Source 接口、Caps 位、ID 编解码
│   │   ├── registry.go
│   │   ├── resolver.go            # 跨源聚合搜索 / 剧集去重 / 别名归一
│   │   ├── providers/
│   │   │   ├── hongguo/           # B 的 app 签名 + catalog + playback + comment（原样搬入）
│   │   │   ├── huangguo/          # B 的 ai / video 两个 facet 合并（原样搬入）
│   │   │   ├── guipian/
│   │   │   ├── hanxiaoquan/
│   │   │   ├── huangdou/
│   │   │   ├── huangju/
│   │   │   ├── yeguo/
│   │   │   ├── dsd/
│   │   │   ├── cloudfront/
│   │   │   ├── sorani/
│   │   │   ├── legacy/            # 旧版接口自动发现（api host 嗅探）
│   │   │   ├── xifan/             # ★ 移植 C 的 xifan.js（135 行）
│   │   │   └── hema/              # ★ 移植 C 的 hema.js（167 行）
│   │   └── providers/*_test.go    # 含 B 的 27 个测试（原样保留）
│   ├── media/                     # ★ 媒体管线（承接 A 的实测能力）
│   │   ├── pipeline.go            # Resolve → Fetch → Decrypt → Sanitize → Probe
│   │   ├── ffmpeg.go              # ffmpeg -decryption_key K -i URL -c copy + faststart
│   │   ├── sanitize.go            # ★ MP4 净化（Go 转写 A 的 mp4_sanitize.py，300 行）
│   │   ├── boxes.go               # box 解析 / stco / co64 偏移重写
│   │   ├── hls.go
│   │   └── probe.go
│   ├── danmaku/                   # ★ 弹幕内核
│   │   ├── danmaku.go             # 分片抓取 + 合并 + identity 去重（移植 C 的 danmaku.js）
│   │   ├── webfallback.go         # 无签名的公开评论降级源
│   │   ├── layout.go              # 轨道布局（两端共享，保证视觉一致）
│   │   ├── sign/
│   │   │   ├── gorgon.go          # X-Gorgon
│   │   │   ├── ladon.go           # X-Ladon
│   │   │   ├── argus.go           # X-Argus
│   │   │   ├── sm3/sm3.go         # 纯 Go SM3
│   │   │   ├── aes/               # AES-128-CBC + PKCS#7
│   │   │   └── sign_test.go
│   │   └── testdata/vectors.json  # ★ 搬用 C 的 danmaku-sign-vectors.json 黄金向量
│   ├── device/                    # 设备注册 / 参数构造（对应 A 的 build_device_payload）
│   ├── storage/                   # 缓存 / 索引 / 追剧记录
│   └── net/                       # TLS 指纹、UA 池、重试、限速（B 的 tls-client/fhttp）
│
├── apps/
│   ├── desktop/                   # PC 端（Electron + React 19 + Vite）
│   │   ├── package.json
│   │   ├── electron-builder.yml
│   │   ├── src/main/              # 主进程：窗口、IPC、任务队列、子进程管理
│   │   ├── src/preload/           # contextBridge，暴露 window.hn
│   │   └── src/renderer/          # React 组件（新写；UI 是产品决策，不搬 C 的组件）
│   │                              # ★ 播放器：@videojs/react 10.x · createPlayer({features})
│   │                              #   （npm 包 video.js 在 v10 已是墓碑包，禁写 v8 的 videojs()）
│   └── android/                   # ★ 安卓端 = Flutter 应用，**整端 vendor 自 B**，不重建
│       ├── lib/                   # ★ Dart 层原样 vendor（B 的 84 文件 / 30558 行）
│       │   └── core_bridge.dart   #   保留 dart:ffi：2 个 C 符号 ⇄ bridge/go/main.go
│       ├── android/               # ★ Flutter 平台层：Gradle(KTS)、MainActivity.kt、
│       │                          #   AndroidManifest、res/、jniLibs/libhncore_android.so
│       ├── ios/                   # ★ Flutter 平台层（B 自带，随 vendor 进仓；非首发目标）
│       ├── windows/               # ★ Flutter 平台层（同上）
│       ├── pubspec.yaml           # 包名 duanju_app → hongguo_next；依赖原样保留
│       └── test/ · integration_test/ · test_driver/   # B 的既有测试，随 vendor 进仓
│
├── bridge/                        # 语言侧绑定
│   ├── go/                        # FFI 导出（搬 B 的 bridge/main.go 形状，2 个符号）
│   ├── dart/                      # dart:ffi 绑定（取自 B 的 lib/core_bridge.dart，唯一副本）
│   └── ts/                        # 生成的 TS 类型（从 contracts/ 生成）
│
├── tools/
│   ├── migrate/                   # 各批次搬运脚本（含 C 的 JS → Go 移植辅助）
│   └── vectors/                   # 向量校验工具（与 core/danmaku/testdata 对齐）
│
├── docs/                          # 见 §1.3
└── .github/workflows/
    ├── ci.yml                     # 主 pipeline（矩阵见 §6）
    └── nightly-sources.yml        # 联网源健康巡检
```

### 2.2 每层边界

| 层 | 允许依赖 | 禁止依赖 | 硬性约束 |
|---|---|---|---|
| `contracts/` | 无（纯 schema） | 一切 | 破坏性变更必须 bump major |
| `core/source` | `contracts`、`core/registry`、`core/net` | `apps/*`、`core/media`（仅允许通过 `Stream.ResolveHint` 间接） | provider **不得** import `core/source` 之外的任何 `core` 私有包 |
| `core/media` | `core/source`（只读接口）、`core/net` | `apps/*` | 不认识任何具体 provider 的 ID |
| `core/danmaku` | `core/source`（取 vid）、`core/net`、`core/danmaku/sign` | `apps/*` | 不做 UI，不做布局之外的渲染 |
| `core/registry` | `contracts` | 所有 provider | 只认接口 |
| `bridge/*` | `contracts`、`core` | `apps/*` 的内部类型 | 稳定 ABI（PC 走子进程 JSON，Android 走 2 个 C 符号） |
| `apps/desktop`（Electron + React）、`apps/android`（**Flutter，vendor 自 B**） | `bridge` 或 `core/cmd/hncore-server` | `core/*` 内部包 | 端侧只做渲染、交互、平台能力；安卓端**整端 vendor、不重建 UI** |

**依赖方向铁律**：`apps → bridge ⇄ core`，`core/source → core/registry`，`core/media` 与 `core/danmaku` 依赖 `core/source` 的**接口**而非具体实现。任何反向 import 由 CI lint 拦截。

### 2.3 分层图

```mermaid
flowchart TB
    subgraph Edge["端侧 apps/ · 只做渲染与平台能力"]
        D["apps/desktop<br/>Electron + React<br/>Windows / macOS"]
        A["apps/android<br/>Flutter · 整端 vendor 自 B<br/>Android arm64"]
    end

    subgraph Bridge["桥接 bridge/ · 稳定 ABI"]
        JB["bridge/ts<br/>子进程 JSON-RPC"]
        FB["bridge/dart<br/>FFI · 2 个 C 符号"]
    end

    subgraph Core["core/ · 共享 Go 内核"]
        RG["registry<br/>源路由与能力查询"]
        SRC["source<br/>Source 接口 + 13 个 provider"]
        MED["media<br/>Resolve→Decrypt→Sanitize→Probe<br/>ffmpeg + MP4 净化"]
        DM["danmaku<br/>抓取 + 去重 + 布局"]
        SG["danmaku/sign<br/>Gorgon / Ladon / Argus / SM3 / AES"]
        NET["net · device · storage"]
    end

    subgraph Contract["contracts/ · 无实现"]
        CS["bridge-api.schema.json<br/>source-caps.schema.json<br/>danmaku.schema.json"]
    end

    subgraph Legacy["legacy/ · 来源档案 · 无构建职责"]
        LA["A hongguo-monorepo<br/>实测结论 → 转写 Go"]
        LB["B guoapp-android<br/>Go 内核 → core/<br/>Flutter 端 → apps/android"]
        LC["C hongguo-drama<br/>GPL-3.0 → 直接搬运"]
    end

    D --> JB
    A --> FB
    JB --> RG
    FB --> RG
    RG --> SRC
    SRC --> NET
    MED --> SRC
    DM --> SRC
    DM --> SG
    SG --> NET

    Contract -.契约.-> JB
    Contract -.契约.-> FB
    Contract -.契约.-> RG

    LA -.转写·实测结论.-> MED
    LB -.vendor.-> SRC
    LB -.vendor.-> NET
    LC -.搬运·移植.-> SRC
    LC -.搬运·黄金向量.-> DM

    classDef legacy fill:#3a2a2a,stroke:#a33,color:#fdd
    class LA,LB,LC legacy
```

### 2.4 一次播放请求的时序（说明边界怎么落）

```mermaid
sequenceDiagram
    autonumber
    participant U as 用户
    participant R as Renderer<br/>(apps/desktop)
    participant M as Main<br/>(apps/desktop)
    participant K as hncore-server<br/>(core/cmd)
    participant G as registry
    participant P as provider/hongguo
    participant MM as core/media
    participant F as ffmpeg

    U->>R: 点播放
    R->>M: play({seriesId, episodeId, quality})
    M->>K: JSON-RPC {method:"source.Episodes"}
    K->>G: 路由 source
    G->>P: Episodes(ctx, seriesId)
    P-->>G: []Episode
    G-->>K: Episode{vid, source}
    K-->>M: 选中集
    M->>K: {method:"media.Resolve", episodeId, quality}
    K->>G: 定位 provider
    G->>P: Resolve(ctx, episodeId, quality)
    P-->>G: Stream{url, encrypted, decryptionKey?}
    G-->>K: Stream
    K->>MM: Pipeline(stream)
    alt encrypted == false
        MM-->>K: 直链 + Range 支持
    else encrypted == true
        MM->>F: ffmpeg -decryption_key K -i URL -c copy
        F-->>MM: 临时 MP4
        MM->>MM: sanitize（剥 saio/saiz/senc/sinf/encv/tenc，重写 stco/co64）
        MM-->>K: 本地可播文件
    end
    K-->>M: Playable{url, local, needsSeek}
    M-->>R: hongguo-stream:// 注册并播放
```

---

## 3. 搬运映射表

### 3.0 总体原则

- **原样 vendor**：整目录复制，0 逻辑改动，只改 module 路径与包名。**这是本仓的默认策略**。
- **移植**：跨语言逐行对应转写（JS → Go、Python → Go），保留原算法结构与常量，不重新设计。
- **最小改造**：复制后只删/改与本仓定位冲突的片段（例如 edition 门控），不改其余行为。
- **不搬**：仅限"搬过来没有收益"的部分（混淆产物、NAS 服务端形态、老 UI）。

**许可前提已全部满足**：三个老仓都由同一版权人持有并已授权按 GPL-3.0 搬运，本仓亦为 GPL-3.0（`LICENSE` 已就位）。因此映射表里**不再有"许可风险"列** —— 每个条目的唯一判据是"工作量 vs 收益"。搬运行为本身只需履行 §3.4 的署名与来源记录义务。

> 关于"去掉隔离机制"的理由：`hongguo-drama` 已捆绑 **ffmpeg 9.0.2 + x264** 并以 **GPLv3-or-later** 分发（含 x264 无法走 LGPL），**现有分发物本来就是 GPLv3 二进制**。全仓 GPL-3.0 不新增任何分发义务，却把原先为规避许可而设计的重写工作量整体归零。

### 3.1 核心能力（PC 拉流 / 解密 / MP4 净化）

| # | 源路径 | 行数 | 目标路径 | 搬法 | 说明 |
|---|---|---|---|---|---|
| 1 | `A/nas-backend/mp4_sanitize.py` | 300 | `core/media/sanitize.go` + `core/media/boxes.go` | **移植**（Python → Go，逐函数对应） | 唯一需要跨语言转写的地方。逻辑原样保留：递归容器剥 `saio/saiz/senc/sinf/encv/tenc/sgpd/sbgp/cslg`，重写 `stco/co64` 偏移。Go 侧用 `encoding/binary` 手写 box 游标，配套 golden mp4 fixture 单测。**不再以"补 LICENSE"为前置**。 |
| 2 | `A/nas-backend/hongguo_core.py`（ffmpeg 解密段 1664-1905） | ~240 | `core/media/ffmpeg.go` + `core/media/pipeline.go` | **移植 + 实测结论固化** | 参数与实测结论原样承接：`ffmpeg -decryption_key K -i URL -c copy` 产物不含 CENC 信令 box；`faststart` 后 remux 会让 `sgpd/sbgp` 回来，故必须 `sanitize` 二次净化；中间文件**必须**保留 `.mp4` 后缀让 ffmpeg 推断封装。写成 `docs/ADR/0004` + 代码注释 + fixture 测试。**注意：ffmpeg 构建本身的许可在 §3.4 处理，与本条无关**。 |
| 3 | `A/nas-backend/sources/base.py` | 392 | `core/source/source.go` | **参考 + 重写接口** | A 的 `class Source` 只有 ~80 行有效接口。本仓接口以 **B 的 Go 结构**为主干（原样 vendor 后天然对齐），A 仅提供能力位细化的参考。 |
| 4 | `A/nas-backend/liushen/flurl/*.py` | 88767+ | ❌ 不搬 | — | 8.9 万行是**代码生成/混淆产物**。搬过来收益为零：B 的 `provider_hongguo_sign.go` 已是可用的原生实现，直接 vendor 即可。**此项从"必须重写"降为"不搬"**。 |
| 5 | `A/nas-backend/liushen/device_register.py` | 243 | `core/device/register.go` | **移植**（或直接用 B 的等价实现） | 设备注册参数构造属协议事实。若 B 的 `core` 已有等价实现则以 B 为准，A 仅作交叉验证。 |
| 6 | `A/nas-backend/server.py` / `app_nas.py` | 1626 | ❌ 不搬 | — | Flask + NAS 部署形态是 A 的历史包袱，本仓不保留 NAS 服务端形态。 |
| 7 | `A/nas-backend/static/` + `A/pc/`（Electron 壳 + UI） | — | ❌ 不搬 | — | UI 全新写（产品决策，非许可约束）；Electron 主进程借鉴 IPC 通道划分思路。 |
| 8 | `A/android/android`（Java WebView + Media3） | — | ❌ 不搬 | — | **旧方案，已废弃**：这是旧 monorepo 的 Java WebView + Media3 安卓端，本仓不采用。安卓端以 **B 的 Flutter 实现**为准（见 §3.2 #18）。要区分清楚：**废弃的是 A 的 Media3 原生方案，不是 Flutter**。 |
| 9 | `A/docs/ARCHITECTURE-V2.md` 等 | — | `docs/legacy/A-hongguo-monorepo.md` | **档案** | 只做档案，记录 v64 实测能力清单。 |

### 3.2 安卓端 / Go 内核（来自 B —— 整体 vendor）

| # | 源路径 | 规模 | 目标路径 | 搬法 | 说明 |
|---|---|---|---|---|---|
| 10 | `B/native/go.mod`、`go.sum` | — | `core/go.mod` | **最小改造** | 依赖沿用以至不动（`bogdanfinn/tls-client`、`fhttp` 是 TLS 指纹刚需），module 名换为 `github.com/dawei233/hongguo-next/core`。 |
| 11 | `B/native/core/*.go`（非 provider） | **121 文件 / 24035 行** | `core/`（`net` / `storage` / `source` 基础设施） | **原样 vendor** | ★ 本次修订最大的一处变更。原先"绝不复制任何一行、分两期重写"的 24035 行，**直接 vendor 进本仓并统一维护**。只做机械替换：module 前缀、以及删掉 `app_build.go` 的门控（见 #15）。 |
| 12 | `B/native/core/provider_*.go`（hongguo/huangguo/huangdou/huangju/yeguo/dsd/sorani/guipian/hanxiaoquan/legacy） | 10 个 provider | `core/source/providers/*` | **原样 vendor** | 同上。10 个 provider 的实现、常量、端点全部保留，与本仓 `Source` 接口对齐时按 §4.1 做薄适配层，不改 provider 内部逻辑。 |
| 13 | `B/native/core/provider_hongguo_comment_sign.go` | 206 | `core/danmaku/sign/*.go` | **原样 vendor + 用 C 的向量验证** | 弹幕签名原先被列为"全仓最敏感、必须从零重写"。既然两边都是自有/GPL-3.0 授权，**直接采用 B 的实现**，并用 C 的黄金向量（#22）做回归验证 —— 这比自写一份第三实现更可靠。 |
| 14 | `B/native/core/app_danmaku.go` + `ui_playback_danmaku.go` | 33 + 185 | `core/danmaku/danmaku.go` | **原样 vendor** | 分片抓取（按 `start/duration`）+ 按 `(offset_ms, text)` 生成 identity 合并去重的逻辑保留。 |
| 15 | `B/native/core/app_build.go`（`buildAllSources` 门控） | — | **删除门控** | **最小改造** | `var buildAllSources = "false"` 及其全部引用（`app_build.go`、`app_build_test.go`、`app_runtime.go:356`、`app_sources_test.go:126`）一并删除，`core/source` 默认全量开放 13 个源（见 §4.5）。原 `-ldflags -X ...buildAllSources=true` 的 `allsources` 构建标签随之消失，CI 少一条矩阵。 |
| 16 | `B/native/bridge/main.go` | 30 | `bridge/go/main.go` | **原样 vendor** | "2 个导出符号 + JSON 字符串进出"的极简 FFI 形状（`DuanjuRequest`/`DuanjuFree`）。**B 的 Flutter 安卓端已经在用这两个符号**（`lib/core_bridge.dart` 经 `dart:ffi` 调用），因此形状原样保留即可，不需要另设计安卓侧 ABI。 |
| 17 | `B/native/core/*_test.go` | **27 文件** | 随 vendor 一并进仓 | **原样 vendor** | 原先被判为"测试是受保护表达，必须重写等价测试"，现随代码一起搬入。27 个测试文件是本仓 `core-test` 的现成回归网，**必须全绿**才能继续（见 §6）。 |
| 18 | `B/lib/`、`B/android/`、`B/ios/`、`B/windows/`、`B/test/`、`B/integration_test/` | **84 个 dart 文件 / 30558 行**（另：`android/` 32 文件、`ios/` 7 文件、`windows/` 3 文件平台层） | `apps/android/`（**整端原样 vendor**） | **原样 vendor** | ★ **本行是本次修正的重点，原判"❌ 不搬 / 由 apps/android 重建（Media3）"为错误判断。** B 是 **Flutter 应用**（`pubspec.yaml` → `name: duanju_app`，依赖 `media_kit` / `video_player` / `ffmpeg_kit_flutter_new_min_gpl` / `ffi`），**不是 Media3 原生端**。用户明确指示"安卓版用 guoapp-android 他的"，故 **`lib/` 全量 + `android/`/`ios/`/`windows/` 平台层原样搬进 `apps/android`，不重建**。仅三处最小改造：① `pubspec.yaml` 包名 `duanju_app` → `hongguo_next`；② 主题层改为 `ThemeExtension` 令牌镜像（`DESIGN-SYSTEM.md` §6.1）；③ FFI 随 `core/` 改名对齐。`ios/`+`windows/` 属 B 自带的 Flutter 平台层，随仓保留但本仓首发不做这两端。**`bridge/dart` 的 FFI 绑定即取自这里的 `lib/core_bridge.dart`**（见 §2.1），二者不得各改一份。 |
| 19 | `B/NOTICE`、`THIRD-PARTY-NOTICES.md` | — | 并入本仓 `NOTICE` | **改造** | 沿用 B 的来源链路声明格式，如实写明搬入了 `native/core/`（121 文件）与 `native/bridge/main.go`，并保留上游①`zhoufuweigg/guoapp`、上游②「鱼佬」的署名与"两跳均未声明许可证"的记录。 |

### 3.3 PC 端（来自 C，GPL-3.0 —— 直接搬运/移植）

| # | 源路径 | 规模 | 目标路径 | 搬法 | 说明 |
|---|---|---|---|---|---|
| 20 | `C/src/native/danmaku.js` | 488 | `core/danmaku/danmaku.go`（与 #14 合并） | **移植**（JS → Go） | 与 B 的 #14 是同一算法的两份实现。合并策略：以 B 的 Go 版为骨架，用 C 版补齐 B 缺失的分片/降级细节。 |
| 21 | `C/src/native/danmaku-sign.js` | 385 | `core/danmaku/sign/`（与 #13 合并） | **移植/交叉验证** | 原先"只取注释里的约束、不复制代码"。现在直接搬，并且其注释中的两条硬约束（RC4 KSA 用 `box[i]=box[j]` 而非交换；64 位运算必须 `& 0xFFFFFFFFFFFFFFFF` 掩码）**作为断言写进 `sign_test.go`**，防止后人"修好"反而搞坏签名。 |
| 22 | `C/scripts/fixtures/danmaku-sign-vectors.json` + `danmaku-sign.test.cjs` | 向量 + 241 行测试 | `core/danmaku/testdata/vectors.json` + `sign_test.go` | **原样搬用** | ★ 原先要求"自生成向量：写 3 个独立实现、2 万组随机输入交叉比对"。**该机制整条删除** —— C 的向量文件是 GPL-3.0 作品，本仓同为 GPL-3.0，直接作为基准真值搬入即可，比自造 2 万组交叉比对更省事也更可信。其 `"source": "nas-backend/danmaku.py"` 字段说明向量本身源自 A 的 Python 实现，三方同源，正好覆盖 A/B/C 三条路径。 |
| 23 | `C/src/components/*`（49 个 React 组件）+ `main.js`(3983) + `preload.js`(204) | ~16.5k 行 | ❌ 不搬 | — | **这是产品决策，不再是许可约束**：**PC 端**要自己的 Electron/React UI（这套 DOM 组件只服务单一渲染栈）。**安卓端不需要重写 UI** —— 它整端来自 B 的 Flutter 端（见 #18）。IPC 通道划分（72 个）作为设计参考记入文档。 |
| 24 | `C/src/native/xifan.js`(135)、`hema.js`(167) | 302 | `core/source/providers/xifan` / `hema` | **移植**（JS → Go，逐函数对应） | ★ 原先"从零用 Go 重写、唯一依据是外部接口行为（302 行 JS → 约 500 行 Go）"。现直接移植，保留原有的会话标识、锁定集处理与端点常量：`xifan-api-cn.youlishipin.com`、`www.kuaikaw.cn`、`xifan:<src>:<id>:<ep>` / `hema:<id>:<ep>` 的 ID 编码规则。fixture 仍从 C 的 `scripts/fixtures/hema-browse` 直接搬。 |
| 25 | `C/src/native/sm3.js`(36) | 36 | `core/danmaku/sign/sm3/sm3.go` | **移植** | SM3 是国标算法（标准文本公开），无论从哪份实现出发都要过国标测试向量。搬 C 的版本最省事，且与 B/C 的签名调用方一致。 |
| 26 | `C/docs/ADR-0001-danmaku.md` | — | `docs/ADR/0003-*.md` | **引用** | 结论（"上游的弹幕没有时间轴、不是真弹幕"）与降级源设计一并沿用。 |
| 27 | `C/src/native/licenses/*`（`LICENSE.drpys.txt` 674 行 + `NOTICE.txt`） | 674 | `third_party/drpys/` | **原样搬用** | 原先"随 C 留在 C，不进本仓"。但既然本仓搬运了依赖该算法的签名实现，**其许可证全文必须随本仓一同分发**（GPL-3.0 第 4/5 条）。同时在 `NOTICE` 记明：签名与 Spade 取址算法改编自 `woshishiq1/drpys`，经 `N3urda/hongguoTV` vendor 核对。 |
| 28 | `C` 整仓 | — | `docs/legacy/C-hongguo-drama.md` | **来源档案** | 记录 URL、`upstream=jackotom/hongguo-drama`、参考 commit `c884fab`、以及**搬入清单**（替代原先的"未复用任何代码"声明）。 |

### 3.4 许可与合规

**许可结构**

| 范围 | 许可 | 依据 |
|---|---|---|
| 本仓全部源码 | **GPL-3.0** | `LICENSE`（GPL-3.0 全文，674 行）已置于仓库根 |
| A `hongguo-monorepo` 来源代码 | 由本仓以 GPL-3.0 授权 | 版权人本人名下、`license=NONE`、`fork=false` 的自有仓，有权重新授权 |
| B `guoapp-android` 来源代码 | 由本仓以 GPL-3.0 授权 | 同上（版权人本人名下自有仓） |
| C `hongguo-drama` 来源代码 | **GPL-3.0**（原生） | 该仓本即以 GPL-3.0 授权，与本仓同许可，搬运即合规 |
| 捆绑的 ffmpeg / x264 | **GPLv3-or-later** | 见下 |

**为什么全仓 GPL-3.0 是唯一自洽的选择**

`hongguo-drama` 的 Windows/macOS 发行物已内置从源码构建的 **FFmpeg 9.0.2** 与 **x264 b35605ace3ddf7c1a5d67a2eb553f034aef41d55**，`x264` 是唯一外部编解码静态库（GPLv2-or-later），**整体构建为 GPLv3-or-later**。因含 x264 而无法走 LGPL —— 也就是说**产品线现有的分发物本来就是 GPLv3 二进制**（见 `hongguo-drama/build/ffmpeg/WINDOWS-NOTICE.txt`、`THIRD-PARTY-NOTICES.md`）。既然分发侧已经在承担 GPLv3 义务，源码侧统一为 GPL-3.0 不新增任何约束，反而消除了全部"隔离/重写"成本。

**必须保留的署名（写进 `NOTICE`）**

1. **直接上游（C 链路）**：`dawei233/hongguo-drama` ← `jackotom/hongguo-drama`（原 `hongguo-downloader`，GPL-3.0）← `zhenyong97/hongguo-downloader` ← `327044572/hongguo-downloader`。按 GPL-3.0 第 5(a) 条声明"本仓为修改版本、修改时间与主要修改内容"。
2. **直接上游（B 链路）**：`dawei233/guoapp-android` ← `zhoufuweigg/guoapp`（未声明许可证）← 「鱼佬」的短剧库 APP 源码（上游自述，未提供仓库与许可信息）。**如实记录两跳均未声明许可证**，并说明本仓为版权人自有仓、有权以 GPL-3.0 授权。
3. **直接上游（A 链路）**：`dawei233/hongguo-monorepo`，版权人自有仓，`license=NONE`。
4. **算法出处**：签名与 Spade 取址算法改编自 `woshishiq1/drpys`（GPL-3.0），经 `N3urda/hongguoTV` vendor 源码核对；许可证全文随仓分发于 `third_party/drpys/`。
5. **接口参考**：西饭（xifan）来源的接口路径核对参考 `Hululu007/drpy-node`（GPL-3.0，commit `295f2b7`）。
6. **第三方组件清单**：Electron / React / axios（MIT）、Anime4K-WebGPU（MIT）、Go 依赖（`bogdanfinn/tls-client` 等，各自许可证）。

**捆绑 ffmpeg/x264 的对应源码提供义务（怎么写）**

GPL-3.0 第 6 条要求：分发目标代码（二进制）时，必须提供或承诺提供**完整对应源码**（Corresponding Source）。本仓的落地方式：

1. **源码包随 Release 一同发布**，不单独索取。沿用 C 已有做法：文件名形如 `ffmpeg-9.0.2-x264-b35605a-win-x64-corresponding-source.tar.gz`，内含
   - FFmpeg 9.0.2 完整源码（来源 `https://ffmpeg.org/releases/ffmpeg-9.0.2.tar.xz`）
   - x264 完整源码（commit `b35605ace3ddf7c1a5d67a2eb553f034aef41d55`，来源 `https://code.videolan.org/videolan/x264.git`）
   - **精确的构建脚本**（`scripts/build-ffmpeg-source-win.sh`）
   - 配置日志、工具链版本、二进制 SHA256 哈希
2. **`NOTICE` 中写明**：对应源码的获取路径（本仓 Release 资产 + 上游官方地址双重途径），以及"可向分发者索取"的兜底渠道。
3. **许可全文随产物分发**：`COPYING.GPLv3`、`COPYING.GPLv2`、`COPYING.LGPLv2.1`、`LICENSE.md`、`WINDOWS-NOTICE.txt` / `MACOS-NOTICE.txt`、x264 许可（`WINDOWS-X264-LICENSE.txt` 由构建脚本从 x264 源码的 `COPYING` 生成，`MACOS-X264-LICENSE.txt`）、以及 Windows 侧工具链许可（`WINDOWS-MINGW-LICENSE.txt`、`WINDOWS-GCC-LICENSE.txt`）。参考 C 的做法：`scripts/build-ffmpeg-source-win.sh` 负责产出这些文件，`scripts/check-windows-release.cjs` 在打包后逐个断言存在且非空。
4. **禁止"只给上游链接"敷衍**：仅指向 `ffmpeg.org` **不满足**第 6 条 —— 必须是**与被分发二进制完全对应**的那份源码与构建脚本，因此本仓坚持自建源码包 + 记录哈希。
5. **本仓不修改 FFmpeg/x264 源码**，仅通过命令行调用 `ffmpeg.exe` / `ffprobe`（进程边界清晰），这一点在 `NOTICE` 中明示。
6. **Android 侧同理**：`apps/android` 是 B 的 Flutter 端整端 vendor，其 `pubspec.yaml` **原样保留** `ffmpeg_kit_flutter_new_min_gpl`（LGPL-3.0 插件 + GPL-3.0 编解码器 `x264/x265/xvidcore/vid.stab`），打包后整体许可为 GPL-3.0，需同样履行对应源码提供义务 —— 与本仓全仓 GPL-3.0 一致，不新增约束。

**来源可追溯性（取代原 `legacy.lock.json`）**

老仓与搬入内容记在 `legacy/SOURCES.md`（人读）与 `NOTICE`（对外声明），字段为 `{repo, ref, license, 搬入路径清单}`。**不再是构建依赖** —— 它只承担 GPL 的来源可追溯与署名义务。

```yaml
# legacy/SOURCES.md 的记录形态（示意）
A:
  repo: https://github.com/dawei233/hongguo-monorepo.git
  ref: cfb1701
  license: NONE(author-owned, re-licensed as GPL-3.0 by this repo)
  taken: [nas-backend/mp4_sanitize.py → core/media/{sanitize,boxes}.go,
          nas-backend/hongguo_core.py#1664-1905 → core/media/{ffmpeg,pipeline}.go]
B:
  repo: https://github.com/dawei233/guoapp-android.git
  ref: android/main
  upstream_chain: [zhoufuweigg/guoapp (no license), "短剧库 APP / 鱼佬 (per upstream README)"]
  license: NONE(author-owned, re-licensed as GPL-3.0 by this repo)
  taken: [native/core/** (121 files, 24035 LOC, 27 test files) → core/,
          native/bridge/main.go → bridge/go/main.go,
          lib/** (84 dart files, 30558 LOC) + android/ ios/ windows/ + test/ integration_test/ test_driver/
            → apps/android/ (整端 vendor；Flutter 应用端)]
  modified: [pubspec name duanju_app → hongguo_next;
             theme layer → ThemeExtension<HongguoTokens> token mirror]
  removed: [app_build.go buildAllSources gate]
C:
  repo: https://github.com/dawei233/hongguo-drama.git
  ref: c884fab
  upstream_chain: [jackotom/hongguo-drama, zhenyong97/hongguo-downloader, 327044572/hongguo-downloader]
  license: GPL-3.0
  taken: [src/native/xifan.js, src/native/hema.js → core/source/providers/{xifan,hema},
          src/native/danmaku.js, danmaku-sign.js → core/danmaku/**,
          scripts/fixtures/danmaku-sign-vectors.json → core/danmaku/testdata/vectors.json,
          src/native/licenses/* → third_party/drpys/]
```

---

## 4. 站源统一设计

### 4.1 Source 接口（Go）

```go
// core/source/source.go
package source

type ID string // "hongguo" | "huangguo" | ... | "hema"

// Series / Episode / Stream 是跨源统一模型，JSON 形态即为 bridge 契约。
type Series struct {
    ID        ID
    SourceID  ID
    Title     string
    Cover     string
    Year      int
    Total     int          // 集数；未知为 0
    Tag       string
    Score     string
}

type Episode struct {
    SeriesID  ID
    EpisodeID string        // 源内不透明 ID，格式 <source>:<...>
    Index     int
    Title     string
    DurationS int
}

// Stream 描述"怎么播"，不含任何源特有字段。
type Stream struct {
    URL           string
    Encrypted     bool
    DecryptionKey string      // 仅当 Encrypted
    Headers       map[string]string
    Quality       string      // "1080p" / "720p" / "auto"
    Container     string      // "mp4" | "hls"
    NeedsSanitize bool        // 见 ADR-0004：ffmpeg remux 后是否必须二次净化
    ExpiresAt     time.Time
}

// DanmakuTrack 是内核归一后的弹幕，与 UI 无关。
type DanmakuTrack struct {
    VideoID string
    Items   []Danmaku
}
type Danmaku struct {
    TimeMS int64
    Text   string
    Color  string   // "#RRGGBBAA"，缺省 "#FFFFFF"
    Mode   int      // 1=滚动 2=顶部 3=底部
    Weight int
}

type Query struct {
    Keyword  string
    Page     int
    PageSize int
    Filters  map[string]string
}

type Page struct {
    Items  []Series
    Total  int
    HasMore bool
}

// Source 是唯一的站源扩展点。实现必须无状态、可并发。
type Source interface {
    ID() ID
    Name() string
    Caps() Caps

    Search(ctx context.Context, q Query) (Page, error)
    Categories(ctx context.Context) ([]Category, error)
    Rank(ctx context.Context, board string, page int) (Page, error)

    Detail(ctx context.Context, seriesID string) (Series, error)
    Episodes(ctx context.Context, seriesID string) ([]Episode, error)

    Resolve(ctx context.Context, episodeID string, q Quality) (Stream, error)

    // 弹幕是可选能力，仅当 Caps().Has(CapDanmaku) 时被调用。
    Danmaku(ctx context.Context, episodeID string, startS, durS int64) (DanmakuTrack, error)
}
```

**设计取舍说明**
- `Resolve` **不返回文件路径**，只返回"怎么拿流"。这样 `core/media` 能统一决定 ffmpeg / 直连 / 代理，而不是每个源各写一套下载器。
- `DecryptionKey` 从 Stream 里显式带出，而不是藏在 provider 内部 —— A 的实测结论证明"是否需要二次净化"完全取决于源的加密形态，必须让媒体层知道。
- 所有可选能力用 `Caps()` 声明，不允许 provider 在不支持时返回"假装成功"。
- provider 无状态：所有缓存/登录态在 `core/storage` 或 `core/net` 共享层。

### 4.2 能力矩阵（Caps 位）

| 位 | 含义 |
|---|---|
| `CapSearch` | 支持关键字搜索 |
| `CapCategory` | 支持分类浏览 |
| `CapRank` | 支持榜单 |
| `CapRecommend` | 支持推荐流 |
| `CapMultiQuality` | 支持多清晰度选择 |
| `CapDirectPlay` | 返回可直接播放的直链（无需 ffmpeg） |
| `CapEncryptedPlay` | 需要 `-decryption_key` 解密 |
| `CapMP4Sanitize` | 解密产物需 `core/media/sanitize` 二次净化 |
| `CapHLS` | 返回 m3u8 而非 mp4 |
| `CapDanmaku` | 提供真弹幕（带时间轴） |
| `CapWebCommentFallback` | 可提供无签名评论作为降级弹幕 |

### 4.3 实际源清单（去重后 13 个）

原始来源计数：A 4 个、B 11 个 source 常量、C 3 个。

**重复情况**

| 源 | 出现在 A | 出现在 B | 出现在 C | 结论 |
|---|:--:|:--:|:--:|---|
| `hongguo` | ✅ | ✅ | ✅ | **三方重复**。单一实现，最完整。以 B 的 Go 签名为算法基线（已实测可用），叠加 A 的解密/净化实测结论。 |
| `guipian` | ✅ | ✅ | — | 二方重复，B 版本更新（Go 化 + 有 provider 测试）。 |
| `hanxiaoquan` | ✅ | ✅ | — | 二方重复，同上。 |
| `huangguo` | ✅ | ✅（`huangguoai` + `huangguo-video` 两个 facet） | — | 二方重复。**合并为一个 source，facet 变成内部枚举**，对外 ID 用 `huangguo`。 |
| `xifan` | — | — | ✅ | **C 独有**，移植 C 的 `xifan.js` 到 Go。 |
| `hema` | — | — | ✅ | **C 独有**，移植 C 的 `hema.js` 到 Go。 |
| `huangdou` | — | ✅ | — | B 独有。 |
| `huangju` | — | ✅ | — | B 独有。 |
| `yeguo` | — | ✅ | — | B 独有。 |
| `dsd` | — | ✅ | — | B 独有。 |
| `cloudfront` | — | ✅ | — | B 独有（特殊用途源）。 |
| `sorani` | — | ✅ | — | B 独有。 |
| `legacy` | — | ✅ | — | B 独有（"旧接口自动发现"，不是真站源，是 fallback 机制）。 |

**去重后最终 13 个 Source 实现**

| # | ID | 来源 | 预计 Caps |
|---|---|---|---|
| 1 | `hongguo` | B（+A 实测） | Search, Category, Rank, MultiQuality, EncryptedPlay, MP4Sanitize, Danmaku |
| 2 | `huangguo` | B（+A 合并） | Search, Category, Rank, EncryptedPlay, MP4Sanitize |
| 3 | `guipian` | B | Search, Category, MultiQuality, DirectPlay |
| 4 | `hanxiaoquan` | B | Search, MultiQuality, DirectPlay |
| 5 | `huangdou` | B | Search, MultiQuality, EncryptedPlay, MP4Sanitize |
| 6 | `huangju` | B | Search, Rank, MultiQuality, DirectPlay |
| 7 | `yeguo` | B | Search, MultiQuality, DirectPlay |
| 8 | `dsd` | B | Search, Category, MultiQuality, EncryptedPlay |
| 9 | `cloudfront` | B | DirectPlay（专用） |
| 10 | `sorani` | B | Search, MultiQuality, DirectPlay |
| 11 | `legacy` | B | Search（动态发现，作为其他源失败时的 fallback） |
| 12 | `xifan` | C 移植 | Search, Category, MultiQuality, DirectPlay |
| 13 | `hema` | C 移植 | Search, Category, MultiQuality, DirectPlay |

> 能力矩阵的**初始值是待验证的假设**，必须在 M3 阶段逐源实测后修正，修正结果写进 `core/source/source_caps_test.go` 的黄金表。

### 4.4 注册与路由

```go
// core/registry/registry.go
var providers = map[source.ID]source.Source{}

func Register(s source.Source)         // init() 自注册，重复注册 panic
func Get(id source.ID) source.Source
func Available() []source.Source        // 按 Caps().Has(CapSearch) 过滤后排序
func ResolveEpisodeID(episodeID string) (source.ID, error)  // "hongguo:12345:2" → ("hongguo", 2)
```

跨源聚合：`core/source/resolver.go` 做 `AggregateSearch(ctx, keyword)` —— 并发打所有支持搜索的源，`| 12s` 超时，按 `(规范化标题, 首播年, 集数)` 归一去重，保留 `[]SeriesID` 别名链（同一剧在多个源都有时，前端可直接切换源，这是多源架构最大的用户价值）。

```mermaid
flowchart LR
    Q["关键词 / 分类 / 榜单"] --> R["registry.Available<br/>过滤 CapSearch"]
    R --> P1["hongguo"]
    R --> P2["guipian"]
    R --> P3["xifan"]
    R --> Pn["… 共 13 个"]
    P1 & P2 & P3 & Pn --> AG["resolver.Aggregate<br/>12s 超时 · 归一去重"]
    AG --> AL["别名链<br/>hongguo:12345 ≡ hema:678"]
    AL --> U["统一 Series ID"]
    U --> M["core/media 管线"]
    M --> F["ffmpeg 解密 → MP4 净化 → 边下边播"]
```

### 4.5 与 B 的 edition 门控的差异

B 用 `-ldflags="-X .../core.buildAllSources=true"` 在构建期只暴露部分源（默认仅 `hongguo`）。**本仓删除这个门控**（vendor 时一并移除，见 §3.2 #15）：13 个源默认全开，理由是
1. 门控是为"合规/审核收缩产品面"设计的，本仓定位是自用聚合器；全仓 GPL-3.0 后更无收缩产品面的理由；
2. 门控会让 CI 与本地行为不一致，是长期 bug 源；删掉它同时消掉 CI 的一条 `allsources` 矩阵；
3. 若将来真需要，按 `config.json` 的 `enabledSources` 在**运行期**过滤，不动构建。

---

## 5. 弹幕统一设计

### 5.1 三套实现的对比与取舍

| 维度 | B（Go，206+185+33 行） | A（Python，894 行） | C（JS，488+385 行） |
|---|---|---|---|
| 语言/运行时 | Go，编译进 `.so`，零额外运行时 | Python，PC 打包 PyInstaller、安卓需额外带解释器 | Node，Electron 自带 |
| 签名覆盖 | X-Gorgon / X-Ladon / X-Argus + SM3 | 同上，且自带头部（X-Khronos / X-SS-Req-Ticket / X-SS-STUB） | 同 A |
| 正确性保障 | 有 Go 单测，但签名本身**无黄金向量** | 无向量测试 | **有黄金向量测试**（241 行 `.test.cjs` + 向量 JSON）+ 联网验证脚本 |
| 工程文档 | 中（代码注释） | 中（模块 docstring 详述来源） | **最好**（明确列出"看似 bug 但不能修"的两条硬约束） |
| 许可 | 🟢 自有仓，已授权 | 🟢 自有仓，已授权 | 🟢 GPL-3.0（与本仓同） |
| 分片抓取 | ✅ `start/duration` 分片 | 有限 | ✅ |
| UI | 无（只出数据） | 无 | ✅ 有渲染组件（不搬：产品决策，见 §3.3 #23） |

**结论与取舍**

1. **签名算法统一到 Go，实现取自 B 并在本仓统一维护**。三份实现同源（B Go → A Python → C JS），既然三方授权都已澄清，**搬用现有实现并用 C 的黄金向量回归**比再写第四份更可靠、也更省工。原先"两边都不可合法复制、只能重写 350 行纯算术"的前提已不成立。
2. **C 的工程知识连同其代码一并继承**，其注释里的两条硬约束**作为断言写进本仓测试**，防止后人"修好"反而搞坏签名：
   - `TestXGorgonKSAMustNotSwap`：RC4 类 KSA 必须复现 `box[i] = box[j]` 的**非交换**写法（改成标准交换会导致服务端 4xx）。
   - `Test64BitTruncation`：所有 64 位运算必须显式 `& 0xFFFFFFFFFFFFFFFF` 掩码，位宽改变即为签名失效。
3. **正确性保障用 C 的黄金向量，不再自建三实现交叉比对**。`C/scripts/fixtures/danmaku-sign-vectors.json`（其 `source` 字段标注源自 `nas-backend/danmaku.py`）+ `danmaku-sign.test.cjs`（241 行）**原样搬入** `core/danmaku/testdata/` 与 `sign_test.go`。该向量覆盖 A 的 Python 实现路径，与 B 的 Go 实现做交叉回归 —— 等价于原先"三实现交叉"的验证强度，但无需重写三份代码。联网冒烟测试（nightly）保留。
4. **弹幕渲染按端落地**：PC 端自己写 DOM/Canvas 渲染；**安卓端沿用 B 的 Flutter 弹幕层**（`lib/danmaku_overlay.dart` / `danmaku_controller.dart` 等，随 vendor 整端进仓，不重写）。不搬 C 的 `OfficialDanmaku.jsx` / `danmakuOfficial.mjs` 是**产品决策**（这套 DOM 组件服务不了 Electron 之外的渲染栈），不是许可限制。

### 5.2 弹幕管线

```mermaid
flowchart TB
    subgraph Sign["core/danmaku/sign · 纯算术 · 无 IO · 100% 覆盖单测"]
        G["X-Gorgon<br/>gorgon.go"] --> L["X-Ladon<br/>ladon.go"]
        G --> AR["X-Argus<br/>argus.go"]
        AR --> S3["SM3<br/>sm3/sm3.go"]
        AR --> AE["AES-128-CBC<br/>aes/"]
    end

    subgraph Fetch["core/danmaku · IO"]
        P["按 start/dur 分片并发抓取<br/>POST /novel/commentapi/comment/list/{vid}/v1/"]
        M["合并 · identity 去重<br/>(offset_ms, text)"]
        T["校验：数量/时间轴单调性/空结果告警"]
    end

    subgraph Lay["core/danmaku/layout.go · 与 UI 无关"]
        L2["密度 · 区域 · 屏蔽词<br/>轨道分配（防重叠）"]
    end

    subgraph Render["各端渲染 · PC 新写 / 安卓沿用 B 的 Flutter 层"]
        RD["desktop: DOM/Canvas"]
        RA["android: Flutter CustomPaint 自绘<br/>(B 的 danmaku_overlay.dart)"]
    end

    subgraph Fallback["降级源 · 无签名"]
        W["公开网页评论<br/>无 offset_time"]
    end

    SG(("Source<br/>Caps:CapDanmaku")) --> P
    P --> M --> T --> L2
    L2 --> RD
    L2 --> RA
    T -.空/失败.-> W
    W --> L2

    Sign -.生成请求头.-> P

    classDef hi fill:#2a1f3a,stroke:#a3a,color:#fdf
    class G,L,AR,S3,AE hi
```

**分层要点**
- `sign/` 是纯函数、无 IO、无时间依赖 → 可 100% 单测、可生成黄金向量。
- `layout.go` 放内核而非各端：**保证 PC 与安卓弹幕视觉一致**（轨道分配是算法，不是渲染），两端只负责画。
- 降级源 `webfallback.go` 保留（它是 C 上游那条路的价值：无签名、公开网页、拿不到时间轴但不会 404），仅在 `CapWebCommentFallback` 且官方源为空时启用，并在 UI 上标注"非时间轴评论"。

### 5.3 弹幕签名接口

```go
// core/danmaku/sign/sign.go
type Params struct {
    RawQuery  string
    StampS    int64
    StampMS   int64
    Aid       string
    DeviceID  string
    InstallID string
    Nonce     [11]byte
}

type Headers struct {
    XArgus        string
    XLadon        string
    XGorgon       string
    XKhronos      string
    XSSReqTicket  string
    XSSStub       string
}

func SignComment(p Params) (Headers, error)
```

---

## 6. CI 与发布

### 6.1 一条 pipeline 的矩阵

```yaml
name: ci
on: { push: {branches: [main]}, pull_request:, workflow_dispatch: }

jobs:
  # ── 门禁层（PR 必须全绿） ──────────────────────────────
  core-lint:
    strategy: { matrix: { go: ['1.24.x'] } }
    steps: [gofmt -l, go vet, golangci-lint, 依赖方向 lint]
  core-test:
    strategy:
      fail-fast: false
      matrix:
        os:   [ubuntu-latest, windows-latest, macos-latest]   # ★ Windows 必跑
        go:   ['1.24.x']
    steps: [go test -race ./...]   # 全部用本地 fixture，禁止联网
                                   # 含 vendor 进来的 B 的 27 个测试文件
  danmaku-vectors:
    steps:
      - core/danmaku/testdata/vectors.json 与实现逐字节一致（搬自 C，只允许显式更新）
      - go test ./core/danmaku/...
  desktop-build:
    strategy: { fail-fast: false, matrix: { os: [windows-latest, macos-latest] } }
    steps: [pnpm i → pnpm build → tsc --noEmit → playwright smoke,
            # ★ 播放器门禁：video.js v10 的 npm 包 video.js 是墓碑包（throw VJS8_LEGACY_*）
            #   必须用 @videojs/react 10.x 的 createPlayer({features}) 组合式 API；
            #   grep -rn "videojs(" src/ 命中即失败
            ]
  flutter-test:                  # ★ B 的 Dart 测试随 vendor 一并进仓并跑
    steps: [flutter pub get → flutter analyze → flutter test]
  android-build:                 # ★ 安卓端 = Flutter（整端 vendor 自 B），不是 Gradle 原生工程
    strategy:
      fail-fast: false
      matrix:
        abi: [arm64-v8a, armeabi-v7a]
    steps: [flutter build apk --release → ABI 校验 → APK 签名校验
            → 断言 jniLibs 内含 libhncore_android.so]

  # ── 产物矩阵 ────────────────────────────────────────
  core-lib:            # 共享内核多平台产物
    strategy:
      matrix:
        include:
          - { goos: windows, goarch: amd64,  out: lib/hncore.dll }
          - { goos: darwin,  goarch: arm64,  out: lib/libhncore.dylib }
          - { goos: linux,   goarch: amd64,  out: lib/libhncore.so }
          - { goos: android, goarch: arm64,  out: lib/libhncore_android.so, ndk: true }
    steps: [go build -buildvcs=false -trimpath ...]

  release:            # 仅 tag v* 触发
    needs: [core-lib, desktop-build, flutter-test, android-build]
    steps:
      - 归档 + checksum + GitHub Release
      - 产物内嵌 NOTICE / LICENSE
      - ★ 同时发布 ffmpeg+x264 对应源码包（GPL-3.0 第 6 条义务，见 §3.4）

  # ── nightly（不阻塞） ────────────────────────────────
  danmaku-live-nightly:
    schedule: cron
    steps: [对官方弹幕接口做一次联网冒烟，验证签名仍然有效]
```

> **已删除的 job：`license-guard`。** 原先它做三件事：① `tools/legacy/fingerprint.py` 对本仓跟踪文件跑归一化哈希 + 最长公共 token 序列克隆检测，确保不含 C 的任何字节；② `tools/legacy/lock_check.py` 校验 `legacy.lock.json` 的 commit 可达且未被篡改；③ `reuse lint / license-eye` 生成第三方许可清单。前两项的存在前提（"本仓不得含 GPL/老仓代码"）已不成立 —— 本仓就是 GPL-3.0，且**明确搬运**了老仓代码；第三项降级为 `release` 里的人工维护清单（`NOTICE`）。同时删除的还有 `-tags allsources` 那条矩阵（门控已删，见 §3.2 #15）与 `sources-nightly`（合并进 `danmaku-live-nightly` 的巡检脚本）。

### 6.2 矩阵排布的取舍

- **PR 门禁只跑 Ubuntu + 1 个 Go 版本 + desktop-windows**：保证反馈 < 8 分钟。macOS 矩阵放到 `main` 分支合并后跑，不阻塞 PR。
- **`core-test` 必须覆盖 `windows-latest`，且是硬要求**：`core/media/boxes.go` 是字节级操作，Windows 的换行/路径/`os.Rename` 语义差异会真实咬人；A 的 `mp4_sanitize.py` 在 Windows 上就是主力路径。这条矩阵**不能省**。三个 OS 都保留，但 Windows 是唯一"缺了就发不出货"的。
- **vendor 进来的 27 个 B 测试随 `core-test` 一起跑**：它们是本次修订后**最大的既有资产**，也是"vendor 没有搬坏"的第一道证据。任何为对齐本仓接口而做的改动，必须先让这 27 个测试保持通过。
- **`danmaku-vectors` 从"三实现生成器"降级为"一致性校验"**：向量文件搬自 C（自身又源自 A 的 Python 实现），不再需要 Go/Python/Node 三实现交叉比对 20000 组。CI 只校验"实现与冻结向量逐字节一致"，防止有人改动签名实现后忘记更新向量。
- **`danmaku-live-nightly` 保留且不可省**：签名是唯一会**静默过期**的部分（上游算法不变但 nonce/时间戳规则可能变），必须每日探活并告警，不能只靠编译期测试。
- **`release` 新增"对应源码包"步骤**：这是全仓 GPL-3.0 之后**唯一新增的强制义务**，必须在流水线上自动化，不能靠人工记得打。
- **`flutter-test` 与 `android-build` 是"vendor 没搬坏"的第二道证据（第一道是 27 个 Go 测试）**：B 的 `test/`+`integration_test/` 随仓进 CI，`android-build` 用 `flutter build apk --release`（**不是** `gradle assembleRelease` —— 安卓端是 Flutter 工程，不是 Gradle 原生工程），并在打包后断言 `jniLibs` 内确实含 `libhncore_android.so`（证明 FFI 内核真的被装进了 APK）。
- **`desktop-build` 加一条播放器门禁**：`grep -rn "videojs(" src/` 必须为空 —— 防止有人照 video.js 8 教程写出必然抛 `VJS8_LEGACY_*` 的代码（见 §0 第 7 条）。

### 6.3 发布物矩阵

| 产物 | 平台 | 形态 | 体积主要来源 |
|---|---|---|---|
| `hongguo-next-setup.exe` / portable | win x64 | Electron + 内置 `hncore.dll` + ffmpeg | ffmpeg (~80MB) |
| `.dmg` | macOS arm64/x64 | Electron + `libhncore.dylib` + ffmpeg | 同上 |
| `app-arm64-v8a.apk` / `universal.apk` | Android 8+ | **Flutter（B 的端，整端 vendor）+ `libhncore_android.so`**（**可直接内置**，见 §1.2） | Flutter 引擎 + `libhncore_android.so` |
| `hncore-*` 四平台动态库 | — | 供集成方使用 | 小 |
| `ffmpeg-9.0.2-x264-b35605a-<platform>-corresponding-source.tar.gz` | 全部 | **GPL-3.0 第 6 条对应源码包**（FFmpeg + x264 源码、构建脚本、配置日志、哈希） | ~100MB |

---

## 7. 分批落地计划（M1–M4，工期已按搬运策略重算）

**总量变化**：原先的 M0（许可前置：给 A 补 LICENSE、向上游发许可邮件）**整段消失**；M1/M2/M3 因改为搬运而大幅缩水；**M4 中的安卓端也从"重建"改为"整端 vendor B 的 Flutter 应用"**（12–16 → 9–13 人日，省下的正是"重写安卓 UI + 从零搭 Android 播放器"这部分），只有 PC 端 UI 与播放器仍是新写。**净工作量从约 60–80 人日降到约 22–33 人日。**

| 阶段 | 内容 | 完成判据 | 估计工作量 |
|---|---|---|---|
| **M1 契约与 vendor**（原 M0 许可工作已消失，直接从这里开工） | ① `B/native/core/**`（121 文件 / 24035 行）+ `bridge/main.go` 原样 vendor 进 `core/`，module 前缀替换；② 删除 `buildAllSources` 门控及其全部引用；③ 27 个测试跑通；④ `contracts/*.schema.json`、`core/source` 接口与 Caps 薄适配层（把 B 的 source id 与 `Drama`/`Task` 形状对到 §4.1 的 `Series`/`Episode`/`Stream`） | `go vet`/`go test -race` 三平台全绿；27 个测试无跳过 | **5–7 人日** |
| **M2 弹幕** | 搬 C 的 `danmaku.js`/`danmaku-sign.js`（JS→Go 移植）并与 B 的实现合并；搬 `danmaku-sign-vectors.json` + `danmaku-sign.test.cjs` 为 `core/danmaku/testdata/` 与 `sign_test.go`；`sm3.js` 移植；`layout.go` 与降级源 | 向量逐字节一致；nightly 直播冒烟通过 | **3–5 人日** |
| **M3 站源与媒体** | ① 10 个 B provider 随 vendor 到位，仅做接口对齐与 Caps 实测校准；② `xifan.js`/`hema.js` 移植（302 行 → Go）；③ `mp4_sanitize.py` 移植为 `core/media/{sanitize,boxes}.go`；④ ffmpeg 解密链路 + 边下边播 | 每源 fixture 单测通过 + 实际播放成功；一集 1080p HEVC 加密档从抓到播全链路通过 | **5–8 人日** |
| **M4 端侧** | `apps/desktop`（Electron + React 19，含**新写 UI 与 `@videojs/react` 10.x 播放器**）、`apps/android`（**整端 vendor B 的 Flutter 端**：`lib/` 84 文件 / 30558 行 + `android/ios/windows` 平台层 + `pubspec` 改名 + 令牌镜像；FFI 走 `bridge/dart`，**不重建 UI**）、发布流水线（含 ffmpeg 对应源码包） | 双端各发布一个可安装包；`flutter analyze`/`flutter test` 全绿；PC 端播放器不出现 `videojs()` v8 写法；安装包内含 NOTICE/LICENSE/源码包 | **9–13 人日** |

**依赖方向图**：`M1 → (M2 ∥ M3) → M4`。

**新关键路径**：`B 内核 vendor 并跑通 27 测试` → `core/media 移植（sanitize + ffmpeg 解密）` → `端侧（PC 播放器 + Flutter 端 vendor 对齐）`。

安卓端的 Flutter 端 vendor **不在关键路径的新增项上**：它是整端搬运 + 三处最小改造（包名、令牌镜像、FFI 改名），而不是"重写 + 对齐"；风险从"能不能写出来"进一步降为"vendor 能不能编过、测试能不能过"。

原先的关键路径是"重写弹幕签名三实现 → 重写 13 个 provider → 重写媒体管线"，全是**从零实现**的串行长链，任何一环失败都会拖垮全局。现在关键路径上**只剩一项真正的开发工作**：Python → Go 的 MP4 净化移植（300 行），其余节点都是"搬运 + 对齐 + 验证"。这也是本次修订最大的收益 —— **风险从"能不能写出来"降为"搬过来能不能跑通"**。

---

## 8. 已知风险

| 风险 | 等级 | 缓解 |
|---|---|---|
| vendor 进来的 24035 行 Go 与本仓接口对齐时引入回归 | 🟡 中 | 27 个 B 测试随代码一并搬入并作为门禁；接口对齐只加薄适配层，不改 provider 内部逻辑 |
| **B 的 Flutter 端（84 文件 / 30558 行 Dart）vendor 后编不过、或与令牌表漂移** | 🟡 中 | **整端 vendor 而非重建**；`flutter analyze` + `flutter test` + `integration_test/`（B 自带）随仓进 CI；只改三处（`pubspec` 包名、令牌镜像、FFI 改名）。令牌用 `ThemeExtension<HongguoTokens>` 逐项镜像同一张表（`DESIGN-SYSTEM.md` §6.1），**不切 Compose、不重写页面**（设计文档 §7 决策表第 4 条已确认） |
| **PC 端播放器误按 video.js 8 教程实现**：npm 包 `video.js` 在 v10 已是墓碑包，`videojs()`/`registerPlugin`/`registerComponent` 全部 `throwLegacyError('VJS8_LEGACY_*')` | 🟡 中 | 统一用 **`@videojs/react` 10.x 的 `createPlayer({features})` 组合式 API**；CI 增加 `grep -rn "videojs(" src/` 必须为空的门禁；结论记在 `DESIGN-SYSTEM.md` §1.3 |
| 安卓端沿用 B 的 Flutter 端 → 与 PC 端出现视觉漂移 | 🟡 中 | 两端共用同一张令牌表（OKLCH 为权威值，HEX 供 Flutter `Color(int)`），由 `pnpm check:tokens` 校验单一来源；弹幕轨道分配放内核 `layout.go`，保证跨端一致 |
| Python → Go 的 `mp4_sanitize` 移植出现字节级偏差 | 🟡 中 | 用 A 的 golden mp4 fixture 做逐字节对比；`core-test` 三平台矩阵 |
| 弹幕签名算法被上游改动而静默失效 | 🔴 高 | `danmaku-live-nightly` 每日探活 + 告警；`ADR-0003` 记录两条已知"陷阱"并写成断言 |
| 全仓 GPL-3.0 → 未来无法闭源或与闭源方合作 | 🟡 中 | **这是已明确接受的取舍**（版权人本人决定；且现有分发物因捆绑 x264 本就是 GPLv3 二进制，实为对齐现实）。若将来需要，只能对**新写的 PC 端代码**做双许可；`core/` 与 vendor 自 B 的 Flutter 端都无法回退 |
| ffmpeg/x264 对应源码未随 Release 发布 → 违反 GPL-3.0 第 6 条 | 🔴 高 | `release` job 强制产出对应源码包并在 `NOTICE` 写获取路径；**只给上游链接不满足义务**，必须是与被分发二进制完全对应的源码 + 构建脚本 + 哈希 |
| 老仓上游第三人（`zhoufuweigg/guoapp`、「鱼佬」）主张权利 | 🟡 中 | `NOTICE` 如实记录来源链路与"两跳均未声明许可"的事实，不做虚假许可声明；保留联系与配合下架渠道 |
| 源方接口频繁变更 → 13 源维护负担 | 🟡 中 | 统一 `Source` 接口让每个源改动面 < 300 行；nightly 巡检定位失效源 |
