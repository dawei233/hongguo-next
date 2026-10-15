# 来源档案与上游跟踪台账

> **性质**：人读档案。**无构建职责** —— 本仓不通过 submodule、锁文件或构建期外部 checkout 引用任何来源仓。
> **用途**：① 履行 GPL-3.0 的署名与来源可追溯义务；② 承担**上游跟踪义务**（见 [`../docs/ARCHITECTURE.md`](../docs/ARCHITECTURE.md) §1.3）。
> **何时更新**：每次 vendor/移植时登记；每里程碑收口与每月各评估一次上游提交（见 §跟踪节奏）。

## 来源结构：三层

**只有自有层真正停止了开发。两个上游仍在活跃开发，因此本仓对其负有持续跟踪义务。**

| 层 | 仓库 | 状态 | 本仓如何取得 |
|---|---|---|---|
| **上游** | [zhoufuweigg/guoapp](https://github.com/zhoufuweigg/guoapp) | 🟢 仍在开发 | 未直接并入。经本方 fork 间接承接，**须跟踪** |
| **上游** | [jackotom/hongguo-drama](https://github.com/jackotom/hongguo-drama) | 🟢 仍在开发（GPL-3.0） | 未直接并入。经本方 fork 间接承接，**须跟踪** |
| 本方 fork | [dawei233/guoapp-android](https://github.com/dawei233/guoapp-android) | ⚪ 已停止由本方维护 | vendor（**已决：直接采用**，上游未声明许可 → 署名 + 响应通道，见 [`../NOTICE`](../NOTICE) 第三节） |
| 本方 fork | [dawei233/hongguo-drama](https://github.com/dawei233/hongguo-drama) | ⚪ 已停止由本方维护 | 搬运 / 移植 |
| **自有** | [dawei233/hongguo-monorepo](https://github.com/dawei233/hongguo-monorepo) | ⚫ 已停止开发 | 移植（Python → Go） |

---

## 台账

字段说明：`repo_ref` 与 `upstream_ref` **必须写 commit SHA，不写分支名**（分支会移动）。`license` 如实记录原文，`NONE` 也照写，不做推断。

```yaml
B:
  layer: fork                      # 本方 fork，其上游活跃
  repo: https://github.com/dawei233/guoapp-android.git
  repo_ref: ff3bad2                # android-only 分支 HEAD（本方 57 个提交之首）
  upstream_repo: https://github.com/zhoufuweigg/guoapp.git
  upstream_ref: 0e60928            # 上游 main；已被 repo_ref 完全包含
  upstream_status: active          # 最后核实 2026-10-15：916★ / 10 open issue / 未归档
  upstream2:                       # 上游① 自述的原始来源；本仓无法进一步核实（链条终点）
    name: 短剧库 APP               # 网上通称「鱼佬」；未提供原始仓库
    repo: null
    license: NONE                  # 未提供许可证信息；源码来自互联网，原仓与许可均无法核实
  license: NONE                    # 上游未声明许可证；本方仅授权本人新增/修改的部分
  adoption: direct                 # 已决（非待办）：直接采用 + 完整署名 + 响应通道；授权范围见 NOTICE 第三节
  taken:
    - native/core/**        → core/          # 121 文件 / 24035 行 / 27 测试
    - native/bridge/main.go → bridge/go/main.go
    - lib/** + android/ ios/ windows/ + test/ + integration_test/ + test_driver/
                            → apps/android/  # 84 dart 文件 / 30558 行，整端 vendor
  modified:
    - "pubspec name: duanju_app → hongguo_next"   # 引号必需：含 ": " 否则会被 YAML 解析成嵌套映射
    - theme layer → ThemeExtension<HongguoTokens>   # 待 M4
    - "core/go.mod module: duanjuapp/native → github.com/dawei233/hongguo-next/core"
    - "bridge/go/main.go import 路径随之改写；C 符号名与 JSON 进出形状不变（ABI 冻结）"
    - "core/app_build_test.go 断言按无门控语义重写；测试名 …RestrictsScheduling → …SchedulesAllSources"
    - "core/app_sources_test.go 去掉 buildAllSources 的 t.Skip"
  removed:
    - app_build.go buildAllSources 门控
  added:                           # hongguo-next 新写（非上游，M1 补齐）
    - core/app_storage_windows.go   # 上游只有 !windows 一份，内核无法编到 Windows；补齐 GetDiskFreeSpaceEx
    - core/proxy_system_windows.go  # 同上；补齐 WinINET 每用户代理设置读取
  verified:                        # 2026-10-15 M1 实测
    - "build+vet: linux/{amd64,arm64} windows/{amd64,arm64} darwin/{amd64,arm64} 全绿"
    - "go test -race: 99 个测试函数 / 98 通过 / 1 跳过（仅 live 网络探活，opt-in）/ 0 失败"
    - "bridge cgo 构建导出 2 个符号：DuanjuRequest / DuanjuFree"
    - "环境注记：需 ffmpeg 在 PATH 才能跑加密 HLS 解码两测；johnvansickle 静态 7.0.2 的 HLS demuxer 在本容器段错误，改用 BtbN gpl 构建后通过"
    - "vendor 保真：hls_playlist_detect_test.go 与 hongguo_finalize_test.go 上游即未 gofmt，本仓逐字节保真保留，未擅自格式化"
  last_triaged: 2026-10-15
  backports: []                    # [{sha: <upstream-sha>, why: 站源失效修复, at: <date>}]

C:
  layer: fork
  repo: https://github.com/dawei233/hongguo-drama.git
  repo_ref: c884fab                # 本方 fork main HEAD
  upstream_repo: https://github.com/jackotom/hongguo-drama.git
  upstream_ref: 5a12c88            # 上游 main；是 repo_ref 的祖先
  upstream_status: active          # 最后核实 2026-10-15：233★ / 19 open issue / 未归档
  license: GPL-3.0                 # 与本仓同许可
  taken:
    - src/native/xifan.js (135), hema.js (167) → core/source/providers/{xifan,hema}   # M3 已移植；hema 另搬 3 个 __NEXT_DATA__ 页面快照
    - src/native/danmaku.js (488), danmaku-sign.js (385), sm3.js (36) → core/danmaku/  # M2 已移植
    - "scripts/fixtures/danmaku-sign-vectors.json → core/danmaku/testdata/vectors.json"
    - components/danmakuModel.mjs, danmakuOfficial.mjs, danmakuSource.mjs → core/danmaku/layout.go
    - src/native/licenses/*                   → third_party/drpys/
  verified:                        # 2026-10-15 M2 实测
    - "向量逐字节搬用：本仓 vectors.json 与 C 的 fixtures sha256 全等（c7308e2da6c7484e…），由主会话独立复核"
    - "三组向量的请求头与 query 全部逐字节一致；SM3 11 种长度、64 组随机输入与扁平包既有实现交叉一致"
  divergence:                      # 已知待收敛，非缺陷隐瞒
    - "X-SS-Req-Ticket：黄金向量要求「秒×1000」（毫秒位恒 000，第 3 组 1234567890123 专为抓此差异而设）；扁平 core/provider_hongguo_comment_sign.go:58 与 provider_hongguo_sign.go:46 用 now.UnixMilli()（毫秒位非零），属偏离。新包从向量（正确），扁平包待拆包时收敛。"
  last_triaged: 2026-10-15
  backports: []

A:
  layer: own                       # 版权人自有、已冻结、无上游
  repo: https://github.com/dawei233/hongguo-monorepo.git
  repo_ref: cfb1701
  upstream_repo: null
  upstream_ref: null
  upstream_status: no-upstream     # 非 fork；已停止开发（最后推送 2026-10-11）
  license: NONE                    # 自有仓，版权人以 GPL-3.0 授权
  taken:
    - nas-backend/mp4_sanitize.py (300)  → core/media/{sanitize,boxes}.go   # M3 已移植，11 组 golden fixture 字节级对齐
    - nas-backend/sources/media.py, server.py 的 ffmpeg 解密调用 → core/media/{ffmpeg,pipeline,hls,probe}.go
    - "nas-backend/danmaku.py (894) 的算法结论 → 经 C 的黄金向量间接承接（见 C 段 divergence）"
  not_adopted:                     # 实测后有意不沿用，非遗漏
    - "server.py:1104 解密失败时净化加密原文件兜底 —— 实测该产物解码吐垃圾，本仓改为解密失败即失败"
    - "liushen/（88767 行代码生成产物）—— 签名改用 B 的 Go 实现 + C 的向量校验，不移植"
  verified:                        # 2026-10-15 M3 实测（主会话独立复核）
    - "11 组 golden fixture；其中 10 组与重新生成的 Python 输出逐字节一致"
    - "real_cenc_decrypted 组输入因 ffmpeg 解密产物不确定而每次不同（输入 sha256 变、输出尺寸恒 93263→91284）；已用「新生成输入喂 Go、与同输入 Python 输出比」的决定性验证确认逐字节一致"
    - "新发现：ffmpeg 不校验 -decryption_key（错密钥同样退出 0、产物尺寸与 ffprobe 全绿，只有真解码吐垃圾）→ Probe 阶段已加解码验收"
  last_triaged: n/a
  backports: []
```

> ⚠️ 上表中 `taken` / `modified` 的目标路径是**规划值**，实际路径以 `docs/ARCHITECTURE.md` §3 搬运映射表为准；执行搬运时须回填真实路径与真实 `repo_ref`。

---

## 跟踪节奏

- **频率**：每里程碑收口 + 每月一次。
- **动作**：`git fetch` 上游后检视 `upstream_ref..upstream/main`，逐条判断是否值得回移。
- **回移优先级**：
  1. 站源失效修复 ≈ 弹幕签名调整（最高 —— 这两类会**静默过期**，不修就是坏源）
  2. 播放修复
  3. 崩溃 / 并发修复
  4. 新特性
- **回移方式**：`git cherry-pick -n` 后按本仓结构改造，并在上方 `backports` 追加 `{sha, why, at}`。
- **明确不做**：
  - 不用 merge / rebase 上游的方式同步 —— 本仓是**整合重写**，不是任何 fork 的直接延续；
  - 不回推上游。

## 评估日志

| 日期 | 来源 | 上游新增提交 | 处理 | 备注 |
|---|---|---|---|---|
| 2026-10-15 | B（guoapp） | 基线建立 | — | 初始登记 `0e60928` ↔ `ff3bad2` |
| 2026-10-15 | C（hongguo-drama） | 基线建立 | — | 初始登记 `5a12c88` ↔ `c884fab` |
