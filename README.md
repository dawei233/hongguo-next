# 红果 Next · hongguo-next

> 一个自托管的多源短剧聚合器：**Windows / macOS 桌面端 + Android 端**，共用一套 Go 内核。
> 搜索、播放、追剧、真弹幕、下载合并，全部本地完成。

---

## 这是什么

`hongguo-next` 是既有项目的**整合重写**：可用实现**并入本仓统一维护**。来源分三层，**必须区分清楚 —— 只有自有层真正停止了开发，两个上游仍在活跃开发**：

| 层 | 仓库 | 状态 | 角色 | 去向 |
|---|---|---|---|---|
| **上游** | [zhoufuweigg/guoapp](https://github.com/zhoufuweigg/guoapp) | 🟢 **仍在开发**（最后推送 2026-09-26 · 916 ★ · 10 open issues · `license=NONE`） | 安卓端 / Go 内核的**原始上游** | 未直接并入；本仓经本方 fork 间接承接，**须持续跟踪** |
| **上游** | [jackotom/hongguo-drama](https://github.com/jackotom/hongguo-drama) | 🟢 **仍在开发**（最后推送 2026-10-15 · 233 ★ · 19 open issues · **GPL-3.0**） | 西饭/河马站源、弹幕签名与黄金向量的**原始上游** | 未直接并入；**须持续跟踪** |
| 本方 fork | [dawei233/guoapp-android](https://github.com/dawei233/guoapp-android) | ⚪ **已停止由本方维护**（`android-only` HEAD `ff3bad2`，含本方 57 个提交） | 安卓端与 Go 内核的**实现来源** | Go 内核（121 文件 / 24035 行 / 27 测试）**原样 vendor** 进 `core/`；Flutter 端整端 vendor 进 `apps/android` |
| 本方 fork | [dawei233/hongguo-drama](https://github.com/dawei233/hongguo-drama) | ⚪ **已停止由本方维护**（`main` HEAD `c884fab`） | 西饭/河马站源、弹幕签名与黄金向量（GPL-3.0） | **直接搬运/移植**，UI 不搬（产品决策） |
| **自有** | [dawei233/hongguo-monorepo](https://github.com/dawei233/hongguo-monorepo) | ⚫ **已停止开发**（最后推送 2026-10-11 · `license=NONE` · **非 fork**） | PC 端拉流 / 解密 / MP4 净化的**实测能力来源** | `mp4_sanitize.py` 与 ffmpeg 解密链路**移植**为 Go |

- **自有层 `hongguo-monorepo` 是唯一真正停止开发的一个**：版权人自有项目，非 fork，已冻结，仅作历史存档。
- **两个上游仍在活跃开发**：它们会持续产生新提交（站源失效修复、播放修复等），而本方两个 fork 已停更 —— 因此本仓对上游负有**持续跟踪义务**：定期评估上游提交、按需定向回移。规则见 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) §1.3「上游跟踪义务」。
- 本仓**不是任何来源 fork 的直接延续**，而是整合重写，因此**不以 merge/rebase 上游的方式同步**，只做定向回移。

## 特性（目标）

- **13 个站源**统一在一个 `Source` 接口下：红果、黄果、鬼片、韩小圈、黄豆、黄剧、夜果、dsd、cloudfront、sorani、legacy，加两个从本方 fork [dawei233/hongguo-drama](https://github.com/dawei233/hongguo-drama) 移植的西饭、河马。
- **跨源聚合搜索**：一次搜索打所有源，按标题/年份/集数归一去重，同一部剧可一键切换播放源。
- **真弹幕**：带时间轴的官方弹幕（`offset_time`），签名内核用 Go 实现，用搬入的黄金向量做回归 + 每日联网探活。
- **加密档直解**：`ffmpeg -decryption_key` 解密 + 自研 MP4 净化（剥 CENC 信令 box、重写 `stco/co64` 偏移），让严格 demuxer 也能播。
- **两端一致**：`core/` 是唯一的实现来源，PC 与 Android 只做渲染与平台能力。

## 目录

```
core/            共享 Go 内核：站源 / 媒体管线 / 弹幕 / 签名 / 网络 / 存储
apps/desktop/    PC 端：Electron + React
apps/android/    安卓端：Media3 + Gradle
bridge/          语言侧绑定（TS / Dart FFI / Go FFI）
contracts/       跨端 JSON 契约（唯一对外稳定面，无实现）
legacy/          来源档案（上游 / 本方 fork / 自有 的 URL、commit、搬入清单与跟踪状态，无构建职责）
docs/            架构、ADR、来源档案、迁移计划
tools/           搬运脚本、向量校验工具
```

架构分层、依赖方向铁律、能力矩阵与 CI 矩阵，见 **[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)**。

## 当前状态

**架构设计阶段，尚未开工写实现代码。** 首批落地顺序见 `docs/ARCHITECTURE.md` §7（M1 vendor 与契约 → M2 弹幕 ∥ M3 站源与媒体 → M4 端侧）。

## 许可与合规

本仓库以 **GNU General Public License v3.0** 发布（全文见 [LICENSE](LICENSE)），**整体开源**。

并入的实现来自**本版权人名下的两层来源**（自有 + 本方 fork）；它们各自还有**仍在活跃开发的第三方上游**。上游不由本版权人持有，本仓**不对其作任何许可主张**：

| 层 | 仓库 | 许可现状 | 并入方式 |
|---|---|---|---|
| 自有 | [hongguo-monorepo](https://github.com/dawei233/hongguo-monorepo) | `license=NONE`，版权人自有仓（**非 fork**） | `mp4_sanitize.py`、ffmpeg 解密链路**移植**为 Go |
| 本方 fork | [guoapp-android](https://github.com/dawei233/guoapp-android) | `license=NONE`，版权人自有仓（fork 自 [zhoufuweigg/guoapp](https://github.com/zhoufuweigg/guoapp)） | Go 内核（121 文件 / 24035 行 / 27 测试）**原样 vendor** |
| 本方 fork | [hongguo-drama](https://github.com/dawei233/hongguo-drama) | **GPL-3.0**（与本仓同许可，fork 自同为 GPL-3.0 的 [jackotom/hongguo-drama](https://github.com/jackotom/hongguo-drama)） | `xifan.js`/`hema.js`、弹幕签名与黄金向量**直接搬运** |
| 上游（**不并入**） | [zhoufuweigg/guoapp](https://github.com/zhoufuweigg/guoapp) | `license=NONE`，**第三方仓、仍在活跃开发**（2026-09-26）；本版权人无权对其授权 | 仅溯源与跟踪（§1.3） |
| 上游（**不并入**） | [jackotom/hongguo-drama](https://github.com/jackotom/hongguo-drama) | **GPL-3.0**，**第三方仓、仍在活跃开发**（2026-10-15） | 仅溯源与跟踪（§1.3） |

**授权的准确范围**：本版权人对自有仓 `hongguo-monorepo`、以及本方两个 fork 中**由本人新增/修改的部分**享有版权并授权按 GPL-3.0 并入本仓。`guoapp-android` 的 fork 基线来自**未声明许可证**的 `zhoufuweigg/guoapp`，该部分**仍是无许可的第三方代码**，本仓如实记录、不作虚假许可声明，并保留配合权利人处理的渠道。

**为什么全仓 GPL-3.0：** `hongguo-drama` 的发行物已内置 FFmpeg 9.0.2 + x264，构建为 GPLv3-or-later（含 x264 无法走 LGPL）—— **既有分发物本来就是 GPLv3 二进制**。源码侧统一为 GPL-3.0 不新增任何分发义务，反而消除了全部许可隔离工作。

**必须保留的署名（`NOTICE` 已记明）：**

1. **C 链路**：`dawei233/hongguo-drama`（本方 fork，已停更，`main` = `c884fab`）← [jackotom/hongguo-drama](https://github.com/jackotom/hongguo-drama)（**仍在活跃开发**，2026-10-15，`main` = `5a12c88`，GPL-3.0）← `zhenyong97/hongguo-downloader` ← `327044572/hongguo-downloader`（均 GPL-3.0）。依 GPLv3 第 5(a) 条声明本仓为修改版本。
2. **B 链路**：`dawei233/guoapp-android`（本方 fork，已停更，`android-only` = `ff3bad2`，含本方 57 个提交）← [zhoufuweigg/guoapp](https://github.com/zhoufuweigg/guoapp)（**仍在活跃开发**，2026-09-26，`main` = `0e60928`，**未声明许可证**）←「鱼佬」的短剧库 APP 源码（上游自述）。**如实记录：上游仍在开发且未声明许可证，不做虚假许可声明。**
3. **A 链路**：`dawei233/hongguo-monorepo`（版权人自有仓，**非 fork**，**已停止开发**，最后推送 2026-10-11）。
4. **上游跟踪义务**：两个上游是活跃项目，本方 fork 与其 vendored 快照会持续落后。每个 vendor 来源须在 `legacy/SOURCES.md` 记录**上游仓库 URL + 上游 commit SHA + 本方 fork 的 SHA**，并按 `docs/ARCHITECTURE.md` §1.3 的节奏定期评估上游提交、按需 cherry-pick（站源失效修复、播放修复优先）。
5. **算法出处**：签名与 Spade 取址算法改编自 `woshishiq1/drpys`（GPL-3.0），经 `N3urda/hongguoTV` 核对；西饭接口路径核对参考 `Hululu007/drpy-node`（GPL-3.0）。许可全文随仓分发于 `third_party/drpys/`。
6. **捆绑 ffmpeg/x264 的对应源码提供义务**：源码包随 Release 发布（FFmpeg + x264 完整源码、精确构建脚本、配置日志、二进制哈希），许可全文随产物分发。详见 [NOTICE](NOTICE) 第七节与 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) §3.4。

## 免责声明

本项目仅用于个人学习与技术研究。内容版权归红果/字节跳动及相关版权方所有，请勿用于商业用途。数据来自公开接口，请遵守目标站点的服务条款。
