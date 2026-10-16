import 'package:flutter/material.dart';

import '../layout.dart';
import '../tokens.dart';

/// 统一导航组件（`ANDROID-UI-REDESIGN.md` §5.1 第 1 条）。
///
/// **一份实现 + 变体参数**，取代现役两套：
/// - `AppBottomNavigation`（手机竖屏底栏，自绘 Row）
/// - Material `NavigationRail`（宽屏左轨）
///
/// ## 为什么要有它
///
/// 规范 §0 的诊断指出：上游把导航写成「底部 4 tab 平权」+「宽屏换成 64px
/// 图标 Rail」两套互不相干的实现，于是同一份 tab 语义要在两处各维护一遍
/// （图标、标签、选中态、角标）。§5.2 明确 `AppBottomNavigation` 要被
/// 「`HnNavigationRail` 的 `bottom` 变体取代（保留形态但不保留实现）」。
///
/// ## 变体与形态的对应（§2.1）
///
/// | 变体 | 宽 | 形态 |
/// |---|---|---|
/// | [HnRailVariant.wide] | 88 | 平板横屏 ★主力 |
/// | [HnRailVariant.narrow] | 72 | 手机横屏 |
/// | [HnRailVariant.bottom] | — | 手机竖屏（底部横排，高 68） |
///
/// 形态 → 变体的换算由 [forForm] 承担，调用方不需要自己判断宽度。

enum HnRailVariant {
  /// 平板横屏：88px 图标轨，标签 `tXs`，**不折叠**（面积够）。
  wide,

  /// 手机横屏：72px 图标轨，与 [wide] 同一套只缩宽。
  narrow,

  /// 手机竖屏：底部横排，每项等宽，高 68。
  bottom,
}

/// 导航项。用不可变值类型表达，避免调用方构造 Material 的
/// `NavigationDestination`/`NavigationRailDestination` 两种不同对象。
@immutable
class HnRailDestination {
  const HnRailDestination({
    required this.icon,
    required this.selectedIcon,
    required this.label,
  });

  final IconData icon;
  final IconData selectedIcon;
  final String label;
}

class HnNavigationRail extends StatelessWidget {
  const HnNavigationRail({
    super.key,
    required this.variant,
    required this.selectedIndex,
    required this.onSelected,
    required this.destinations,
    this.keyPrefix = 'hn-nav',
  });

  final HnRailVariant variant;
  final int selectedIndex;
  final ValueChanged<int> onSelected;
  final List<HnRailDestination> destinations;

  /// 测试用的键前缀（`$keyPrefix-$index`）。沿用现役 `bottom-nav-N` 的习惯，
  /// 便于测试从旧结构迁移。
  final String keyPrefix;

  /// 按 §2.1 的形态矩阵换算变体 —— **调用方不需要自己判宽度**。
  static HnRailVariant forForm(HnForm form) => switch (form) {
    HnForm.tabletLandscape => HnRailVariant.wide,
    HnForm.phoneLandscape => HnRailVariant.narrow,
    HnForm.phonePortrait => HnRailVariant.bottom,
  };

  @override
  Widget build(BuildContext context) => switch (variant) {
    HnRailVariant.bottom => _bottom(context),
    HnRailVariant.wide => _rail(context, HnBreak.railWide),
    HnRailVariant.narrow => _rail(context, HnBreak.railNarrow),
  };

  /// 左轨（平板横屏 88 / 手机横屏 72）：底 `sidebar` + 右侧描边，每项高 64。
  Widget _rail(BuildContext context, double width) {
    final tokens = Theme.of(context).extension<HongguoTokens>();
    final sidebar = tokens?.sidebar ?? Theme.of(context).colorScheme.surface;
    return Container(
      width: width,
      decoration: BoxDecoration(
        color: sidebar,
        border: Border(
          right: BorderSide(
            color: tokens?.sidebarBorder ?? Theme.of(context).colorScheme.outlineVariant,
          ),
        ),
      ),
      child: SafeArea(
        right: false,
        child: Column(
          children: [
            const SizedBox(height: HnSpacing.step3),
            for (final (index, destination) in destinations.indexed)
              _railItem(context, index, destination, width),
          ],
        ),
      ),
    );
  }

  Widget _railItem(
    BuildContext context,
    int index,
    HnRailDestination destination,
    double width,
  ) {
    final tokens = Theme.of(context).extension<HongguoTokens>();
    final selected = index == selectedIndex;
    final fg = selected
        ? (tokens?.sidebarOnPrimary ?? Colors.white)
        : (tokens?.sidebarFg ?? Theme.of(context).colorScheme.onSurfaceVariant);
    return Padding(
      padding: const EdgeInsets.symmetric(
        horizontal: HnSpacing.step2,
        vertical: HnSpacing.step1,
      ),
      child: Semantics(
        container: true,
        button: true,
        selected: selected,
        child: InkWell(
          key: ValueKey('$keyPrefix-$index'),
          // §4.1.1：选中底 `sidebar-primary`（primary-subtle）。
          borderRadius: BorderRadius.circular(HnSpacing.radiusLg),
          onTap: () => onSelected(index),
          child: Container(
            height: 64 - HnSpacing.step2,
            decoration: BoxDecoration(
              color: selected ? tokens?.sidebarPrimary : Colors.transparent,
              borderRadius: BorderRadius.circular(HnSpacing.radiusLg),
            ),
            child: Column(
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                Icon(selected ? destination.selectedIcon : destination.icon,
                    size: 24, color: fg),
                const SizedBox(height: HnSpacing.step1),
                // §4.1.1：标签 tXs(11)。
                Text(
                  destination.label,
                  style: TextStyle(
                    fontSize: HnTypo.tXs,
                    color: fg,
                    fontWeight: selected ? FontWeight.w600 : FontWeight.w500,
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  /// 底部横排（手机竖屏）：高 68，每项等宽，选中项顶部 3px 指示条（§5.1 第 9 条）。
  Widget _bottom(BuildContext context) {
    final theme = Theme.of(context);
    final colors = theme.colorScheme;
    final tokens = theme.extension<HongguoTokens>();
    return Material(
      color: theme.scaffoldBackgroundColor,
      child: SafeArea(
        top: false,
        child: DecoratedBox(
          decoration: BoxDecoration(
            border: Border(
              top: BorderSide(
                color: tokens?.borderSubtle ?? colors.outlineVariant,
              ),
            ),
          ),
          child: Row(
            children: [
              for (final (index, destination) in destinations.indexed)
                Expanded(
                  child: _bottomItem(context, index, destination),
                ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _bottomItem(
    BuildContext context,
    int index,
    HnRailDestination destination,
  ) {
    final tokens = Theme.of(context).extension<HongguoTokens>();
    final selected = index == selectedIndex;
    final primary = tokens?.primary ?? Theme.of(context).colorScheme.primary;
    final fg = selected
        ? primary
        : (tokens?.fgMuted ?? Theme.of(context).colorScheme.onSurfaceVariant);
    return Semantics(
      container: true,
      button: true,
      selected: selected,
      child: Tooltip(
        message: destination.label,
        excludeFromSemantics: true,
        child: InkWell(
          key: ValueKey('$keyPrefix-$index'),
          onTap: () => onSelected(index),
          child: ConstrainedBox(
            constraints: const BoxConstraints(minHeight: 68, minWidth: 48),
            child: Column(
              // 必须 min：`bottomNavigationBar` 给的是宽松高度约束，
              // 默认 MainAxisSize.max 会按可用高度铺满，把内容区挤下去
              // （实测表现：390 宽下 DramaCover 一个都渲染不出来）。
              mainAxisSize: MainAxisSize.min,
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                // §5.1 第 9 条：选中项顶部 18×3 指示条。
                Container(
                  width: 18,
                  height: 3,
                  decoration: BoxDecoration(
                    color: selected ? primary : Colors.transparent,
                    borderRadius: BorderRadius.circular(1.5),
                  ),
                ),
                const SizedBox(height: HnSpacing.step1),
                Icon(selected ? destination.selectedIcon : destination.icon,
                    size: 24, color: fg),
                Text(
                  destination.label,
                  style: TextStyle(
                    fontSize: HnTypo.tXs,
                    color: fg,
                    fontWeight: selected ? FontWeight.w600 : FontWeight.w500,
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
