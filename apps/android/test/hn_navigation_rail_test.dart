import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hongguo_next/ui/components/hn_navigation_rail.dart';
import 'package:hongguo_next/ui/layout.dart';

/// `ANDROID-UI-REDESIGN.md` §5.1 第 1 条与 §2.1 的导航组件回归测试。
///
/// 锁住三件事：
/// 1. 形态 → 变体的换算（平板 88 / 手机横屏 72 / 手机竖屏 bottom）
/// 2. 三个变体各自渲染出正确宽度与键位
/// 3. 点击回调带上正确的 index（B2 挪 IA 后 tab 语义会变，这条保证组件本身中立）
void main() {
  const destinations = [
    HnRailDestination(
      icon: Icons.explore_outlined,
      selectedIcon: Icons.explore,
      label: '发现',
    ),
    HnRailDestination(
      icon: Icons.bookmark_border_rounded,
      selectedIcon: Icons.bookmark_rounded,
      label: '追剧',
    ),
    HnRailDestination(
      icon: Icons.history_rounded,
      selectedIcon: Icons.history_rounded,
      label: '最近观看',
    ),
  ];

  test('§2.1 形态 → 变体换算', () {
    expect(HnNavigationRail.forForm(HnForm.tabletLandscape), HnRailVariant.wide);
    expect(HnNavigationRail.forForm(HnForm.phoneLandscape), HnRailVariant.narrow);
    expect(HnNavigationRail.forForm(HnForm.phonePortrait), HnRailVariant.bottom);
  });

  Future<void> pump(
    WidgetTester tester,
    HnRailVariant variant, {
    required ValueChanged<int> onSelected,
    int selectedIndex = 0,
  }) async {
    // 左轨（wide/narrow）与底栏（bottom）对父约束的要求不同：
    // - 左轨是固定宽的子项，可放在无界宽里
    // - bottom 用 Expanded 均分，**必须有有限宽约束**
    //   （放无界宽里会报 "RenderFlex children have non-zero flex but incoming
    //   width constraints are unbounded"）
    // 故按变体给不同的宿主：这正是调用方要遵守的契约，测试如实表达它。
    final rail = HnNavigationRail(
      variant: variant,
      selectedIndex: selectedIndex,
      onSelected: onSelected,
      destinations: destinations,
    );
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: variant == HnRailVariant.bottom
              ? Column(children: [const Spacer(), rail])
              : Row(children: [rail]),
        ),
      ),
    );
  }

  testWidgets('wide 变体：左轨宽 88（§2.1 railWide）', (tester) async {
    await pump(tester, HnRailVariant.wide, onSelected: (_) {});
    final rail = find.byType(HnNavigationRail);
    final width = tester.getSize(find.descendant(
      of: rail,
      matching: find.byType(Container),
    ).first).width;
    expect(width, HnBreak.railWide);
    expect(find.byKey(const ValueKey('hn-nav-0')), findsOneWidget);
    expect(find.byKey(const ValueKey('hn-nav-2')), findsOneWidget);
  });

  testWidgets('narrow 变体：左轨宽 72（§2.1 railNarrow）', (tester) async {
    await pump(tester, HnRailVariant.narrow, onSelected: (_) {});
    final rail = find.byType(HnNavigationRail);
    final width = tester.getSize(find.descendant(
      of: rail,
      matching: find.byType(Container),
    ).first).width;
    expect(width, HnBreak.railNarrow);
  });

  testWidgets('bottom 变体：无固定轨宽，每项等宽可点', (tester) async {
    var tapped = -1;
    await pump(tester, HnRailVariant.bottom, onSelected: (i) => tapped = i);
    await tester.tap(find.byKey(const ValueKey('hn-nav-1')));
    await tester.pumpAndSettle();
    expect(tapped, 1);
  });

  testWidgets('点击左轨项回调带上正确 index', (tester) async {
    var tapped = -1;
    await pump(
      tester,
      HnRailVariant.wide,
      onSelected: (i) => tapped = i,
      selectedIndex: 1,
    );
    await tester.tap(find.byKey(const ValueKey('hn-nav-2')));
    await tester.pumpAndSettle();
    expect(tapped, 2);
  });
}
