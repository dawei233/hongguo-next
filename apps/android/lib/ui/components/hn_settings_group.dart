import 'package:flutter/material.dart';

import '../tokens.dart';

/// 设置分组卡（`ANDROID-UI-REDESIGN.md` §4.3.3 与 §5.1 第 9 条）。
///
/// 上游设置页是 **9 条平权 `ListTile`** —— 用户必须逐个读才知道自己在哪
/// （§0 诊断：「9 条平权列表让用户逐个读才知道自己在哪」）。本组件承载
/// 「场景分组」：一个分组标题 + 一张卡内的若干行。
///
/// | 规范项 | 值 |
/// |---|---|
/// | 变体 | `card`（手机竖屏）/ `plain`（横屏两栏的右侧内容） |
/// | 分组标题 | `tSm (12) muted`，上下间距 `step2` |
/// | 行高 | 56（横屏 48） |
/// | 卡 | `radius-lg`，`card` 底 |
///
/// **行内不画分隔线**：靠间距区分（§4.3.3 原文「无分隔线（靠间距区分）」）——
/// 上游每行都有 `Divider`，视觉噪音大。
class HnSettingsGroup extends StatelessWidget {
  const HnSettingsGroup({
    super.key,
    required this.title,
    required this.children,
    this.variant = HnSettingsVariant.card,
    this.trailing,
  });

  /// 分组标题（如「通用」「数据与同步」）。
  final String title;
  final List<Widget> children;
  final HnSettingsVariant variant;

  /// 标题右侧的动作（可选）。
  final Widget? trailing;

  /// §5.1 第 9 条：行高 56；横屏 48。
  static const double rowHeight = 56;
  static const double rowHeightWide = 48;

  @override
  Widget build(BuildContext context) {
    final tokens = Theme.of(context).extension<HongguoTokens>();
    final muted = tokens?.fgMuted ?? Theme.of(context).colorScheme.onSurfaceVariant;
    final header = Padding(
      padding: const EdgeInsets.fromLTRB(
        HnSpacing.step4,
        HnSpacing.step2,
        HnSpacing.step4,
        HnSpacing.step2,
      ),
      child: Row(
        children: [
          Expanded(
            child: Text(
              title,
              style: TextStyle(
                fontSize: HnTypo.tSm,
                color: muted,
                fontWeight: FontWeight.w600,
              ),
            ),
          ),
          // 用 null-aware 元素（lint: use_null_aware_elements）：
          // `if (x != null) x!` 的等价写法是 `?x`，更短且不丢空安全。
          ?trailing,
        ],
      ),
    );
    if (variant == HnSettingsVariant.plain) {
      return Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [header, ...children],
      );
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        header,
        // 必须用 Material 而非 Container+BoxDecoration：
        // `ListTile` 把背景与水波纹画在**最近的 Material 祖先**上，
        // 若中间夹一层带背景色的 DecoratedBox，水波纹与背景会被它盖住
        // ——Flutter 会直接抛断言「ListTile background color or ink splashes
        // may be invisible」（我在全量测试里被这条拦住，11 处异常）。
        // Material 同时提供背景色、圆角与裁剪，是 ListTile 的正确宿主。
        Material(
          color: tokens?.card ?? Theme.of(context).colorScheme.surface,
          borderRadius: BorderRadius.circular(HnSpacing.radiusLg),
          clipBehavior: Clip.antiAlias,
          child: Column(children: children),
        ),
      ],
    );
  }
}

enum HnSettingsVariant {
  /// 手机竖屏：分组标题 + 一张卡（§4.3.3）。
  card,

  /// 平板横屏：右侧内容区直接铺开，不再套卡（已在两栏结构里）。
  plain,
}
