import 'package:flutter/widgets.dart';

/// `docs/ANDROID-UI-REDESIGN.md` §2.1 的形态判定与内容密度**唯一实现**。
///
/// ## 为什么要有这个文件
///
/// 重设计规范 §0 的结论是：M4b 只做了"换皮"（令牌镜像 + 包名 + 图标），
/// 没有动信息架构与布局。而布局层最先暴露的问题就是**同一个宽度下密度不一致**：
/// 改造前实测有 **四处各不相同的列数算法**：
///
/// | 位置 | 算法 | 1200px 下结果 |
/// |---|---|---|
/// | `home_screen.dart` 的 `_televisionGrid` | `((width-36)/150).floor().clamp(1,8)` | 7 列 |
/// | `widgets.dart` 的 `dramaGridDelegate` | `width<600 ? 3 : (width/180).floor().clamp(4,9)` | 6 列 |
/// | `detail_screen.dart` 的 `_episodeColumns` | 按集号位数 + 文字缩放求最小 tile 宽 | 动态 |
/// | `remote_widgets.dart` 的 `RemoteGrid` | 由调用方传 `columns` | 各处不同 |
///
/// 后果：同一个 1200px 屏上，发现页 7 列、推荐 6 列、选集列数随集号漂移 ——
/// 用户看到的是"每页密度都不一样"。本文件把这四处收敛为**两个函数**（§6.2 原则 2：
/// 列数算法单点）。
///
/// ## 形态判定只认「宽 + 朝向」
///
/// §2 明确要求：**不认设备类型** —— 同一台平板横竖旋转后是两个不同形态。
/// 所以这里没有 `isTablet` 这类判断设备身份的 API，只有 `formOf(Size)`。
///
/// 形态范围（用户明确，见 §2 表头）：
/// - **平板横屏**是主力形态（宽 ≥ 900 且横屏）
/// - 手机竖屏 / 手机横屏为适配形态
/// - TV / 10-foot 与平板竖屏**不在设计范围**（用户明确：平板只横屏用）

/// 三档形态（§2 的形态矩阵）。
enum HnForm {
  /// 手机竖屏：单列栈 + 底部导航。
  phonePortrait,

  /// 手机横屏：轻量主从（72px 图标轨），发现页仍保持窄流。
  phoneLandscape,

  /// 平板横屏 ★主力：主从结构（88px 左轨 + 内容区）。
  tabletLandscape,
}

/// 断点常量与内容密度算法（§2.1）。
///
/// 断点只此一处；任何页面都不得再写 `900` 这类字面量。
abstract final class HnBreak {
  /// 横屏主从结构的最小宽度（§2.1）。
  static const double tablet = 900;

  /// 平板左导航轨宽（§2.1）。
  static const double railWide = 88;

  /// 手机横屏左导航轨宽（§2.1）。
  static const double railNarrow = 72;

  /// 平板横屏还要求最小高度 —— 极矮的横屏窗口放不下主从结构（§2.1）。
  static const double railTabletMinHeight = 600;

  /// 海报卡的目标**最小**宽度（§2.1 的 `minmax(150px,1fr)` 下界）。
  static const double posterMinWidth = 150;

  /// 海报网格的列间隙（与 `dramaGridDelegate` 横屏档的 18 一致）。
  static const double posterGap = 18;

  /// 固定网格的列数（**不随宽度变化**，故不属于 [posterColumns] / [episodeColumns]
  /// 的响应式算法家族，但仍在此集中定义，避免字面量散落）。
  ///
  /// - [keyboardColumns]：TV 虚拟键盘的字母键位（§5.2 的 TV 相关部分虽在后续批次
  ///   下线，但键盘属独立小部件，其 6 列是**键位规格**而非内容密度）。
  /// - [episodeSidebarColumns]：平板横屏播放器的选集侧栏内固定 4 列
  ///   （§4.2.2：侧栏宽 300，不随整屏变化；整屏档位由 [episodeColumns] 给）。
  static const int keyboardColumns = 6;
  static const int episodeSidebarColumns = 4;

  /// 播放器选集侧栏宽度（§4.2.2）。
  static const double episodeSidebarWidth = 300;

  /// 选集侧栏内单格高度（§4.2.2「4 列 × 44px」）。
  static const double episodeSidebarExtent = 44;

  /// 形态判定：**只认宽 + 朝向**，不认设备类型（§2）。
  ///
  /// 注意判定顺序：先看是否达到平板横屏的「宽 ≥ 900 **且** 高 ≥ 600」门槛，
  /// 否则按朝向归入手机两类。这样一台 900×500 的横屏窗口不会拿到平板布局
  /// ——它的高度放不下主从结构。
  static HnForm formOf(Size size) {
    final landscape = size.width > size.height;
    if (landscape && size.width >= tablet && size.height >= railTabletMinHeight) {
      return HnForm.tabletLandscape;
    }
    return landscape ? HnForm.phoneLandscape : HnForm.phonePortrait;
  }

  /// 该形态是否为「横屏」（决定导航用左轨还是底栏）。
  static bool isLandscape(HnForm form) => form != HnForm.phonePortrait;

  /// 左导航轨宽度；竖屏无轨返回 0（§2.1）。
  static double railWidth(HnForm form) => switch (form) {
    HnForm.tabletLandscape => railWide,
    HnForm.phoneLandscape => railNarrow,
    HnForm.phonePortrait => 0,
  };

  /// 海报墙列数（§2.1）——**取代** `home_screen.dart` 与 `widgets.dart` 的两套。
  ///
  /// [width] 是**内容区可用宽**（即已经扣掉左导航轨与页边距的那个宽度，
  /// 也就是 `SliverGrid` 直接拿到的 `constraints.maxWidth`）。
  /// [screenWidth] 是整屏物理宽，**仅用于形态判定**（是否达到平板门槛）。
  ///
  /// ⚠ 为什么要把这两个宽分开：现役调用点（`home_screen.dart` 的
  /// `_catalog`、`saved_library.dart`、`recommendations_screen.dart`）都在
  /// `Row` 的 `Expanded` 里，`constraints.maxWidth` **已经是内容宽**、不含左轨。
  /// 若本函数再减一次 `railWide` 就会重复扣减（把 88px 扣两遍），
  /// 在 1200px 屏上会少一列。所以 API 显式区分，避免调用方猜。
  ///
  /// 算法用 CSS `auto-fill` 同款公式：列数由「可用宽 + 一个间隙」除以
  /// 「最小列宽 + 一个间隙」求得 —— 这样每列**都真正** ≥ [posterMinWidth]。
  ///
  /// ⚠ **与规范 §2 那句「1200px 宽约 7 列」不一致，此处按物理正确性实现。**
  /// 规范用 `(1200-88-48)/150 = 7.09 → 7`，但该算式**忽略列间隙**：
  /// 7 张 ≥150px 卡 + 6 个 18px 间隙需 `7*150+6*18 = 1158px`，而 1200px 屏
  /// 扣掉 88px 左轨与 48px 页边距只剩 **1064px** —— 放不下；强取 7 列会让
  /// 每张卡缩到 136px，反而**违反**它自己引用的 `minmax(150px,1fr)` 下界。
  ///
  /// 本实现取值：900px→4 列、1200px→**6 列**、1440px→7 列、1600px→8 列。
  /// 验收以 §7 第 7 条为准（「平板横屏下发现页 **≥6 列**」）——1200px 得 6 列，
  /// **通过**。§2 散文里那句「7 列」属规范笔误，已记入实施报告。
  ///
  /// - 平板横屏：按上述公式求解，夹在 4–9
  /// - 手机横屏：固定 3 列（宽度够但高度矮，避免一屏塞太多）
  /// - 手机竖屏：固定 2 列
  static int posterColumns(
    double width, {
    required bool landscape,
    double? screenWidth,
  }) {
    final bool tabletLandscape =
        landscape && (screenWidth ?? width) >= tablet;
    if (tabletLandscape) {
      final columns =
          ((width + posterGap) / (posterMinWidth + posterGap)).floor();
      return columns.clamp(4, 9);
    }
    return landscape ? 3 : 2;
  }

  /// 选集网格列数（§2.1）——**取代** `detail_screen.dart` 的 `_episodeColumns`。
  ///
  /// [minTileWidth] 保留旧实现「按集号位数与文字缩放求最小 tile 宽」的能力：
  /// 集号到三位数、或用户放大了系统字号时，固定列数会把 tile 挤到重叠，
  /// 此时由调用方传入所需最小宽，本函数按可用宽重新求解。不传（或传 0）
  /// 则用 §2.1 的固定档位。
  ///
  /// 下限保持 3：即便文字放到最大，也不至于退化成单列（单列会让 100 集的
  /// 选集网格变成 100 行滚动）。
  static int episodeColumns(
    double width, {
    required bool landscape,
    double minTileWidth = 0,
  }) {
    final bool wide = landscape && width >= tablet;
    final int base = wide ? 8 : (landscape ? 6 : 4);
    if (minTileWidth <= 0) return base;
    final int fitted = (width / minTileWidth).floor();
    return fitted < base ? fitted.clamp(3, base) : base;
  }
}
