# B7 前置清单：TV 相关代码与用例的「删除 vs 改写」分类

> **状态**：待上级逐条确认。**尚未执行任何删除**。
> **依据**：`docs/ANDROID-UI-REDESIGN.md` §5.2（要下线的组件）、§6（B7 批次）、§6.2 防回归三原则。
> **用户范围**：TV / 10-foot UI **不在范围**（"TV 无所谓，主要给平板/手机/PC 用；平板上面只横屏用"）。
> **上级判据**（本轮确认）：**这条用例保护的是"遥控输入"还是"宽屏布局"** ——
> 前者删，后者改写为触摸 + 横屏。

---

## 一、待删代码

| # | 对象 | 规模 | 判据 | 备注 |
|---|---|---|---|---|
| 1 | `lib/television_controls.dart`（整个文件） | 697 行 | 纯遥控实现，且是 §2.2 点名的"第二套控制层" | 删后 `PlayerControls` 成为唯一控制层 |
| 2 | `lib/remote_widgets.dart` 的**遥控焦点部分** | ≈650 行（文件 789 行） | `RemoteTarget` / `RemoteButton` / `RemoteEpisodeButton` / `TelevisionSearchDialog` 等纯遥控件 | **§5.2 明确要求拆出保留 `SliverGrid` 与懒加载重试逻辑**（见下"必须保留"） |
| 3 | `AppBottomNavigation`（整个文件） | 41 行 | §5.2：已被 `HnNavigationRail` 的 bottom 变体取代 | **B6 已完成取代**，该类现已无引用（仅定义处残留），可直接删 |
| 4 | `lib/home_screen.dart` 的 TV 侧边栏（164px）与 `_televisionGrid` | ≈120 行 | TV 形态专用渲染 | 删后 `_tab` 可一并退役（见"连带"） |
| 5 | `lib/app_layout.dart` 的 `AppLayout.television` / `AppDevice.television` / `televisionTheme()` | ≈60 行 | TV 形态开关 | ⚠ 与 #7 联动，见"连带" |
| 6 | `lib/app_orientation.dart` 的 `television` 分支 | ≈15 行 | `orientations(television:...)` 仅在 TV 下锁横屏 | 删后签名可简化 |
| 7 | `lib/episode_browser.dart` / `lib/saved_library.dart` 的 `isTelevision` 分支 | ≈80 行 | 两处 `RemoteGrid` 用法都在 TV 分支内 | **compact 分支（播放器侧栏）必须保留** —— 那是 B5 的成果 |

### 连带影响（需一并决定）

- **`_tab` 退役**：上一轮我刻意保留 `_tab` 让 TV 侧边栏继续工作（§"两套并存"）。删掉 TV 后，非 TV 的导航已由 `_mySection` + 目的地映射承担，`_tab` 可替换为更直接的表达。**这是纯内部重构，但会牵动一批测试断言**（凡 `find.text('追剧')` 之类的路径可能变）。
- **`_television` 状态字段**（`player_screen.dart`）与 `hideOverlayForPictureInPicture` 相关分支：删除 `TelevisionControls` 后，`_videoPane` 里的 `_television ? TelevisionControls : PlayerControls` 二选一变为单分支。
- **`AppLayout.isTelevision`** 是全应用范围的开关（不止播放器），删它需要确认**没有其它依赖**（我尚未全量排查）。

---

## 二、必须保留（§5.2 明确要求）

| 对象 | 原因 |
|---|---|
| `remote_widgets.dart` 的 `RemoteGrid` + 懒加载重试逻辑（约 140 行） | §5.2 原文：「`remote_widgets.dart` 里的 `SliverGrid` 与懒加载重试逻辑被其它页面复用，**拆出保留，只删遥控相关部分**」。实测：`episode_browser`（compact 侧栏）与 `saved_library` 的非 TV 分支都在用 `RemoteGrid`。 |
| `lib/ui/components/` 下 5 个新组件 | B1–B6 成果（`HnNavigationRail` / `HnSegmentedTabs` / `HnSectionHeader` / `HnHeroRail` / `HnFeaturedCard` / `HnSettingsGroup`） |
| `test/remote_test_helpers.dart` 的 `televisionHost`（若还有非 TV 用例需要它） | 需逐条确认；`focusRemote` 是 TV 专用，应删 |

---

## 三、测试用例逐条分类（**10 例**，已实跑核实）

> **勘误**：我在此前几轮的口头汇报里说「16 个 TV 用例」，**那个数字是错的**。
> 实际用 `flutter test` 逐文件跑出来是 **10 例**：
> `television_test.dart` 6 例（5 个 `testWidgets`，其中 1 个按 2 个尺寸展开）、
> `television_player_test.dart` 3 例、`downloads_test.dart` 1 例。
> 另有 `_debug_switcher_test.dart` 用 `television: true` 挂载，但它测的是
> **站源密码门控**（`debug switcher behavior with gate locked vs off`），
> 与 TV 无关 —— 应**改写为普通形态**而非删除（见 #10）。
> 我用"数声明"而非"数引用"高估了数量，这次按实跑输出为准。

### A. 建议**删除**（保护"遥控输入"）

| # | 文件 | 用例 | 判据 |
|---|---|---|---|
| 1 | `television_test.dart` | manual interface preference overrides device detection and survives restart | 断言 `tv-nav-N` 出现/消失 + `displayMode` 持久化。**"界面模式偏好"整个概念随 TV 下线**（用户已明确不做 TV）。若你认为"手动强制某形态"仍有价值，可改写为"强制平板横屏布局"，但那与 `HnBreak` 自动判形态冲突 —— 我建议删。 |
| 2 | `television_test.dart` | TV sources, details, VIP confirmation and back retain remote focus at $dimensions（2 个尺寸参数 = 2 例） | 全程 `sendKeyEvent` + `focusRemote`，保护的是**遥控可达性**。 |
| 3 | `television_test.dart` | episode grid initially focuses an offscreen saved episode | 断言 `primaryFocus.debugLabel == 'remote-131'`，纯焦点行为。**注意**：其中"初始定位到已看集"这个**业务行为**是有价值的（B5 也做了"当前集高亮"），但它在非 TV 下由 `_continuousGrid` 的 `jumpTo` 承担，已有独立覆盖。 |
| 4 | `television_player_test.dart` | TV remote pauses, seeks when hidden, seeks on the timeline and returns from fullscreen | 纯遥控键位 + 焦点。 |
| 5 | `television_player_test.dart` | TV selection restores the current episode and panels keep focus during playback | 同上；"恢复当前集"的业务行为由 B5 的侧栏 `/` 非 TV 播放器路径覆盖。 |
| 6 | `television_player_test.dart` | TV playback error puts remote focus on retry and restores controls after retry | 同上；**"播放错误可重试"的业务行为需要保留**，但那由非 TV 用例覆盖（`player_screen_test.dart` 有错误路径用例）。 |
| 7 | `downloads_test.dart` | TV download navigation and long queue remain usable with only the remote | 纯遥控（长队列表格 + 方向键）。**这条是上一轮我改成"真产品 bug 修复"的那条** —— 修的是"下载页没有遥控焦点"。TV 下线后该修复的**对象**消失，用例随之失效。 |

**小计**：删除 **8 例**（#2 是 2 例）。

### A+. 建议**改写为普通形态**（与 TV 无关但被 TV 宿主牵连）

| # | 文件 | 用例 | 改写方向 |
|---|---|---|---|
| 10 | `_debug_switcher_test.dart` | debug switcher behavior with gate locked vs off | 只把 `television: true` 换成普通形态即可。它测的是**站源密码门控**（`sourceGateOff` 开/关时站源列表差异），与 TV 毫无关系 —— 只是当初借 TV 宿主来跑。**不能删**：那是密码门锁的核心行为。 |

### B. 建议**改写为触摸 + 横屏**（保护"宽屏布局"）

| # | 文件 | 用例 | 改写方向 |
|---|---|---|---|
| 8 | `television_test.dart` | Android TV detection falls back safely when the platform channel is unavailable | **其实不必删也不必改写**：它测的是 `AppDevice.detect()` 对平台通道缺失的**容错**，与 TV UI 无关 —— 只要 `AppDevice` 还在（它同时提供 `version`），这条就该保留。**若删掉 `television` 字段，则本条需改写为"设备信息通道缺失时 `version` 回退"**。 |
| 9 | `television_test.dart` | remote grid reaches unbuilt rows, partial last row and pagination without touch | **改写为触摸**：它直接构造 `RemoteGrid` 测**懒加载 + 分页 + 末行不满**，那是 `RemoteGrid` 本身的行为（§5.2 要求保留该组件）。改为直接测 `RemoteGrid`（不经 TV 壳），或改测使用它的非 TV 路径（`episode_browser` 的 compact 侧栏 / `saved_library`）。**这条的价值与 TV 无关，是网格组件的回归测试。** |

**小计**：改写 **2 例**（#8 若选择保留则实为 1 例）。

---

## 四、需要你裁决的三点

1. **`displayMode`（界面模式偏好）是否保留？** 它是"自动/电视/标准"三态。TV 下线后，"电视"态无意义。建议**降为两态（自动/标准）或整个删除**（由 `HnBreak` 自动判形态）。这影响用例 #1 的处置。

2. **`_tab` 是否随 B7 一并退役？** 我倾向**是**（删掉 TV 后它就是冗余的表达层，且 §3.2 的新 IA 已由目的地 + 分段承担）。但这会牵动一批断言，属"内部重构"而非"用户可见变化"，风险中等。若你希望 B7 纯粹做"删除"、不动内部结构，我可把 `_tab` 退役拆成 B8。

3. **`AppLayout.isTelevision` / `AppDevice.television` 是否全删？** 这是全应用范围的开关。我需要先全量排查其它依赖（本轮未做），排查后再给结论。若你希望先删局部（`TelevisionControls` + TV 侧边栏 + 遥控件），把 `isTelevision` 留到最后一批，也可以 —— 那样风险更可控。

---

## 五、我的建议顺序（若你同意）

1. **B7a（低风险）**：删 `television_controls.dart` + `AppBottomNavigation`，播放器二选一改单分支。测试：删 3 例播放器遥控用例。
2. **B7b（中风险）**：删 TV 侧边栏 + 遥控件（保留 `RemoteGrid`），改写 #9 为触摸/组件级。测试：删 `television_test.dart` 剩余遥控用例，保留/改写 #8 #9。
3. **B7c（中风险）**：`displayMode` 与 `_tab` 收敛、`isTelevision` 全删（**先做全量依赖排查**）。

## 六、账目平衡（10 例的去向）

| 处置 | 例数 | 编号 |
|---|---|---|
| 删除 | 8 | #1 #2(×2) #3 #4 #5 #6 #7 |
| 改写为触摸/组件级 | 1 | #9 |
| 改写为普通形态 | 1 | #10 |
| 保留（仅调整字段） | 0–1 | #8（取决于 `AppDevice.television` 是否删） |

**合计 10 例**，无遗漏。执行时逐条核对，删完必须能对上这张表 ——
B7 是"删"，删错了不会红、只会静默失去能力（"只删不搬"的教训同源）。

---

分三批的理由与前面一致：**便于失败归因**。且 B7 是"删"，与前面"增/改"的风险性质不同 —— 删错了不会红，只会静默失去能力（如"只删不搬"那次教训），所以每批都要逐项核对"删除对象是否还有别的使用者"。
