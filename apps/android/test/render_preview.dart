import 'dart:io';

import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hongguo_next/main.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'fixtures.dart';

/// 主会话用来**看实际界面**的渲染脚本（不是回归测试，不参与 CI 门禁）。
///
/// 目的：用户在装 APK 前想先看效果，而 APK 构建要 ~10 分钟。
/// Flutter widget 测试能在无设备环境渲染真实 `DuanjuApp`，
/// 用 golden 机制落成 PNG 就能立刻看到成品。
///
/// 跑法（--update-goldens 才会真正写文件）：
///   flutter test test/render_preview.dart --update-goldens
/// 产物：test/goldens/*.png
void main() {
  // 测试环境的默认字体没有中文字形，会渲染成方框。
  // 加载系统里的文泉驿微米黑，让预览图上的中文可读。
  setUpAll(() async {
    TestWidgetsFlutterBinding.ensureInitialized();
    // App 未声明 fontFamily（用系统默认族），所以要把中文字体同时注册成
    // 测试环境的默认族名（'Roboto' 与 'sans-serif'），否则中文仍是方框。
    const candidates = [
      '/usr/share/fonts/truetype/wqy/wqy-microhei.ttc',
      '/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc',
    ];
    final path = candidates.firstWhere(
      (p) => File(p).existsSync(),
      orElse: () => '',
    );
    if (path.isNotEmpty) {
      final bytes = File(path).readAsBytesSync();
      for (final family in ['Roboto', 'sans-serif', 'WenQuanYi Micro Hei']) {
        final loader = FontLoader(family)
          ..addFont(Future.value(ByteData.view(bytes.buffer)));
        await loader.load();
      }
    }
  });

  Future<void> shoot(WidgetTester tester, String name, Size size) async {
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
    // 多泵几帧让异步 fixture 就位、动画落定。
    for (var i = 0; i < 6; i++) {
      await tester.pump(const Duration(milliseconds: 120));
    }

    await expectLater(
      find.byType(DuanjuApp),
      matchesGoldenFile('goldens/$name.png'),
    );
  }

  testWidgets('render 平板横屏 1200x800', (tester) async {
    await shoot(tester, 'tablet-landscape', const Size(1200, 800));
  });

  testWidgets('render 手机竖屏 390x844', (tester) async {
    await shoot(tester, 'phone-portrait', const Size(390, 844));
  });

  testWidgets('render 手机横屏 844x390', (tester) async {
    await shoot(tester, 'phone-landscape', const Size(844, 390));
  });
}
