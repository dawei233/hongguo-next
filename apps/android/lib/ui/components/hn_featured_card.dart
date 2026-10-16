import 'package:flutter/material.dart';

import '../../core_bridge.dart';
import '../../models.dart';
import '../../widgets.dart';
import '../tokens.dart';

/// 发现页「今日首选」大卡（`ANDROID-UI-REDESIGN.md` §4.1.1）。
///
/// | 规范项 | 值 |
/// |---|---|
/// | 高 | 148 |
/// | 底 | `card` + `radius-lg` |
/// | 内边距 | `step4 (16)` |
/// | 左 | 海报 3:4 宽 96 |
/// | 中 | 剧名 `tLg/w700` + 题材·集数 `tSm muted` + 简介 `tBase` line-clamp-2 |
/// | 右 | `追剧`(outline) + `继续第 N 集`(filled primary) |
///
/// ## 数据诚实性
///
/// 它展示的是**推荐位的第一条**（`repository.recommendations`）。标题「今日首选」
/// 是位置名而非承诺 —— 不编造"编辑推荐""今日必看"这类没有数据支撑的说法
/// （与 §4.1.1 对空态的要求同源）。取不到数据时由调用方**整块不渲染**，
/// 而不是显示一张空卡。
class HnFeaturedCard extends StatelessWidget {
  const HnFeaturedCard({
    super.key,
    required this.drama,
    required this.repository,
    required this.onOpen,
    this.onContinue,
    this.onFollow,
    this.following = false,
    this.watchedEpisode,
    this.episodeCount = 0,
  });

  final Drama drama;
  final AppRepository repository;
  final VoidCallback onOpen;

  /// 「继续第 N 集」；为 null 时不显示该按钮（没有观看进度时）。
  final VoidCallback? onContinue;

  /// [`null`] 时不显示「追剧」按钮。
  final VoidCallback? onFollow;
  final bool following;

  /// 已看到第几集（用于按钮文案）。null = 没有进度。
  final int? watchedEpisode;
  final int episodeCount;

  static const double height = 148;
  static const double posterWidth = 96;

  @override
  Widget build(BuildContext context) {
    final tokens = Theme.of(context).extension<HongguoTokens>();
    final fg = tokens?.fg ?? Theme.of(context).colorScheme.onSurface;
    final muted = tokens?.fgMuted ?? Theme.of(context).colorScheme.onSurfaceVariant;
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: HnSpacing.step5),
      child: Container(
        key: const ValueKey('featured-card'),
        height: height,
        padding: const EdgeInsets.all(HnSpacing.step4),
        decoration: BoxDecoration(
          color: tokens?.card ?? Theme.of(context).colorScheme.surface,
          borderRadius: BorderRadius.circular(HnSpacing.radiusLg),
        ),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            // 左：海报 3:4 宽 96（§4.1.1）。
            SizedBox(
              width: posterWidth,
              child: AspectRatio(
                aspectRatio: 3 / 4,
                child: ClipRRect(
                  borderRadius: BorderRadius.circular(HnSpacing.radiusMd),
                  child: DramaCover(drama: drama, repository: repository),
                ),
              ),
            ),
            const SizedBox(width: HnSpacing.step4),
            // 中：标题/副行/简介。
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                mainAxisSize: MainAxisSize.min,
                children: [
                  Text(
                    drama.title,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(
                      fontSize: HnTypo.tLg,
                      fontWeight: FontWeight.w700,
                      color: fg,
                    ),
                  ),
                  const SizedBox(height: 2),
                  Text(
                    _meta(drama, episodeCount),
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(fontSize: HnTypo.tSm, color: muted),
                  ),
                  const SizedBox(height: HnSpacing.step2),
                  if (drama.description.isNotEmpty)
                    Text(
                      drama.description,
                      // §4.1.1：简介 line-clamp-2。
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(fontSize: HnTypo.tBase, color: muted),
                    ),
                ],
              ),
            ),
            const SizedBox(width: HnSpacing.step4),
            // 右：追剧（outline）+ 继续第 N 集（filled primary）。
            Column(
              mainAxisAlignment: MainAxisAlignment.center,
              mainAxisSize: MainAxisSize.min,
              children: [
                if (onFollow != null)
                  OutlinedButton(
                    key: const ValueKey('featured-follow'),
                    onPressed: onFollow,
                    style: OutlinedButton.styleFrom(
                      minimumSize: const Size(96, HnSpacing.step10),
                      textStyle: TextStyle(fontSize: HnTypo.tSm),
                    ),
                    child: Text(following ? '已追剧' : '追剧'),
                  ),
                if (onContinue != null) ...[
                  const SizedBox(height: HnSpacing.step2),
                  FilledButton(
                    key: const ValueKey('featured-continue'),
                    onPressed: onContinue,
                    style: FilledButton.styleFrom(
                      minimumSize: const Size(96, HnSpacing.step10),
                      textStyle: TextStyle(fontSize: HnTypo.tSm),
                    ),
                    child: Text(
                      watchedEpisode == null
                          ? '继续观看'
                          : '继续第 $watchedEpisode 集',
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                ],
                if (onFollow == null && onContinue == null)
                  TextButton(
                    key: const ValueKey('featured-open'),
                    onPressed: onOpen,
                    child: const Text('查看'),
                  ),
              ],
            ),
          ],
        ),
      ),
    );
  }

  /// 副行：题材 · 集数（§4.1.1）；两者都缺时退回站源无关的空串。
  static String _meta(Drama drama, int episodeCount) {
    final total = episodeCount > 0 ? episodeCount : drama.episodes;
    return [
      if (drama.category.isNotEmpty) drama.category,
      if (total > 0) '$total 集',
    ].join(' · ');
  }
}
