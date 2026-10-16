import 'package:hongguo_next/app_layout.dart';
import 'package:hongguo_next/models.dart';
import 'package:hongguo_next/remote_widgets.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

import 'fixtures.dart';

class TelevisionRepository extends FixtureRepository {
  @override
  Future<DramaDetail> detail(Drama drama) async {
    detailCalls++;
    return DramaDetail(
      drama,
      List.generate(
        100,
        (index) => Episode({
          'id': '${index + 1}',
          'currentEpisode': index + 1,
          'vip': true,
        }, index + 1),
      ),
    );
  }
}

void main() {
  // B7b：`makeStore` 随其唯一使用者（TV 遥控用例）一并删除。

  void size(WidgetTester tester, Size size) {
    tester.view.physicalSize = size;
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
  }

  Future<void> press(WidgetTester tester, LogicalKeyboardKey key) async {
    if (key == LogicalKeyboardKey.goBack) {
      await tester.binding.handlePopRoute();
    } else {
      await tester.sendKeyEvent(key);
    }
    await tester.pumpAndSettle();
  }

  testWidgets(
    'Android TV detection falls back safely when the platform channel is unavailable',
    (tester) async {
      debugDefaultTargetPlatformOverride = TargetPlatform.android;
      addTearDown(() => debugDefaultTargetPlatformOverride = null);
      final messenger =
          TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
      addTearDown(
        () => messenger.setMockMethodCallHandler(AppDevice.channel, null),
      );
      messenger.setMockMethodCallHandler(AppDevice.channel, (call) async {
        expect(call.method, 'deviceInfo');
        return {'television': true, 'version': appVersion};
      });
      final detected = await AppDevice.detect();
      expect(detected.television, isTrue);
      expect(detected.version, appVersion);
      messenger.setMockMethodCallHandler(
        AppDevice.channel,
        (call) async => throw MissingPluginException(),
      );
      expect((await AppDevice.detect()).television, isFalse);
      debugDefaultTargetPlatformOverride = null;
    },
  );

  // B7b：原「manual interface preference overrides device detection and survives
  // restart」已删除 —— 它断言 `tv-nav-N` 随界面模式出现/消失。TV 侧边栏已随本批
  // 删除；`displayMode` 的整删归 B9（届时连 `LocalStore` 字段与 snapshot 白名单
  // 一并处理，避免同一片代码改两次）。

  // B7b：原「TV sources, details, VIP confirmation and back retain remote focus」
  // （2 个尺寸 = 2 例）已删除 —— 全程 `sendKeyEvent` + `focusRemote`，保护的是
  // **遥控可达性**，按判据（遥控输入删 / 宽屏布局改写）删除。
  // 其中 VIP 确认、返回等业务行为由非 TV 用例覆盖，能力未丢失。

  testWidgets(
    'remote grid reaches unbuilt rows, partial last row and pagination without touch',
    (tester) async {
      size(tester, const Size(960, 540));
      var loaded = 0;
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: RemoteGrid(
              itemKeys: List.generate(59, (index) => '$index'),
              columns: 4,
              itemExtent: 100,
              autofocus: true,
              footer: Center(
                child: RemoteButton(label: '加载更多', onPressed: () => loaded++),
              ),
              itemBuilder: (_, index, node, onFocus) => RemoteEpisodeButton(
                number: index,
                focusNode: node,
                onFocus: onFocus,
                onPressed: () {},
              ),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(FocusManager.instance.primaryFocus?.debugLabel, 'remote-0');
      for (var index = 0; index < 3; index++) {
        await press(tester, LogicalKeyboardKey.arrowRight);
      }
      for (var row = 0; row < 14; row++) {
        await press(tester, LogicalKeyboardKey.arrowDown);
      }
      expect(FocusManager.instance.primaryFocus?.debugLabel, 'remote-58');
      // 末行首次按下方向下键只把列表滚到底（RemoteGrid 返回 handled 且不移
      // 焦点），第二次才交给焦点遍历进入 footer 的「加载更多」。
      await press(tester, LogicalKeyboardKey.arrowDown);
      await press(tester, LogicalKeyboardKey.arrowDown);
      await press(tester, LogicalKeyboardKey.select);
      expect(loaded, 1);
      expect(tester.takeException(), isNull);
    },
  );

  // B7b：原「episode grid initially focuses an offscreen saved episode」已删除 ——
  // 它断言 `primaryFocus.debugLabel` 这类**焦点标签**，保护的是遥控初始聚焦。
  // 其中"初始定位到已看集"的业务行为由 B5 的非 TV 路径承担
  // （`_continuousGrid` 的 jumpTo + `watched` 高亮，`player_sidebar_test` 覆盖）。

}
