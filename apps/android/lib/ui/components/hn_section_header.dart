import 'package:flutter/material.dart';

import '../tokens.dart';

/// 区块头（`ANDROID-UI-REDESIGN.md` §5.1 第 3 条）。
///
/// 用于发现页的分区标题（如「热门短剧」+ 右侧「全部 ›」）。
///
/// | 规范项 | 值 |
/// |---|---|
/// | 高 | 40 |
/// | 标题 | `tMd (15) / w600` |
/// | 左标记 | 3×16 `primary` 竖条 |
/// | 右侧动作 | `tSm (12) text-primary` |
///
/// ## 与上游的差异
///
/// 上游发现页把「分类 chip 行」常驻首屏，区块之间没有标题层级 ——
/// 用户看到的是"筛选器 + 一片网格"。规范 §4.1.1 要求首屏是**内容流**
/// （Hero 轨 → 今日首选 → 区块头 + 海报墙），区块头是其中的分隔元素，
/// 也是"筛选收进 Sheet"之后首屏仍能表达分组的方式。
class HnSectionHeader extends StatelessWidget {
  const HnSectionHeader({
    super.key,
    required this.title,
    this.actionLabel,
    this.onAction,
    this.padding = const EdgeInsets.fromLTRB(
      HnSpacing.step5,
      HnSpacing.step6,
      HnSpacing.step5,
      HnSpacing.step2,
    ),
  });

  final String title;

  /// 右侧动作文案（如「全部 ›」）。为 null 时不渲染动作。
  final String? actionLabel;
  final VoidCallback? onAction;
  final EdgeInsets padding;

  /// 规范 §5.1 第 3 条：高 40。
  static const double height = 40;

  @override
  Widget build(BuildContext context) {
    final tokens = Theme.of(context).extension<HongguoTokens>();
    final primary = tokens?.primary ?? Theme.of(context).colorScheme.primary;
    final fg = tokens?.fg ?? Theme.of(context).colorScheme.onSurface;
    return Padding(
      padding: padding,
      child: SizedBox(
        // 测试用键：规范 §5.1 第 3 条的「高 40」指这个内容行，
        // 不含外侧区块间距（上 step6 / 下 step2，属调用方排版）。
        key: const ValueKey('section-header-row'),
        height: height,
        child: Row(
          children: [
            // 左 3×16 primary 竖条。
            Container(
              width: 3,
              height: 16,
              decoration: BoxDecoration(
                color: primary,
                borderRadius: BorderRadius.circular(1.5),
              ),
            ),
            const SizedBox(width: HnSpacing.step3),
            Expanded(
              child: Text(
                title,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: TextStyle(
                  fontSize: HnTypo.tMd,
                  fontWeight: FontWeight.w600,
                  color: fg,
                ),
              ),
            ),
            if (actionLabel != null) ...[
              const SizedBox(width: HnSpacing.step2),
              TextButton(
                key: const ValueKey('section-action'),
                onPressed: onAction,
                style: TextButton.styleFrom(
                  minimumSize: const Size(0, HnSpacing.step10),
                  padding: const EdgeInsets.symmetric(
                    horizontal: HnSpacing.step2,
                  ),
                  foregroundColor: primary,
                  textStyle: TextStyle(fontSize: HnTypo.tSm),
                ),
                child: Text(actionLabel!),
              ),
            ],
          ],
        ),
      ),
    );
  }
}
