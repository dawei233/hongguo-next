import 'package:flutter/material.dart';

import '../../core_bridge.dart';
import '../../models.dart';
import '../../widgets.dart';
import '../tokens.dart';

/// 发现页「正在热播」横轨（`ANDROID-UI-REDESIGN.md` §4.1.1）。
///
/// | 规范项 | 值 |
/// |---|---|
/// | 轨高 | 168 |
/// | 卡宽 | 280 |
/// | 卡间距 | `step3 (12)` |
/// | 卡内容 | 封面(16:9) + 左下 `scrim` 渐变 + 剧名 `tMd/w600` + 副行 `tXs` |
/// | 可见数 | **3.2 张**（第 4 张被裁切＝刻意的"还有更多"暗示） |
///
/// ## 两种形态的差异（用户明确要求"横屏也要搞"）
///
/// - **平板横屏**（主力）：卡宽 280，1200px 下恰好露出 3.2 张 —— 这是规范
///   点名的"裁切暗示"，不是副作用。
/// - **手机竖屏**：可用宽只有 ~350，按 280 卡宽只能露 1.2 张，观感像"半张卡
///   卡在边上"而不是"还有更多"。故竖屏改用较小的卡宽（[portraitCardWidth]），
///   让可见数落在 2.2 左右（§4.1.2 的「2.2 张可见」即此）。
///
/// 关键：**两形态共用同一个 widget 与同一套内部逻辑，只换卡宽** ——
/// 与 §2.2 对控制层的要求同源（形态只影响布局参数，不换实现）。
class HnHeroRail extends StatelessWidget {
  const HnHeroRail({
    super.key,
    required this.items,
    required this.repository,
    required this.onOpen,
    this.height = 168,
    this.cardWidth,
    this.loading = false,
  });

  final List<Drama> items;

  /// 取封面用（`DramaCover` 需要它）。**显式传入而非藏在 InheritedWidget 里**：
  /// 与仓库里其它列表（`SavedLibrary` / `RecommendationsScreen`）的既有做法一致，
  /// 少一层隐式依赖。
  final AppRepository repository;
  final void Function(Drama drama) onOpen;

  /// 轨高（§4.1.1：168）。
  final double height;

  /// 卡宽覆盖值；null 时按形态取默认（横屏 280 / 竖屏 [portraitCardWidth]）。
  final double? cardWidth;

  /// 骨架态（首屏加载中）。骨架比例与真卡一致，避免高度抖动。
  final bool loading;

  /// §4.1.1 的轨高（168）。写成静态常量供调用方与测试引用，
  /// 避免页面上再硬编码一次。
  static const double defaultHeight = 168;

  /// 平板/横屏档卡宽（§4.1.1）。
  static const double landscapeCardWidth = 280;

  /// 手机竖屏档卡宽 —— 由「可用宽 / 2.2」反推，使可见数约 2.2 张（§4.1.2）。
  static const double portraitCardWidth = 150;

  double _widthFor(BuildContext context, double maxWidth) {
    if (cardWidth != null) return cardWidth!;
    final landscape =
        maxWidth > MediaQuery.sizeOf(context).height;
    return landscape ? landscapeCardWidth : portraitCardWidth;
  }

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        final width = _widthFor(context, constraints.maxWidth);
        return SizedBox(
          height: height,
          child: loading
              ? _skeleton(context, width)
              : ListView.separated(
                  key: const ValueKey('hero-rail'),
                  scrollDirection: Axis.horizontal,
                  padding: const EdgeInsets.symmetric(
                    horizontal: HnSpacing.step5,
                  ),
                  itemCount: items.length,
                  separatorBuilder: (_, _) =>
                      const SizedBox(width: HnSpacing.step3),
                  itemBuilder: (context, index) => _card(
                    context,
                    items[index],
                    width,
                  ),
                ),
        );
      },
    );
  }

  Widget _card(BuildContext context, Drama drama, double width) {
    final tokens = Theme.of(context).extension<HongguoTokens>();
    return SizedBox(
      width: width,
      child: GestureDetector(
        key: ValueKey('hero-${drama.id}'),
        onTap: () => onOpen(drama),
        child: ClipRRect(
          borderRadius: BorderRadius.circular(HnSpacing.radiusXl),
          child: Stack(
            fit: StackFit.expand,
            children: [
              DramaCover(drama: drama, repository: repository),
              // 左下 scrim 渐变（§4.1.1）：保证白字在任何封面上可读。
              const DecoratedBox(
                decoration: BoxDecoration(
                  gradient: LinearGradient(
                    begin: Alignment.center,
                    end: Alignment.bottomCenter,
                    colors: [Colors.transparent, Color(0xCC000000)],
                  ),
                ),
              ),
              Positioned(
                left: HnSpacing.step3,
                right: HnSpacing.step3,
                bottom: HnSpacing.step3,
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Text(
                      drama.title,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(
                        fontSize: HnTypo.tMd,
                        fontWeight: FontWeight.w600,
                        color: tokens?.danmaku ?? Colors.white,
                      ),
                    ),
                    const SizedBox(height: 2),
                    Text(
                      _subtitle(drama),
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(
                        fontSize: HnTypo.tXs,
                        color: (tokens?.danmaku ?? Colors.white)
                            .withValues(alpha: .78),
                      ),
                    ),
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _skeleton(BuildContext context, double width) {
    final tokens = Theme.of(context).extension<HongguoTokens>();
    Widget block(double w) => Container(
      width: w,
      height: height,
      margin: const EdgeInsets.only(right: HnSpacing.step3),
      decoration: BoxDecoration(
        color: tokens?.skeleton,
        borderRadius: BorderRadius.circular(HnSpacing.radiusXl),
      ),
    );
    // 骨架比例与真卡一致：一张完整 + 一张半张（暗示"还有更多"，§4.1.1 的加载态）。
    return ListView(
      key: const ValueKey('hero-rail-skeleton'),
      scrollDirection: Axis.horizontal,
      physics: const NeverScrollableScrollPhysics(),
      padding: const EdgeInsets.symmetric(horizontal: HnSpacing.step5),
      children: [block(width), block(width * .6)],
    );
  }

  /// 副行：题材 · 集数（缺则退回标签，都缺则不显示空行）。
  static String _subtitle(Drama drama) {
    final parts = [
      if (drama.category.isNotEmpty) drama.category,
      if (drama.episodes > 0) '共 ${drama.episodes} 集',
    ];
    return parts.isEmpty ? drama.tags.take(2).join(' · ') : parts.join(' · ');
  }
}
