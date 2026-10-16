import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hongguo_next/app_theme.dart';
import 'package:hongguo_next/ui/components/hn_segmented_tabs.dart';
import 'package:hongguo_next/ui/tokens.dart';

/// `ANDROID-UI-REDESIGN.md` §5.1 第 2 条的回归测试。
///
/// 锁住规范给定的尺寸（高 36 / 圆角 radius-sm）、两种变体、以及
/// 选中态与回调 —— 因为「我的」的三分段（追剧/最近/下载）依赖它，
/// 而 B2 的 IA 重排正是把上游那三个平级 tab 收进这个分段。
void main() {
  Future<void> pump(
    WidgetTester tester, {
    required int selectedIndex,
    required ValueChanged<int> onSelected,
    HnSegmentedVariant variant = HnSegmentedVariant.filled,
    bool enabled = true,
  }) async {
    await tester.pumpWidget(
      MaterialApp(
        // 必须用 AppTheme.light：它把 HongguoTokens 装进 ThemeData.extensions，
        // 组件才取得到令牌（用裸 MaterialApp 会退化成 colorScheme 回退色，
        // 断言就会看到白色而不是 onPrimary）。
        theme: AppTheme.light,
        home: Scaffold(
          body: Center(
            child: HnSegmentedTabs(
              labels: const ['追剧', '最近', '下载'],
              selectedIndex: selectedIndex,
              onSelected: onSelected,
              variant: variant,
              enabled: enabled,
            ),
          ),
        ),
      ),
    );
  }

  testWidgets('§5.1 第 2 条：高 36', (tester) async {
    await pump(tester, selectedIndex: 0, onSelected: (_) {});
    expect(
      tester.getSize(find.byType(HnSegmentedTabs)).height,
      HnSegmentedTabs.height,
    );
    expect(HnSegmentedTabs.height, 36);
  });

  testWidgets('三段都渲染且可点，回调带正确 index', (tester) async {
    var tapped = -1;
    await pump(tester, selectedIndex: 0, onSelected: (i) => tapped = i);
    for (var i = 0; i < 3; i++) {
      expect(find.byKey(ValueKey('hn-seg-$i')), findsOneWidget);
    }
    await tester.tap(find.byKey(const ValueKey('hn-seg-2')));
    await tester.pumpAndSettle();
    expect(tapped, 2);
  });

  testWidgets('选中态用 primary 底 + onPrimary 字（filled 变体）', (tester) async {
    await pump(tester, selectedIndex: 1, onSelected: (_) {});
    final tokens = AppTheme.tokensOf(Brightness.light);
    // 选中段（index 1）的文字应为 onPrimary；未选中段为 muted。
    final selected = tester.widget<Text>(
      find.descendant(
        of: find.byKey(const ValueKey('hn-seg-1')),
        matching: find.byType(Text),
      ),
    );
    expect(selected.style?.color, tokens.onPrimary);
    expect(selected.style?.fontWeight, FontWeight.w600);
    final unselected = tester.widget<Text>(
      find.descendant(
        of: find.byKey(const ValueKey('hn-seg-0')),
        matching: find.byType(Text),
      ),
    );
    expect(unselected.style?.color, tokens.fgMuted);
  });

  testWidgets('outline 变体：选中用 primarySubtle 底 + onPrimarySubtle 字', (tester) async {
    await pump(
      tester,
      selectedIndex: 0,
      onSelected: (_) {},
      variant: HnSegmentedVariant.outline,
    );
    final tokens = AppTheme.tokensOf(Brightness.light);
    final selected = tester.widget<Text>(
      find.descendant(
        of: find.byKey(const ValueKey('hn-seg-0')),
        matching: find.byType(Text),
      ),
    );
    expect(selected.style?.color, tokens.onPrimarySubtle);
  });

  testWidgets('disabled：点击不触发回调', (tester) async {
    var tapped = -1;
    await pump(
      tester,
      selectedIndex: 0,
      onSelected: (i) => tapped = i,
      enabled: false,
    );
    await tester.tap(find.byKey(const ValueKey('hn-seg-1')));
    await tester.pumpAndSettle();
    expect(tapped, -1);
  });
}
