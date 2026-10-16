import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hongguo_next/episode_browser.dart';
import 'package:hongguo_next/local_store.dart';
import 'package:hongguo_next/player_screen.dart';
import 'package:hongguo_next/remote_widgets.dart';
import 'package:hongguo_next/ui/layout.dart';
import 'package:media_kit/media_kit.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'fixtures.dart';
import 'player_fixtures.dart';

/// B5 回归测试：**平板横屏下选集侧栏常驻**（`ANDROID-UI-REDESIGN.md` §4.2.2）。
///
/// 改造前的缺陷：播放器 `build` 里 `if (fullscreen) return _videoPane(context)`
/// —— 全屏**没有任何侧栏**。而平板横屏观看恰是全屏（`_showFullscreen` 在横屏且
/// 宽高比 ≥1 时为真），所以主力形态下永远看不到常驻选集，只能按「选集」弹 Sheet
/// —— 正是规范要消除的两次点击（点选集 → 选集数 → 关闭）。
///
/// 本文件锁住三件事：
/// 1. 平板横屏出现 B5 侧栏（宽 [HnBreak.episodeSidebarWidth]，体内固定
///    [HnBreak.episodeSidebarColumns] 列）
/// 2. 手机竖屏不出现
/// 3. 手机横屏（宽 <900，未达 §2 平板门槛）**不出现 B5 侧栏**
///    —— 这条防的是「840 阈值与 §2 的 900 混用」：非全屏宽布局另有 312/210
///    两档既有侧栏，若把它们的宽度也改成 300，手机上就会冒出平板侧栏。
void main() {
  Future<void> settle(WidgetTester tester) async {
    for (var i = 0; i < 12; i++) {
      await tester.pump(const Duration(milliseconds: 10));
    }
  }

  Future<void> mount(
    WidgetTester tester,
    RouteRepository repository,
    ScriptedPlayer platform, {
    required Size size,
    bool android = true,
    Size? videoSize,
  }) async {
    // 平板横屏播放走的是「宽高比 ≥1 时自动全屏」这条路径，而 `_mobile` 与
    // `_aspectRatio` 两者共同决定 `_showFullscreen`：
    //   - `_mobile` 要求 defaultTargetPlatform 是 android/iOS
    //     （测试环境默认 linux，故须显式 override）
    //   - `_aspectRatio` 初始 9/16（竖屏），只在播放器上报视频尺寸后变 ≥1
    //     （须用 ScriptedPlayer.videoSize 模拟一个横屏片源）
    // 两者缺一，全屏分支就不会触发，B5 的常驻侧栏也就测不到。
    // 注意：不能用 addTearDown 复位 —— flutter_test 的
    // `debugAssertAllFoundationVarsUnset` 在测试体结束、tearDown 之前就会断言，
    // 届时 override 仍为非 null 会报 "foundation debug variable was changed by
    // the test"。故必须在用例体内显式复位（unmount 里做）。
    if (android) debugDefaultTargetPlatformOverride = TargetPlatform.android;
    SharedPreferences.setMockInitialValues({});
    tester.view.physicalSize = size;
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final store = LocalStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    final detail = await repository.detail(FixtureRepository.free);
    await tester.pumpWidget(
      MaterialApp(
        theme: ThemeData.dark(),
        home: PlayerScreen(
          detail: detail,
          initialIndex: 0,
          initialPosition: 7,
          repository: repository,
          store: store,
          playerFactory: () => Player(platformPlayer: platform),
          videoBuilder: (controls) => controls,
        ),
      ),
    );
    await settle(tester);
    if (videoSize != null) {
      platform.videoSize(videoSize.width.toInt(), videoSize.height.toInt());
      await settle(tester);
    }
  }

  Future<void> unmount(WidgetTester tester, ScriptedPlayer player) async {
    await tester.pumpWidget(const SizedBox.shrink());
    await settle(tester);
    expect(player.disposed, isTrue);
    expect(tester.takeException(), isNull);
    // 复位 platform override（见 mount 里的说明：必须在测试体内复位）。
    debugDefaultTargetPlatformOverride = null;
  }

  /// B5 侧栏的判据：宽 == `HnBreak.episodeSidebarWidth` 的 SizedBox，
  /// 且其内是 compact 选集网格且列数 == `HnBreak.episodeSidebarColumns`。
  ///
  /// 不满足两个条件时返回空 —— 这样"非全屏 312/210 既有侧栏"不会被误判为 B5 侧栏。
  Finder b5Sidebar(WidgetTester tester) {
    final boxes = find.byWidgetPredicate(
      (widget) =>
          widget is SizedBox && widget.width == HnBreak.episodeSidebarWidth,
    );
    for (final element in boxes.evaluate()) {
      final box = find.byWidget(element.widget);
      final grids = find.descendant(of: box, matching: find.byType(RemoteGrid));
      if (grids.evaluate().isEmpty) continue;
      if (tester.widget<RemoteGrid>(grids.first).columns ==
          HnBreak.episodeSidebarColumns) {
        return box;
      }
    }
    return find.byWidgetPredicate((_) => false);
  }

  testWidgets('平板横屏：选集侧栏常驻，宽 300，体内固定 4 列', (tester) async {
    final repository = RouteRepository();
    final player = ScriptedPlayer();
    await mount(
      tester,
      repository,
      player,
      // 1200×800 是 §2 形态矩阵里平板横屏的代表尺寸。
      size: const Size(1200, 800),
      // 横屏片源（16:9）→ `_aspectRatio >= 1` → 自动进全屏，
      // 即这条用例覆盖的正是「全屏下的常驻侧栏」。
      videoSize: const Size(1920, 1080),
    );

    final sidebar = b5Sidebar(tester);
    expect(sidebar, findsOneWidget);
    expect(
      find.descendant(of: sidebar, matching: find.byType(EpisodeBrowser)),
      findsOneWidget,
    );

    // 平板横屏宽高比 ≥1 → `_showFullscreen` 为真，即这条断言覆盖的是
    // **全屏状态下的常驻侧栏**（改造前全屏分支直接返回 _videoPane，没有侧栏）。
    expect(
      tester.widget<RemoteGrid>(
        find.descendant(of: sidebar, matching: find.byType(RemoteGrid)).first,
      ).columns,
      HnBreak.episodeSidebarColumns,
    );

    await unmount(tester, player);
  });

  testWidgets('手机竖屏：不出现 B5 侧栏', (tester) async {
    final repository = RouteRepository();
    final player = ScriptedPlayer();
    await mount(tester, repository, player, size: const Size(390, 844));
    expect(b5Sidebar(tester), findsNothing);
    await unmount(tester, player);
  });

  testWidgets('手机横屏（宽 <900，未达 §2 平板门槛）：不出现 B5 侧栏', (tester) async {
    final repository = RouteRepository();
    final player = ScriptedPlayer();
    await mount(tester, repository, player, size: const Size(844, 390));
    expect(b5Sidebar(tester), findsNothing);
    await unmount(tester, player);
  });
}
