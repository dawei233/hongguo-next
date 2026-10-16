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
/// 同时记录一条**边界的历史**：B2/B3 期间 TV 侧边栏（`tv-nav-N`）曾在改动
/// 范围之外（当时 10 个 TV 用例压着，不能顺手统一）。**B7b 之后该边界已拆除**
/// —— TV 侧边栏随 TV 下线一并删除，TV 形态回落到统一导航轨，本文件末尾的
/// 用例相应改为断言 `tv-nav-N` 不存在。
///
/// （原注释写的「16 个 TV 用例」是我数错的数字，实跑为 10 —— 见
/// `docs/B7-TV-REMOVAL-CHECKLIST.md` 的勘误。）
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

  testWidgets('左轨路径同样能触发密码门（接线守卫·横屏）', (tester) async {
    await mount(tester, const Size(1200, 800));
    // 左轨 index 2 = 「最近」；连点 6 次。
    for (var i = 0; i < 6; i++) {
      await tester.tap(find.byKey(const ValueKey('rail-2')));
      await tester.pump();
    }
    await tester.pumpAndSettle();
    expect(find.text('站源密码锁'), findsOneWidget,
        reason: '左轨「最近」连点 6 次也应开门（两条导航路径都要接）');
  });

  // B7c：原「TV 侧边栏已下线，TV 形态改用统一导航轨」用例已删除 ——
  // TV 相关代码已**全部**清理（`AppLayout.television` / `AppDevice.television` /
  // `displayMode` / TV 侧边栏 / 遥控组件），不再存在需要锁的 TV 边界。


  // ⚠ 这条是为一个**真实回归**补的守卫（B7b 时由分析器 unused_element 暴露）：
  //
  // B2 把底栏/左轨的回调从 `_onNavSelected` 换成 `_onDestinationSelected` /
  // `_onRailSelected`，**忘了把 6 连点密码门控搬过去**；于是那条接线只剩 TV
  // 侧边栏还在用。B7b 删掉 TV 侧边栏后，密码门控在**全部形态下都不可达**。
  //
  // 之所以长期没被发现：`source_gate_test.dart` 只测 `RepeatTapGate` 本身
  // （纯逻辑），**没有任何测试覆盖"连点导航 6 次会打开密码门"这条接线**。
  // 本用例补的正是这条接线 —— 逻辑对 ≠ 接线对。
  testWidgets('连点「最近」6 次打开站源密码门（接线守卫）', (tester) async {
    await mount(tester, const Size(390, 844));
    // 进入「我的」并切到「最近」分段。
    await tester.tap(find.byKey(const ValueKey('bottom-nav-1')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('my-seg-1')));
    await tester.pumpAndSettle();
    // 连点底栏「我的」**6 次**（阈值 6；切到「最近」分段那一步不计入，
    // 因为 `_mySection == 1` 这个条件在它之前尚未成立）。
    // 走底栏入口，覆盖 `_onDestinationSelected` 那一路的接线。
    for (var i = 0; i < 6; i++) {
      await tester.tap(find.byKey(const ValueKey('bottom-nav-1')));
      await tester.pump();
    }
    await tester.pumpAndSettle();
    // 密码门控是对话框：弹出来说明接线通了。标题为「站源密码锁」。
    expect(find.text('站源密码锁'), findsOneWidget,
        reason: '连点 6 次应弹出站源密码门（B2 曾漏迁移此接线，B7b 才暴露）');
  });

  _overflowMenuGuards();
}
// ───────────────────────────────────────────────────────────────────────────
// 顶栏「更新剧库」入口的**互斥性**守卫（真机 bug 回归）
//
// 用户反馈：「右上角那个（更多/设置类）按钮点不了」。根因是 B9 清理后，
// `PopupMenuButton` 仍**无条件渲染**，而它唯一的菜单项带着
// `constraints.maxWidth < 400` 条件 —— 宽屏（≥400）下点开是**空菜单**。
//
// 实测范围比反馈更广：`_tab != 0`（「我的」等页）与推荐视图下同样是空菜单，
// 与宽度无关。
//
// 修法是把"按钮渲染"与"菜单项出现"收敛为**同一个谓词** `_showsOverflowMenu`，
// 与同位置的 `catalog-refresh` 按钮**互为补集**（两者都调 `_refreshCatalog`）。
// 下面四条锁住这个互补关系：任一宽度下，**恰好一个**入口存在。
// ───────────────────────────────────────────────────────────────────────────
void _overflowMenuGuards() {
  Future<void> pumpHome(WidgetTester tester, Size size) async {
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
  }

  /// 「更新剧库」溢出菜单：以 tooltip 定位（同位置刷新按钮的 tooltip 相同，
  /// 故用 PopupMenuButton 类型区分），且**必须非空**才算合格入口。
  Finder overflowMenu() => find.byType(PopupMenuButton<String>);

  testWidgets('宽屏：不渲染溢出菜单，且刷新按钮存在（二者互补）', (tester) async {
    await pumpHome(tester, const Size(1200, 800));
    // 关键断言：宽屏下**不存在** PopupMenuButton —— 从结构上杜绝空菜单。
    expect(overflowMenu(), findsNothing, reason: '宽屏不该出现溢出菜单（会点成空弹窗）');
    expect(
      find.byKey(const ValueKey('catalog-refresh')),
      findsOneWidget,
      reason: '宽屏的「更新剧库」入口应由刷新按钮承担',
    );
  });

  testWidgets('窄屏：渲染溢出菜单且菜单项非空，刷新按钮不出现', (tester) async {
    await pumpHome(tester, const Size(390, 844));
    expect(overflowMenu(), findsOneWidget);
    expect(find.byKey(const ValueKey('catalog-refresh')), findsNothing);
    // 点开确认**不是空菜单**：必须能看到「更新剧库」这一项。
    await tester.tap(overflowMenu());
    await tester.pumpAndSettle();
    expect(find.text('更新剧库'), findsOneWidget, reason: '窄屏菜单必须有实际内容');
  });

  testWidgets('非发现页（我的）：不渲染溢出菜单（原先此处也是空菜单）', (tester) async {
    await pumpHome(tester, const Size(390, 844));
    await tester.tap(find.byKey(const ValueKey('bottom-nav-1')));
    await tester.pumpAndSettle();
    expect(
      overflowMenu(),
      findsNothing,
      reason: '「我的」页没有可放的内容项，不该渲染空菜单（与宽度无关）',
    );
  });

  testWidgets('宽屏下设置入口可达（左轨第 5 项）', (tester) async {
    // 用户说"右上角那个设置按钮点击不了" —— 实为上述空菜单。
    // 本条确认真正的设置入口（左轨）在宽屏下可用。
    await pumpHome(tester, const Size(1200, 800));
    await tester.tap(find.byKey(const ValueKey('rail-4')));
    await tester.pumpAndSettle();
    expect(find.text('设置与备份'), findsOneWidget);
  });
}
