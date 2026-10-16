import 'package:flutter/material.dart';

import '../tokens.dart';

/// 分段标签（`ANDROID-UI-REDESIGN.md` §5.1 第 2 条）。
///
/// 用于「我的」页的三分段（追剧 / 最近 / 下载）—— 规范 §3.2 把上游那三个
/// 平级 tab 合并进一个目的地后，三者改为**同级分段**，需要这个组件承载。
///
/// | 规范项 | 值 |
/// |---|---|
/// | 变体 | `filled` / `outline` |
/// | 高 | 36 |
/// | 圆角 | `HnSpacing.radiusSm (7)` |
/// | 状态 | default / selected / disabled |
///
/// ## 为什么不用 M3 的 `SegmentedButton`
///
/// Material 3 的 `SegmentedButton` 高度、内间距与圆角由 `SegmentedButtonThemeData`
/// 决定，默认高 40、圆角随 `shape`，要贴合 §5.1 的「高 36 / radius-sm」得写一层
/// 主题覆写；而它同时带 checkmark 图标、以及 M3 的 selected 色（`secondaryContainer`），
/// 与设计系统的 `primarySubtle` + `primary` 口径不一致。这里按规范直绘，
/// 尺寸与颜色都取自令牌，避免"看起来差不多但每处差 2px"的漂移。
class HnSegmentedTabs extends StatelessWidget {
  const HnSegmentedTabs({
    super.key,
    required this.labels,
    required this.selectedIndex,
    required this.onSelected,
    this.variant = HnSegmentedVariant.filled,
    this.enabled = true,
    this.keyPrefix = 'hn-seg',
  });

  final List<String> labels;
  final int selectedIndex;
  final ValueChanged<int> onSelected;
  final HnSegmentedVariant variant;

  /// 整体禁用（§5.1 的 disabled 态）。
  final bool enabled;

  /// 测试用的键前缀（`$keyPrefix-$index`）。
  final String keyPrefix;

  /// 规范 §5.1 第 2 条：高 36。
  static const double height = 36;

  @override
  Widget build(BuildContext context) {
    final tokens = Theme.of(context).extension<HongguoTokens>();
    final primary = tokens?.primary ?? Theme.of(context).colorScheme.primary;
    final onPrimary = tokens?.onPrimary ?? Theme.of(context).colorScheme.onPrimary;
    final subtle =
        tokens?.primarySubtle ?? Theme.of(context).colorScheme.primaryContainer;
    final onSubtle = tokens?.onPrimarySubtle ??
        Theme.of(context).colorScheme.onPrimaryContainer;
    final border = tokens?.border ?? Theme.of(context).colorScheme.outlineVariant;
    final muted = tokens?.fgMuted ?? Theme.of(context).colorScheme.onSurfaceVariant;

    return SizedBox(
      height: height,
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          for (final (index, label) in labels.indexed)
            _segment(
              context: context,
              index: index,
              label: label,
              selected: index == selectedIndex,
              primary: primary,
              onPrimary: onPrimary,
              subtle: subtle,
              onSubtle: onSubtle,
              border: border,
              muted: muted,
            ),
        ],
      ),
    );
  }

  Widget _segment({
    required BuildContext context,
    required int index,
    required String label,
    required bool selected,
    required Color primary,
    required Color onPrimary,
    required Color subtle,
    required Color onSubtle,
    required Color border,
    required Color muted,
  }) {
    final Color background;
    final Color foreground;
    final BoxBorder? stroke;
    switch (variant) {
      case HnSegmentedVariant.filled:
        background = selected ? primary : Colors.transparent;
        foreground = selected ? onPrimary : muted;
        stroke = selected
            ? null
            : Border.all(color: border.withValues(alpha: .6));
      case HnSegmentedVariant.outline:
        background = selected ? subtle : Colors.transparent;
        foreground = selected ? onSubtle : muted;
        stroke = Border.all(color: selected ? primary : border);
    }
    final first = index == 0;
    final last = index == labels.length - 1;
    return Semantics(
      button: true,
      selected: selected,
      enabled: enabled,
      child: InkWell(
        key: ValueKey('$keyPrefix-$index'),
        onTap: enabled ? () => onSelected(index) : null,
        child: Container(
          // §5.1 第 2 条：高 36 / 圆角 radius-sm。首尾各留外圆角，
          // 中间段直角 —— 分段控件连成一条（避免每段各圆一次像三个按钮）。
          constraints: const BoxConstraints(minWidth: 72),
          decoration: BoxDecoration(
            color: background,
            border: stroke,
            borderRadius: BorderRadius.horizontal(
              left: Radius.circular(first ? HnSpacing.radiusSm : 0),
              right: Radius.circular(last ? HnSpacing.radiusSm : 0),
            ),
          ),
          alignment: Alignment.center,
          padding: const EdgeInsets.symmetric(horizontal: HnSpacing.step4),
          child: Text(
            label,
            style: TextStyle(
              fontSize: HnTypo.tBase,
              color: enabled ? foreground : muted.withValues(alpha: .5),
              fontWeight: selected ? FontWeight.w600 : FontWeight.w500,
            ),
          ),
        ),
      ),
    );
  }
}

enum HnSegmentedVariant {
  /// 选中段实心 `primary`。
  filled,

  /// 选中段 `primarySubtle` 底 + `primary` 描边。
  outline,
}
