import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hongguo_next/local_store.dart';
import 'package:hongguo_next/main.dart';
import 'package:hongguo_next/ui/components/hn_navigation_rail.dart';
import 'package:hongguo_next/ui/components/hn_segmented_tabs.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'fixtures.dart';

/// `ANDROID-UI-REDESIGN.md` §3.2 / §7 第 1、2 条的回归测试：**IA 重排**。
///
/// 上游把「追剧 / 最近观看 / 下载」做成三个平级 tab，规范把它们收进一个
/// 「我的」目的地 + 三分段。本文件锁住新 IA 的形状，防止回退。
///
/// 同时锁住一条**边界**：TV 侧边栏（`tv-nav-N`）**不在本次改动内** ——
/// TV 已明确不在范围，16 个 TV 用例还压着，改导航时不能顺手统一它。
void main() {
  Future<LocalStore> mount(WidgetTester tester, Size size) async {
    tester.view.physicalSize = size;
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    await tester.pumpWidget(
      DuanjuApp(repository: FixtureRepository(), store: store),
    );
    await tester.pumpAndSettle();
    return store;
  }

  testWidgets('§7 第 1 条：手机竖屏底栏 3 个目的地（发现/我的/设置）', (tester) async {
    await mount(tester, const Size(390, 844));
    final rail = tester.widget<HnNavigationRail>(
      find.byType(HnNavigationRail),
    );
    expect(rail.variant, HnRailVariant.bottom);
    expect(
      rail.destinations.map((d) => d.label).toList(),
      ['发现', '我的', '设置'],
    );
    // 旧 IA 的「追剧/最近观看/下载」不再出现在底栏。
    expect(rail.destinations.any((d) => d.label == '追剧'), isFalse);
    expect(rail.destinations.any((d) => d.label == '最近观看'), isFalse);
  });

  testWidgets('§7 第 1 条：平板横屏左轨 5 项（含分段快捷入口与设置）', (tester) async {
    await mount(tester, const Size(1200, 800));
    final rail = tester.widget<HnNavigationRail>(
      find.byType(HnNavigationRail),
    );
    expect(rail.variant, HnRailVariant.wide);
    expect(
      rail.destinations.map((d) => d.label).toList(),
      ['发现', '追剧', '最近', '下载', '设置'],
    );
  });

  testWidgets('§7 第 2 条：「我的」内有三分段（追剧/最近/下载），且共用一个列表骨架',
      (tester) async {
    await mount(tester, const Size(390, 844));
    // 进入「我的」（底栏 index 1）。
    await tester.tap(find.byKey(const ValueKey('bottom-nav-1')));
    await tester.pumpAndSettle();
    expect(find.byType(HnSegmentedTabs), findsOneWidget);
    final seg = tester.widget<HnSegmentedTabs>(find.byType(HnSegmentedTabs));
    expect(seg.labels, ['追剧', '最近', '下载']);
    expect(seg.selectedIndex, 0);
    // 切到「最近」分段。
    await tester.tap(find.byKey(const ValueKey('my-seg-1')));
    await tester.pumpAndSettle();
    expect(
      tester.widget<HnSegmentedTabs>(find.byType(HnSegmentedTabs)).selectedIndex,
      1,
    );
  });

  testWidgets('发现页不套分段壳（分段只属「我的」）', (tester) async {
    await mount(tester, const Size(390, 844));
    expect(find.byType(HnSegmentedTabs), findsNothing);
  });

  testWidgets('设置是第 3 个目的地，且进入后底栏仍可见', (tester) async {
    await mount(tester, const Size(390, 844));
    await tester.tap(find.byKey(const ValueKey('bottom-nav-2')));
    await tester.pumpAndSettle();
    expect(find.text('设置与备份'), findsOneWidget);
  });

  testWidgets('§2.1：手机横屏用 narrow 变体（72），不是平板的 88', (tester) async {
    await mount(tester, const Size(844, 390));
    final rail = tester.widget<HnNavigationRail>(
      find.byType(HnNavigationRail),
    );
    expect(rail.variant, HnRailVariant.narrow);
  });

  testWidgets('边界：TV 侧边栏仍是 tv-nav-N（本次不动 TV）', (tester) async {
    tester.view.physicalSize = const Size(960, 540);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    await tester.pumpWidget(
      DuanjuApp(repository: FixtureRepository(), store: store, television: true),
    );
    await tester.pumpAndSettle();
    // TV 走自己的 164px 侧边栏，**不出现** HnNavigationRail。
    expect(find.byKey(const ValueKey('tv-nav-0')), findsOneWidget);
    expect(find.byKey(const ValueKey('tv-nav-1')), findsOneWidget);
    expect(find.byType(HnNavigationRail), findsNothing);
  });
}
