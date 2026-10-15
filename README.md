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
- **B 链路（`guoapp`）的许可定性已决，不是待办**：其上游（`zhoufuweigg/guoapp` 与「鱼佬」一链）**未声明许可**，版权人已明确决定**直接采用**，代价是以**完整、如实的署名声明 + 合规响应通道**承担合规责任 —— 本仓**不存在"等待上游授权 / 需取得许可才能用"这类前置条件**。声明全文见 [NOTICE](NOTICE) 第三节。

## 特性（目标）

- **11 个站源**统一在一个 `Source` 接口下：红果、韩小圈、鬼片、青空（默认可见），黄豆、剧果、野果、帝果、黄果（黄果含 AI / 视频 / 旧版三个 facet），加两个从本方 fork [dawei233/hongguo-drama](https://github.com/dawei233/hongguo-drama) 移植的西饭、河马。`cloudfront` 是黄果的旧版 facet，`legacy`（旧接口自动发现）只是回退机制，两者都不是独立站源。
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

并入的实现来自**本版权人名下的两层来源**（自有 + 本方 fork）；它们各自还有**仍在活跃开发的第三方上游**。上游不由本版权人持有，本仓**不对其作任何许可主张**。

> **B 链路（`guoapp`）的采用方式与授权范围是已决事实，不是待办**：版权人已决定**直接采用**上游未声明许可的该来源，并以**完整署名声明 + 合规响应通道**承担合规责任 —— **不存在"等待上游授权 / 需取得许可才能用"这类前置**。对外可发布的完整声明见 [NOTICE](NOTICE) **第三节**。下表为速览：

| 层 | 仓库 | 许可现状 | 并入方式 |
|---|---|---|---|
| 自有 | [hongguo-monorepo](https://github.com/dawei233/hongguo-monorepo) | `license=NONE`，版权人自有仓（**非 fork**） | `mp4_sanitize.py`、ffmpeg 解密链路**移植**为 Go |
| 本方 fork | [guoapp-android](https://github.com/dawei233/guoapp-android) | `license=NONE`，版权人自有仓（fork 自 [zhoufuweigg/guoapp](https://github.com/zhoufuweigg/guoapp)，**上游未声明许可**） | Go 内核（121 文件 / 24035 行 / 27 测试）**原样 vendor** |
| 本方 fork | [hongguo-drama](https://github.com/dawei233/hongguo-drama) | **GPL-3.0**（与本仓同许可，fork 自同为 GPL-3.0 的 [jackotom/hongguo-drama](https://github.com/jackotom/hongguo-drama)） | `xifan.js`/`hema.js`、弹幕签名与黄金向量**直接搬运** |
| 上游（**不并入**） | [zhoufuweigg/guoapp](https://github.com/zhoufuweigg/guoapp) | `license=NONE`，**第三方仓、仍在活跃开发**（2026-09-26）；本版权人无权对其授权，**也不去索取** | 仅溯源与跟踪（§1.3）；**已决：直接采用**经本方 fork 承接的该来源，以署名 + 响应通道承担合规责任 |
| 上游（**不并入**） | [jackotom/hongguo-drama](https://github.com/jackotom/hongguo-drama) | **GPL-3.0**，**第三方仓、仍在活跃开发**（2026-10-15） | 仅溯源与跟踪（§1.3） |

**授权的准确范围（如实收窄，不越界）**：本版权人对自有仓 `hongguo-monorepo`、以及本方两个 fork 中**由本人新增/修改的部分**享有版权并授权按 GPL-3.0 并入本仓。`guoapp-android` 的 fork 基线来自**未声明许可证**的 `zhoufuweigg/guoapp`（再往上还有**无法核实的**上游②「鱼佬」的短剧库 APP），**该部分仍是无许可的第三方代码**：

- 本仓对该部分**仅作署名与来源记录 —— 既不主张授权，也不声称已获授权**；本仓不是它的权利人，**无权代表它授权**，本仓的 GPL-3.0 声明也不改变它的法律状态。
- 本版权人**已明确决定直接采用**该来源，承担方式不是"取得授权"（**不存在这类待办或前置**），而是**完整如实的署名声明 + 合规响应通道**：权利人可以经本仓 Issue 联系，本仓配合处理（**含移除相关内容**）。全文见 [NOTICE](NOTICE) 第三节。
- **使用者提示**：该部分上游未声明许可，**使用、修改、再分发的风险需由使用者自行评估**。

**B 自身写的是"未获上游授权前请勿商用或对外分发"，本仓明确不一致。** `guoapp-android` 的 README 该句是其作为 fork 维护者的谨慎表述；本仓**不沿用**这一前置条件 —— 版权人已知情并决定**直接采用、公开分发**（本仓即公开发布的 GPL-3.0 仓库），承担方式改为署名 + 响应通道。本仓**不主张**自己更谨慎，也**不主张**该表述已被上游授权解除；二者是不同主体在知情下的不同取舍（逐字对照见 NOTICE 第三节 3.6）。

**为什么全仓 GPL-3.0：** `hongguo-drama` 的发行物已内置 FFmpeg 9.0.2 + x264，构建为 GPLv3-or-later（含 x264 无法走 LGPL）—— **既有分发物本来就是 GPLv3 二进制**。源码侧统一为 GPL-3.0 不新增任何分发义务，反而消除了全部许可隔离工作。

**必须保留的署名（`NOTICE` 已记明）：**

1. **C 链路**：`dawei233/hongguo-drama`（本方 fork，已停更，`main` = `c884fab`）← [jackotom/hongguo-drama](https://github.com/jackotom/hongguo-drama)（**仍在活跃开发**，2026-10-15，`main` = `5a12c88`，GPL-3.0）← `zhenyong97/hongguo-downloader` ← `327044572/hongguo-downloader`（均 GPL-3.0）。依 GPLv3 第 5(a) 条声明本仓为修改版本。
2. **B 链路**：`dawei233/guoapp-android`（本方 fork，已停更，`android-only` = `ff3bad2`，含本方 57 个提交）← [zhoufuweigg/guoapp](https://github.com/zhoufuweigg/guoapp)（**仍在活跃开发**，2026-09-26，`main` = `0e60928`，**未声明许可证**）←「鱼佬」的短剧库 APP 源码（**来自互联网，原始仓库与许可信息均未提供，本仓无法进一步核实**，链路到此为止）。**如实记录：全链均未声明许可证，不做虚假许可声明；本仓仅对版权人本人新增/修改的部分主张授权，上游派生部分只作署名与来源记录 —— 既不主张、也不声称已获授权**（NOTICE 第三节）。
3. **A 链路**：`dawei233/hongguo-monorepo`（版权人自有仓，**非 fork**，**已停止开发**，最后推送 2026-10-11）。
4. **上游跟踪义务**：两个上游是活跃项目，本方 fork 与其 vendored 快照会持续落后。每个 vendor 来源须在 `legacy/SOURCES.md` 记录**上游仓库 URL + 上游 commit SHA + 本方 fork 的 SHA**，并按 `docs/ARCHITECTURE.md` §1.3 的节奏定期评估上游提交、按需 cherry-pick（站源失效修复、播放修复优先）。
5. **算法出处**：签名与 Spade 取址算法改编自 `woshishiq1/drpys`（GPL-3.0），经 `N3urda/hongguoTV` 核对；西饭接口路径核对参考 `Hululu007/drpy-node`（GPL-3.0）。许可全文随仓分发于 `third_party/drpys/`。
6. **捆绑 ffmpeg/x264 的对应源码提供义务**：源码包随 Release 发布（FFmpeg + x264 完整源码、精确构建脚本、配置日志、二进制哈希），许可全文随产物分发。详见 [NOTICE](NOTICE) 第八节与 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) §3.4。
7. **合规响应通道**：来源链路中**任一环的权利人**（含 `zhoufuweigg/guoapp` 作者、「鱼佬」本人或其继承人，以及 A/C 链路、算法出处与第三方组件的权利人）若认为本仓不妥，可经本仓 Issue 联系，本仓配合处理（**含移除相关内容**、补充署名、下架）。这是"直接采用 B 链路"这一决定的**对应义务**，见 [NOTICE](NOTICE) 第三节 3.3。

## 免责声明

**先把"代码"与"内容"的许可区别说清楚，避免自相矛盾：**

- **代码**：本仓源码以 **GPL-3.0** 发布（全文见 [LICENSE](LICENSE)）。该许可**允许**商业使用与再分发 —— 本仓**不**对代码附加"禁止商用"的限制。
- **内容**：本软件**不含**任何剧集媒体内容；剧集、封面、元数据及相关商标的版权归红果 / 字节跳动及相关版权方所有，**不在本仓的授权范围内**，本仓无权亦不对其作任何授权。

因此：

- **请勿**将本软件用于转售、付费分发，或以"官方""授权"名义收费；请勿冒充官方。
- 本软件**不解除、不绕过**任何平台的 VIP、付费或授权限制；数据来自公开接口，请遵守目标站点的服务条款。
- 使用本软件所产生的一切后果由使用者自行承担。
- **B 链路派生部分的上游未声明许可**，使用、修改、再分发的风险需**自行评估**（见上「许可与合规」与 [NOTICE](NOTICE) 第三节 3.4）。
