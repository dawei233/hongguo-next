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

/// 播放器选集侧栏的回归测试（`ANDROID-UI-REDESIGN.md` §4.2.2）。
///
/// ## 两个方向都要锁（否则下次又会两边错一边）
///
/// - **平板横屏「非全屏」→ 有常驻侧栏**（B5 的收益：换集从两次点击变一次）
/// - **平板横屏「全屏」→ 无侧栏**（沉浸观影，纯画面）
/// - 手机竖屏 / 手机横屏（未达 §2 平板门槛）→ 无常驻侧栏
///
/// ## 历史：这里发生过一次方向性错误，两个方向都错过
///
/// **第一次**（B5 之前）：播放器 build 里是 `if (fullscreen) return
/// _videoPane(context)` —— 全屏没有侧栏。B5 认为这是缺陷（主力形态=平板横屏
/// 观看=全屏，于是永远看不到常驻选集），把侧栏加进了**全屏分支**。
///
/// **第二次**（B5 之后，用户真机反馈「安卓的播放界面咋把播放选集列表也常驻了
/// 全屏也不消失」）：B5 的错误在于**把「横屏」与「全屏」当成同一件事**。
/// 横屏布局可以有侧栏，**全屏不该有** —— 全屏的语义是沉浸观影，常驻 300px
/// 侧栏会挤占画面，成了退不出的干扰。
///
/// **正确行为**：非全屏的平板横屏常驻侧栏（保留 B5 收益）；全屏下**刻意不放**
/// 侧栏，选集走「选集」按钮唤出的面板。**全屏无侧栏是正确行为，不是遗漏**
/// —— 别再把它"优化"回去。
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

  testWidgets('平板横屏·非全屏：选集侧栏常驻，宽 300，体内固定 4 列', (tester) async {
    final repository = RouteRepository();
    final player = ScriptedPlayer();
    await mount(
      tester,
      repository,
      player,
      // 1200×800 是 §2 形态矩阵里平板横屏的代表尺寸。
      size: const Size(1200, 800),
      // **刻意不给 videoSize** —— 竖屏片源 → `_aspectRatio` 保持 9/16 < 1
      // → `_showFullscreen` 为假 → 走「非全屏」路径。
      // 这正是 B5 收益成立的那条路径：常驻侧栏、换集一次点击。
    );

    final sidebar = b5Sidebar(tester);
    expect(sidebar, findsOneWidget, reason: '非全屏平板横屏应有常驻选集侧栏');
    expect(
      find.descendant(of: sidebar, matching: find.byType(EpisodeBrowser)),
      findsOneWidget,
    );
    expect(
      tester.widget<RemoteGrid>(
        find.descendant(of: sidebar, matching: find.byType(RemoteGrid)).first,
      ).columns,
      HnBreak.episodeSidebarColumns,
    );

    await unmount(tester, player);
  });

  testWidgets('平板横屏·全屏：**不**出现常驻侧栏（沉浸观影）', (tester) async {
    final repository = RouteRepository();
    final player = ScriptedPlayer();
    await mount(
      tester,
      repository,
      player,
      size: const Size(1200, 800),
      // 横屏片源（16:9）→ `_aspectRatio >= 1` → `_showFullscreen` 为真
      // → 真的经过 fullscreen 为真的分支（不是直接调私有构造）。
      videoSize: const Size(1920, 1080),
    );

    // 这是用户真机反馈的那个 bug：全屏下侧栏不该常驻。
    expect(
      b5Sidebar(tester),
      findsNothing,
      reason: '全屏=沉浸观影，不该有常驻侧栏（用户反馈"全屏也不消失"）',
    );
    // 画面区仍在。
    expect(find.byType(EpisodeBrowser), findsNothing);

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
