import 'package:flutter/material.dart';

import 'catalog_sort.dart';
import 'models.dart';

/// 「排序与筛选」Sheet 的返回结果（§5.1 第 10 条 `HnFilterSheet`）。
///
/// B2 首屏重构把**分类选择**（原先常驻首屏的 `CatalogFilters` chip 行）
/// 一并收进这个 Sheet —— 规范 §4.1.1 要求筛选低频化、首屏让位给内容流。
/// 因此 Sheet 现在同时返回分类与排序/筛选，调用方一次应用两个变更。
class CatalogFilterResult {
  const CatalogFilterResult({
    required this.category,
    required this.view,
    this.hideVip,
  });

  /// 选中的分类 id（`''` = 全部）。与 `CatalogFilters.onCategory` 同语义。
  final String category;
  final CatalogView view;

  /// VIP 显隐（null = 本 Sheet 未提供该项，调用方不应改动）。
  ///
  /// B2 把原先首屏的 `VipIcon` 开关一并收进这里 —— 否则非 TV 形态下
  /// 这个用户可见的控制就**没有入口**了（不能只删不搬）。
  final bool? hideVip;
}

Future<CatalogFilterResult?> chooseCatalogView(
  BuildContext context,
  CatalogView current, {
  // 新增可选参数：分类候选与当前分类。不传时 Sheet 只显示排序/筛选
  // （保持既有调用方行为不变）。
  List<CatalogCategory> categories = const [],
  String category = '',
  bool? hideVip,
}) => showModalBottomSheet<CatalogFilterResult>(
  context: context,
  showDragHandle: true,
  isScrollControlled: true,
  useSafeArea: true,
  builder: (context) {
    var selected = current;
    var selectedCategory = category;
    var selectedHideVip = hideVip;
    return StatefulBuilder(
      builder: (context, update) => SingleChildScrollView(
        padding: EdgeInsets.fromLTRB(
          20,
          0,
          20,
          20 + MediaQuery.viewInsetsOf(context).bottom,
        ),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('排序与筛选', style: Theme.of(context).textTheme.titleLarge),
            // 分类选择（B2）：原先常驻首屏的 chip 行收进这里。
            if (categories.isNotEmpty) ...[
              const SizedBox(height: 16),
              const Text('分类'),
              const SizedBox(height: 8),
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                  for (final entry in categories)
                    ChoiceChip(
                      // 键沿用 `category-<id>`：首屏 chip 行迁走后，
                      // 既有测试的 `find.widgetWithText(ChoiceChip, ...)`
                      // 语义不变（仍在 ChoiceChip 上，只是换了宿主）。
                      key: ValueKey('category-${entry.id}'),
                      label: Text(entry.name),
                      selected: selectedCategory == entry.id,
                      showCheckmark: false,
                      onSelected: (_) =>
                          update(() => selectedCategory = entry.id),
                    ),
                ],
              ),
            ],
            if (selectedHideVip != null) ...[
              const SizedBox(height: 16),
              const Text('VIP 内容'),
              const SizedBox(height: 8),
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                  for (final entry in const {
                    false: '显示',
                    true: '隐藏',
                  }.entries)
                    ChoiceChip(
                      // 键沿用首屏开关的 tooltip 语义（`VIP：显示/隐藏`），
                      // 使既有测试的 `find.byTooltip('VIP：隐藏')` 断言
                      // 在迁移后仍指向"同一个用户控制"。
                      key: ValueKey('vip-${entry.key}'),
                      label: Text('VIP：${entry.value}'),
                      selected: selectedHideVip == entry.key,
                      showCheckmark: false,
                      onSelected: (_) =>
                          update(() => selectedHideVip = entry.key),
                    ),
                ],
              ),
            ],
            const SizedBox(height: 16),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                for (final sort in CatalogSort.values)
                  ChoiceChip(
                    label: Text(sort.label),
                    selected: selected.sort == sort,
                    onSelected: (_) =>
                        update(() => selected = selected.copyWith(sort: sort)),
                  ),
              ],
            ),
            const SizedBox(height: 24),
            const Text('剧集状态'),
            const SizedBox(height: 8),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                for (final entry in const {
                  '': '全部',
                  'ongoing': '连载中',
                  'finished': '已完结',
                  'unknown': '状态未知',
                }.entries)
                  ChoiceChip(
                    label: Text(entry.value),
                    selected: selected.release == entry.key,
                    onSelected: (_) => update(
                      () => selected = selected.copyWith(release: entry.key),
                    ),
                  ),
              ],
            ),
            const SizedBox(height: 16),
            Text(
              '排序和筛选作用于已加载的剧集；缺少排序资料的条目排在最后。',
              style: Theme.of(context).textTheme.bodySmall,
            ),
            const SizedBox(height: 20),
            SizedBox(
              width: double.infinity,
              child: FilledButton(
                onPressed: () => Navigator.pop(
                  context,
                  CatalogFilterResult(
                    category: selectedCategory,
                    view: selected,
                    hideVip: selectedHideVip,
                  ),
                ),
                child: const Text('应用'),
              ),
            ),
          ],
        ),
      ),
    );
  },
);
