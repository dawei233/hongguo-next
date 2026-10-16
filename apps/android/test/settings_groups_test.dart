import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hongguo_next/settings_screen.dart';
import 'package:hongguo_next/ui/components/hn_settings_group.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'fixtures.dart';

/// `ANDROID-UI-REDESIGN.md` §4.3 的回归测试：设置页从 **9 条平铺 ListTile**
/// 改为 **5 个场景分组**，且平板横屏用两栏（左分组导航 240 + 右内容）。
///
/// ## 关于「4 个还是 5 个分组」
///
/// 规范 §0/§3.2 写「4 个场景分组」，但 §4.3.1 的归属表与 §4.3.2 的线框图
/// 都列出 **5 个**（通用 / 播放与下载 / 内容与站源 / 数据与同步 / 关于）——
/// §0 那句漏了「通用」。实现以**明细章节**为准，本测试锁住 5 个。
void main() {
  Future<void> pump(WidgetTester tester, Size size) async {
    tester.view.physicalSize = size;
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    // 用真实主题（AppTheme）以便令牌可用 —— 但 SettingsScreen 需要 repository。
    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (context) => FutureBuilder(
            future: Future<void>.value(),
            builder: (context, _) => SettingsScreen(
              repository: FixtureRepository(),
              store: store,
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
  }

  testWidgets('手机竖屏：分组卡（§4.3.3），顺序为 通用→…→关于', (tester) async {
    await pump(tester, const Size(390, 844));
    // ListView 是**懒构建**的：390×844 一屏放不下 5 组，故同一时刻只会有
    // 前几组在树上（实测 4 组）。所以不能断言"一次找到 5 个"，而要：
    // ① 已渲染的必须是期望顺序的前缀；② 逐组下滚，直到 5 组都见过。
    const expected = ['通用', '播放与下载', '内容与站源', '数据与同步', '关于'];
    final seen = <String>[];

    List<String> visibleTitles() => tester
        .widgetList<HnSettingsGroup>(find.byType(HnSettingsGroup))
        .map((g) => g.title)
        .toList();

    // ① 首屏渲染的应是期望顺序的前缀。
    final first = visibleTitles();
    expect(first, isNotEmpty);
    expect(expected.take(first.length).toList(), first);

    // ② 逐组下滚，收集全部 5 个标题（顺序应与 expected 一致）。
    for (var i = 0; i < 8 && seen.length < expected.length; i++) {
      for (final title in visibleTitles()) {
        if (!seen.contains(title)) seen.add(title);
      }
      await tester.drag(find.byType(ListView).first, const Offset(0, -300));
      await tester.pumpAndSettle();
    }
    expect(seen, expected, reason: '5 个分组应齐全且顺序正确');

    // 竖屏用 card 变体（分组卡），不是两栏的 plain。
    expect(
      tester
          .widgetList<HnSettingsGroup>(find.byType(HnSettingsGroup))
          .every((g) => g.variant == HnSettingsVariant.card),
      isTrue,
    );
  });

  testWidgets('平板横屏：两栏 + 左分组导航 5 项（§4.3.2）', (tester) async {
    await pump(tester, const Size(1200, 800));
    // 左导航 5 项。
    for (var i = 0; i < 5; i++) {
      expect(
        find.byKey(ValueKey('settings-group-$i')),
        findsOneWidget,
        reason: '左分组导航应有第 $i 项',
      );
    }
    // 右内容一次只显示**当前**分组。
    final groups = tester
        .widgetList<HnSettingsGroup>(find.byType(HnSettingsGroup))
        .toList();
    expect(groups.length, 1, reason: '两栏下右内容只渲染当前分组');
    expect(groups.single.title, '通用');
    expect(groups.single.variant, HnSettingsVariant.plain);
  });

  testWidgets('平板横屏：点左导航切换右内容', (tester) async {
    await pump(tester, const Size(1200, 800));
    await tester.tap(find.byKey(const ValueKey('settings-group-3')));
    await tester.pumpAndSettle();
    final group = tester.widget<HnSettingsGroup>(
      find.byType(HnSettingsGroup),
    );
    expect(group.title, '数据与同步');
  });

  testWidgets('「播放调试日记」已降级但仍可达（降级 ≠ 删除）', (tester) async {
    // 手机竖屏下滚到「关于」组找到它 —— 这条守的是"只删不搬"的教训：
    // 降级到「关于 → 诊断」不等于把它弄丢。
    await pump(tester, const Size(390, 844));
    final diary = find.byKey(const ValueKey('diary-setting'));
    await tester.scrollUntilVisible(diary, 300);
    expect(diary, findsOneWidget);
    expect(find.text('播放调试日记'), findsOneWidget);
  });

  testWidgets('平板横屏下「关于」组含网络与资源 + 调试日记', (tester) async {
    await pump(tester, const Size(1200, 800));
    // 切到「关于」（第 5 项）。
    await tester.tap(find.byKey(const ValueKey('settings-group-4')));
    await tester.pumpAndSettle();
    expect(find.text('关于'), findsWidgets);
    expect(find.byKey(const ValueKey('diary-setting')), findsOneWidget);
  });

  testWidgets('外观主题在「通用」组（§4.3.1：提到最上，最常改）', (tester) async {
    await pump(tester, const Size(1200, 800));
    // 两栏默认停在第一个分组 = 通用。
    expect(find.byKey(const ValueKey('theme-setting')), findsOneWidget);
  });
}
