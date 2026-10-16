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
/// ## 本测试的定位：A（§6/B4 状态机单点化）的**守卫**
///
/// 实测（2026-10-16）：本测试**当前通过** —— 手机两朝向都解析到同一个
/// `PlayerControls`，State 延续。状态机重复（`PlayerControls` 4s vs
/// `TelevisionControls` 5s）确实存在，但只在 `_television` 翻转时才可能换实现，
/// 而 `_television` 是**设备级硬件标志**（Android `deviceInfo`，不随朝向变化），
/// 实际触发面 ≈ 0。
///
/// 因此 A 被判定为**结构性收敛**（消掉两份 `_visible`/`_hideTimer` 重复、
/// 把 5s/4s 差异显式参数化），**不是修正在发生的 bug**，优先级排在 B2/B3 之后。
/// **本测试保留**：A 被推迟期间由它盯着"形态切换不得重建状态机"这条约束，
/// 一旦有人为某形态新写一套控制层，这里会立刻红。
///
/// A 的完整收敛见 `docs/ANDROID-UI-REDESIGN.md` §6/B4。
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
