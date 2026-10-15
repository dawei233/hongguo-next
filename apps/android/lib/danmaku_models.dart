const danmakuWindowMs = 30000;
const danmakuLifetimeMs = 8000;
const danmakuMaxDurationMs = 86400000;

class DanmakuItem {
  const DanmakuItem({
    required this.id,
    required this.text,
    required this.timeMs,
  });
  final String id;
  final String text;
  final int timeMs;

  /// 弹幕的内容身份：同一时间点上的同一句话视为同一条。
  ///
  /// 上游按游标分批返回子集，同一条弹幕会在不同批次里带着不同的 id 再次出现，
  /// 因此跨批次去重不能依赖 id（详见 native/core/ui_playback_danmaku.go 的说明）。
  String get identity => '$timeMs|$text';
}

class DanmakuPage {
  const DanmakuPage({
    required this.episodeId,
    required this.startMs,
    required this.nextMs,
    required this.items,
  });
  final String episodeId;
  final int startMs;
  final int nextMs;
  final List<DanmakuItem> items;

  factory DanmakuPage.fromJson(
    Map<String, dynamic> json, {
    required String episodeId,
    required int startMs,
    required int durationMs,
  }) {
    final next = json['nextMs'];
    final rows = json['items'];
    if (json['episodeId'] != episodeId ||
        json['startMs'] != startMs ||
        next is! int ||
        next <= startMs ||
        next > durationMs ||
        rows is! List) {
      throw const FormatException('弹幕分集或时间范围不符');
    }
    final items = <DanmakuItem>[];
    // 两道去重都要保留：
    //  - seenId：同一批次里同一个 id 被重复使用（内容却不同）属于异常数据，沿用旧行为拒收；
    //  - seenIdentity：上游会把同一条弹幕以不同 id 重复给出，这才是"大量重复"的根因。
    final seenId = <String>{};
    final seenIdentity = <String>{};
    for (final row in rows.take(90)) {
      if (row is! Map) continue;
      final id = row['id'], text = row['text'], time = row['timeMs'];
      if (id is! String ||
          id.isEmpty ||
          id.length > 120 ||
          text is! String ||
          time is! int ||
          time < startMs ||
          time >= next ||
          seenId.contains(id)) {
        continue;
      }
      final cleaned = text
          .replaceAll(RegExp(r'[\x00-\x1f\x7f-\x9f\u2028\u2029]'), ' ')
          .trim();
      if (cleaned.isEmpty) continue;
      final item = DanmakuItem(
        id: id,
        text: String.fromCharCodes(cleaned.runes.take(181)),
        timeMs: time,
      );
      if (!seenIdentity.add(item.identity)) continue;
      seenId.add(id);
      items.add(item);
    }
    items.sort((a, b) => a.timeMs.compareTo(b.timeMs));
    return DanmakuPage(
      episodeId: episodeId,
      startMs: startMs,
      nextMs: next,
      items: List.unmodifiable(items),
    );
  }
}
