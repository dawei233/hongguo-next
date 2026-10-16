import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hongguo_next/local_store.dart';
import 'package:hongguo_next/player_controls.dart';
import 'package:hongguo_next/player_screen.dart';
import 'package:media_kit/media_kit.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'fixtures.dart';
import 'player_fixtures.dart';

/// **B4/A 的核心回归测试**：形态切换（手机横 ↔ 竖）后控制层的
/// **显隐状态机不被重建**（`ANDROID-UI-REDESIGN.md` §2.2 硬约束）。
///
/// 判据是「同一个 State 实例是否在旋转后延续」，而不是看 opacity ——
/// 后者经实测不可靠：`visible = _visible || !playing || buffering || panelOpen`，
/// 暂停时 `!playing` 会把 opacity 强行拉回 1（点击画面会触发播放/暂停），
/// 于是 opacity 恒为 1，反映不出 `_visible`。那是测试手段的问题，不是 §2.2 的问题。
///
/// - State 延续 → 状态机未被重建（§2.2 满足）
/// - State 被替换 → `initState` 重新赋值 `_visible = widget.showOnPlaybackReady`，
///   用户唤出的控制条被重置（§2.2 违反）
///
/// B4 采用方案 A（状态机抽到 controller / `_PlayerScreenState`）后本测试应通过；
/// 若仍失败，说明状态机还挂在 widget 内（即上级说的「B7 必须提前的信号」）。
void main() {
  Future<void> settle(WidgetTester tester) async {
    for (var i = 0; i < 14; i++) {
      await tester.pump(const Duration(milliseconds: 10));
    }
  }

  Future<void> mount(
    WidgetTester tester,
    RouteRepository repository, {
    required Size size,
  }) async {
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
          playerFactory: () => Player(platformPlayer: ScriptedPlayer()),
          videoBuilder: (controls) => controls,
        ),
      ),
    );
    await settle(tester);
  }

  testWidgets('手机横竖切换后控制层状态机不被重建（State 延续）', (tester) async {
    // `_mobile` 要求 defaultTargetPlatform 是 android/iOS（测试环境默认 linux）。
    // 必须在用例体内复位：addTearDown 会被
    // `debugAssertAllFoundationVarsUnset` 判为 "foundation debug variable was changed"。
    debugDefaultTargetPlatformOverride = TargetPlatform.android;
    final repository = RouteRepository();
    await mount(tester, repository, size: const Size(390, 844));

    final finder = find.byType(PlayerControls);
    expect(finder, findsOneWidget, reason: '手机形态应使用 PlayerControls');
    final before = tester.state(finder);

    // 切到横屏（844 <900 → 仍属 phoneLandscape，`_mobile` 维持为真，
    // 控制层仍应是 PlayerControls）：**形态变了，但控制层不该换实现**。
    tester.view.physicalSize = const Size(844, 390);
    await tester.pump();
    await settle(tester);

    final after = tester.state(find.byType(PlayerControls));
    expect(
      identical(before, after),
      isTrue,
      reason: '§2.2：形态只影响布局，不得重建显隐状态机。'
          'State 被替换意味着 initState 重新赋值 _visible，'
          '用户唤出的控制条会被重置（hongguo-drama PC 端「控制条唤不出」即此类）。',
    );

    await tester.pumpWidget(const SizedBox.shrink());
    await settle(tester);
    debugDefaultTargetPlatformOverride = null;
  });
}
