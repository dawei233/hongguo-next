import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hongguo_next/app_theme.dart';
import 'package:hongguo_next/ui/components/hn_section_header.dart';

/// `ANDROID-UI-REDESIGN.md` §5.1 第 3 条的回归测试：区块头。
///
/// 发现页首屏从"筛选器 + 一片网格"改为内容流（Hero 轨 → 今日首选 → 区块头 +
/// 海报墙），区块头是其中的分隔元素，也是"筛选收进 Sheet"后首屏仍能表达
/// 分组层级的方式。
void main() {
  Future<void> pump(
    WidgetTester tester, {
    String? actionLabel,
    VoidCallback? onAction,
  }) async {
    await tester.pumpWidget(
      MaterialApp(
        // 必须用 AppTheme.light：它把 HongguoTokens 装进 extensions。
        theme: AppTheme.light,
        home: Scaffold(
          body: HnSectionHeader(
            title: '热门短剧',
            actionLabel: actionLabel,
            onAction: onAction,
          ),
        ),
      ),
    );
  }

  testWidgets('§5.1 第 3 条：内容行高 40，标题 tMd/w600', (tester) async {
    await pump(tester);
    // 规范 §5.1 第 3 条的「高 40」是**内容行**的高度（标题 + 左竖条 + 动作），
    // 不含组件外侧的区块间距（上 step6=24 / 下 step2=8，见 §4.1.1 的
    // 「上间距 step8」说明 —— 间距属调用方排版，不是组件自身高度）。
    // 故断言内部那个 SizedBox 的高，而不是整个 HnSectionHeader 的高。
    expect(
      tester
          .getSize(find.byKey(const ValueKey('section-header-row')))
          .height,
      HnSectionHeader.height,
    );
    expect(HnSectionHeader.height, 40);
    final title = tester.widget<Text>(find.text('热门短剧'));
    expect(title.style?.fontSize, 15);
    expect(title.style?.fontWeight, FontWeight.w600);
  });

  testWidgets('无 actionLabel 时不渲染动作（plain 变体）', (tester) async {
    await pump(tester);
    expect(find.byKey(const ValueKey('section-action')), findsNothing);
  });

  testWidgets('with-action：动作文案可点且回传（tSm / primary）', (tester) async {
    var tapped = false;
    await pump(
      tester,
      actionLabel: '全部 ›',
      onAction: () => tapped = true,
    );
    expect(find.text('全部 ›'), findsOneWidget);
    final action = tester.widget<TextButton>(
      find.byKey(const ValueKey('section-action')),
    );
    expect(action.style?.textStyle?.resolve({})?.fontSize, 12);
    await tester.tap(find.byKey(const ValueKey('section-action')));
    await tester.pumpAndSettle();
    expect(tapped, isTrue);
  });
}
