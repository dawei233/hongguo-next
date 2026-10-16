import 'dart:math';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'models.dart';
import 'ui/layout.dart';
import 'remote_widgets.dart';

const episodePageSize = 50;

class EpisodeRangeBar extends StatelessWidget {
  const EpisodeRangeBar({
    super.key,
    required this.episodes,
    required this.page,
    required this.onLocate,
    this.currentNumber,
    this.title = '选集',
  });
  final List<Episode> episodes;
  final int page;
  final ValueChanged<int> onLocate;
  final int? currentNumber;
  final String title;

  Future<void> _jump(BuildContext context) async {
    final controller = TextEditingController();
    String? error;
    final index = await showDialog<int>(
      context: context,
      builder: (context) => StatefulBuilder(
        builder: (context, setState) {
          void submit() {
            final number = int.tryParse(controller.text.trim());
            final found = episodes.indexWhere(
              (episode) => episode.number == number,
            );
            if (found < 0) {
              setState(() => error = '没有找到这一集，请输入已有集数');
            } else {
              Navigator.pop(context, found);
            }
          }

          return AlertDialog(
            title: const Text('跳转到指定集数'),
            content: TextField(
              controller: controller,
              autofocus: true,
              keyboardType: TextInputType.number,
              inputFormatters: [FilteringTextInputFormatter.digitsOnly],
              decoration: InputDecoration(labelText: '集数', errorText: error),
              onSubmitted: (_) => submit(),
            ),
            actions: [
              TextButton(
                onPressed: () => Navigator.pop(context),
                child: const Text('取消'),
              ),
              FilledButton(onPressed: submit, child: const Text('定位')),
            ],
          );
        },
      ),
    );
    controller.dispose();
    if (index != null && context.mounted) onLocate(index);
  }

  @override
  Widget build(BuildContext context) {
    final count = (episodes.length / episodePageSize).ceil();
    final current = episodes.indexWhere(
      (episode) => episode.number == currentNumber,
    );
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 6),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Text(
                  '$title · ${episodes.length} 集',
                  style: Theme.of(context).textTheme.titleMedium,
                ),
              ),
              if (current >= 0)
                TextButton(
                  onPressed: () => onLocate(current),
                  child: const Text('定位当前'),
                ),
              IconButton(
                tooltip: '跳转集数',
                onPressed: episodes.isEmpty ? null : () => _jump(context),
                icon: const Icon(Icons.pin_outlined),
              ),
            ],
          ),
          if (count > 1)
            Row(
              children: [
                IconButton(
                  tooltip: '上一组选集',
                  onPressed: page > 0
                      ? () => onLocate((page - 1) * episodePageSize)
                      : null,
                  icon: const Icon(Icons.chevron_left_rounded),
                ),
                Expanded(
                  child: DropdownButtonHideUnderline(
                    child: DropdownButton<int>(
                      key: const ValueKey('episode-range'),
                      isExpanded: true,
                      value: page.clamp(0, count - 1),
                      items: [
                        for (var i = 0; i < count; i++)
                          DropdownMenuItem(
                            value: i,
                            child: Text(
                              '第 ${episodes[i * episodePageSize].number}–${episodes[min((i + 1) * episodePageSize, episodes.length) - 1].number} 集',
                            ),
                          ),
                      ],
                      onChanged: (value) {
                        if (value != null) onLocate(value * episodePageSize);
                      },
                    ),
                  ),
                ),
                IconButton(
                  tooltip: '下一组选集',
                  onPressed: page + 1 < count
                      ? () => onLocate((page + 1) * episodePageSize)
                      : null,
                  icon: const Icon(Icons.chevron_right_rounded),
                ),
              ],
            ),
        ],
      ),
    );
  }
}

class EpisodeBrowser extends StatefulWidget {
  const EpisodeBrowser({
    super.key,
    required this.episodes,
    required this.onSelected,
    this.currentNumber,
    this.selectedNumbers,
    this.keyPrefix = 'episode',
    this.title = '选集',
    this.compact = false,
    this.watchedNumbers,
  });
  final List<Episode> episodes;
  final ValueChanged<int> onSelected;
  final int? currentNumber;
  final Set<int>? selectedNumbers;

  /// §4.2.2：已看过的集号集合（显示 ✓ 并降色）。null 表示不显示已看标记。
  final Set<int>? watchedNumbers;
  final String keyPrefix;
  final String title;
  final bool compact;
  @override
  State<EpisodeBrowser> createState() => _EpisodeBrowserState();
}

class _EpisodeBrowserState extends State<EpisodeBrowser> {
  final _scroll = ScrollController();
  String _layout = '';
  int get _pageSize => episodePageSize;
  late int _page =
      max(
        0,
        widget.episodes.indexWhere((e) => e.number == widget.currentNumber),
      ) ~/
      _pageSize;
  int? _located;

  @override
  void didUpdateWidget(EpisodeBrowser oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.currentNumber != widget.currentNumber) {
      _page =
          max(
            0,
            widget.episodes.indexWhere(
              (episode) => episode.number == widget.currentNumber,
            ),
          ) ~/
          _pageSize;
      _located = null;
    }
    _page = _page.clamp(0, max(0, (widget.episodes.length - 1) ~/ _pageSize));
  }

  @override
  void dispose() {
    _scroll.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (widget.compact) return _continuousGrid(context);
    final pageSize = _pageSize;
    _page = _page.clamp(0, max(0, (widget.episodes.length - 1) ~/ pageSize));
    final start = _page * pageSize;
    final visible = widget.episodes.skip(start).take(pageSize).toList();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        EpisodeRangeBar(
          episodes: widget.episodes,
          page: _page,
          title: widget.title,
          currentNumber: widget.currentNumber,
          onLocate: (index) => setState(() {
            _page = index ~/ episodePageSize;
            _located = widget.episodes[index].number;
          }),
        ),
        Expanded(
          child: LayoutBuilder(
            builder: (context, constraints) {
              final digits = visible.fold<int>(
                1,
                (value, episode) =>
                    max(value, episode.number.toString().length),
              );
              final scale = MediaQuery.textScalerOf(context);
              // §4.2.2（B5）：播放器选集侧栏内**固定 4 列**（compact 模式）。
              //
              // 规范原文：「体：4 列 × 44px（HnBreak.episodeColumns 返回 8 是按整屏
              // 算的，侧栏内固定 4）」。侧栏宽 300 与整屏宽是两回事 —— 用整屏算法
              // 会得到 8 列，把 44px 的格子挤到 30px 出头。故 compact 走常量。
              //
              // 非 compact 的调用点（详情页 / 下载选择器）保持原算法：那些确实按
              // 整屏宽排布，且要照顾集号位数与系统文字缩放。
              final columns = widget.compact
                  ? HnBreak.episodeSidebarColumns
                  // B7c：原 `max(television ? 100 : 82, ...)` ——
                  // TV 档 100（格子更大）随 TV 下线删除，恒取非 TV 档 82。
                  : ((constraints.maxWidth - 36) /
                            max(
                              82,
                              scale.scale(20) * digits * .65 + 40,
                            ))
                        .floor()
                        .clamp(1, 12);
              final extent = widget.compact
                  ? HnBreak.episodeSidebarExtent
                  : max(56.0, scale.scale(20) + 30);
              final target = max(
                0,
                visible.indexWhere(
                  (episode) =>
                      episode.number == (_located ?? widget.currentNumber),
                ),
              );
              final layout =
                  '$_page:$_located:${widget.currentNumber}:$columns:$extent:${constraints.maxHeight}';
              if (layout != _layout) {
                _layout = layout;
                WidgetsBinding.instance.addPostFrameCallback((_) {
                  if (!mounted || !_scroll.hasClients || _layout != layout) {
                    return;
                  }
                  _scroll.jumpTo(
                    (18 +
                            target ~/ columns * (extent + 8) -
                            constraints.maxHeight / 2 +
                            extent / 2)
                        .clamp(0.0, _scroll.position.maxScrollExtent),
                  );
                });
              }
              return RemoteGrid(
                key: ValueKey('episode-page-$_page-$_located'),
                controller: _scroll,
                // B7c：原 `autofocus: television && _located != null` ——
                // 非 TV 一直是 false（触摸不需要初始焦点），删除后行为不变。
                autofocus: false,
                initialIndex: target,
                itemKeys: visible
                    .map((episode) => '${episode.number}')
                    .toList(),
                columns: columns,
                itemExtent: extent,
                spacing: widget.compact ? 6 : 8,
                padding: widget.compact
                    ? const EdgeInsets.fromLTRB(4, 4, 4, 12)
                    : const EdgeInsets.fromLTRB(18, 2, 18, 18),
                itemBuilder: (_, index, node, onFocus) {
                  final episode = visible[index];
                  return RemoteEpisodeButton(
                    key: ValueKey('${widget.keyPrefix}-${episode.number}'),
                    number: episode.number,
                    vip: episode.vip,
                    compact: widget.compact,
                    // §4.2.2：已看过的集显示 ✓ 并降色。由调用方传入集合
                    //（播放器侧按 store.watched(dramaId) 的进度算）。
                    watched: widget.watchedNumbers?.contains(episode.number) ??
                        false,
                    current:
                        widget.selectedNumbers?.contains(episode.number) ??
                        episode.number == widget.currentNumber,
                    focusNode: node,
                    onFocus: onFocus,
                    onPressed: () => widget.onSelected(start + index),
                  );
                },
              );
            },
          ),
        ),
      ],
    );
  }

  Widget _compactRangeSelector({required int target, required int pageCount}) {
    if (pageCount <= 1) return const SizedBox.shrink();
    final colors = Theme.of(context).colorScheme;
    final selectedPage = (target ~/ episodePageSize).clamp(0, pageCount - 1);
    return SizedBox(
      height: 34,
      child: ListView.separated(
        padding: const EdgeInsets.fromLTRB(4, 2, 4, 3),
        scrollDirection: Axis.horizontal,
        itemCount: pageCount,
        separatorBuilder: (_, _) => const SizedBox(width: 6),
        itemBuilder: (context, page) {
          final start = page * episodePageSize;
          final end = min(start + episodePageSize, widget.episodes.length) - 1;
          final selected = page == selectedPage;
          return OutlinedButton(
            style: OutlinedButton.styleFrom(
              minimumSize: const Size(0, 28),
              tapTargetSize: MaterialTapTargetSize.shrinkWrap,
              padding: const EdgeInsets.symmetric(horizontal: 10),
              foregroundColor: selected ? colors.onPrimary : colors.onSurface,
              backgroundColor: selected ? colors.primary : Colors.transparent,
              side: BorderSide(
                color: selected ? colors.primary : colors.outlineVariant,
              ),
              textStyle: const TextStyle(fontSize: 12),
              shape: RoundedRectangleBorder(
                borderRadius: BorderRadius.circular(7),
              ),
            ),
            onPressed: () {
              setState(() {
                _located = widget.episodes[start].number;
              });
            },
            child: Text(
              '${widget.episodes[start].number}-${widget.episodes[end].number}',
            ),
          );
        },
      ),
    );
  }

  Widget _continuousGrid(BuildContext context) => LayoutBuilder(
    builder: (context, constraints) {
      // §4.2.2（B5）：**播放器选集侧栏内固定 4 列 × 44px**。
      //
      // 这里原先是 compact 专用的一套算术
      // （`((maxWidth-12)/minimum).floor().clamp(5, 10|12)`），下限硬编码 5、
      // 且按集号位数与文字缩放浮动 —— 与 §4.2.2 明示的「侧栏内固定 4」冲突
      // （实测 300 宽下得 3–5 列不等，格子宽度随之漂移）。
      //
      // 侧栏只有 300 宽这一个形态，没有"适配不同宽度"的需求，故直接取常量；
      // 44px 的行高同样固定（原实现是 `max(34, textScale*14+18)`）。
      // 注：原先的 `MediaQuery.textScalerOf` 取值随该算术一并删除（已无用处）。
      final columns = HnBreak.episodeSidebarColumns;
      final extent = HnBreak.episodeSidebarExtent;
      final target = max(
        0,
        widget.episodes.indexWhere(
          (episode) => episode.number == (_located ?? widget.currentNumber),
        ),
      );
      final pageCount = (widget.episodes.length / episodePageSize).ceil();
      final layout =
          'compact:${widget.episodes.length}:$_located:${widget.currentNumber}:$columns:$extent:${constraints.maxHeight}';
      if (layout != _layout) {
        _layout = layout;
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (!mounted || !_scroll.hasClients || _layout != layout) return;
          _scroll.jumpTo(
            (target ~/ columns * (extent + 6) -
                    constraints.maxHeight / 2 +
                    extent / 2)
                .clamp(0.0, _scroll.position.maxScrollExtent),
          );
        });
      }
      return Column(
        children: [
          _compactRangeSelector(target: target, pageCount: pageCount),
          Expanded(
            child: RemoteGrid(
              key: ValueKey('episode-continuous-$_located'),
              controller: _scroll,
              // B7c：同 `_continuousGrid`，非 TV 一直是 false。
              autofocus: false,
              initialIndex: target,
              itemKeys: widget.episodes
                  .map((episode) => '${episode.number}')
                  .toList(),
              columns: columns,
              itemExtent: extent,
              spacing: 5,
              padding: const EdgeInsets.fromLTRB(4, 6, 4, 4),
              itemBuilder: (_, index, node, onFocus) {
                final episode = widget.episodes[index];
                return RemoteEpisodeButton(
                  key: ValueKey('${widget.keyPrefix}-${episode.number}'),
                  number: episode.number,
                  vip: episode.vip,
                  compact: true,
                  watched: widget.watchedNumbers?.contains(episode.number) ??
                      false,
                  current:
                      widget.selectedNumbers?.contains(episode.number) ??
                      episode.number == widget.currentNumber,
                  focusNode: node,
                  onFocus: onFocus,
                  onPressed: () => widget.onSelected(index),
                );
              },
            ),
          ),
        ],
      );
    },
  );
}
