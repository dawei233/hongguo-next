import 'dart:io';

import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hongguo_next/main.dart';
import 'package:hongguo_next/models.dart';
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
/// 预览用夹具：**必须实现 `recommendations`**。
///
/// 首屏的「正在热播」横轨与「今日首选」大卡的数据都来自推荐位；
/// 而 `FixtureRepository` 未实现该接口（会抛错），首屏会**按设计静默降级**
/// —— 那样预览图里就永远看不到 B2 新加的两块，看不到用户最关心的部分。
class PreviewRepository extends FixtureRepository {
  @override
  Future<CatalogPage> recommendations(
    String genre, {
    bool more = false,
    bool force = false,
  }) async => CatalogPage([
    Drama(
      id: 'hongguo:p1',
      source: 'hongguo',
      title: '逆光而行',
      category: '都市',
      episodes: 32,
      description: '清末民初，一个关于复仇与救赎的故事。他在暗处蛰伏十年，只为等一个时机。',
    ),
    Drama(
      id: 'hongguo:p2',
      source: 'hongguo',
      title: '茅山打更人',
      category: '悬疑',
      episodes: 24,
      description: '深夜的更声里藏着秘密。',
    ),
    Drama(
      id: 'hongguo:p3',
      source: 'hongguo',
      title: '雪落无声',
      category: '爱情',
      episodes: 18,
    ),
  ]);
}

void main() {
  // 测试环境的默认字体没有中文字形，会渲染成方框。
  // 加载系统里的文泉驿微米黑，让预览图上的中文可读。
  setUpAll(() async {
    TestWidgetsFlutterBinding.ensureInitialized();
    // ⚠ 测试环境的字体装配是个坑，结论先写在前面：
    //   - 默认族没有中文字形 → 中文渲染成方框
    //   - 文泉驿**没有拉丁与图标字形** → 若只用它顶替默认族，"Next"、图标变方框
    //   - 同一族**多次 `FontLoader(family).load()` 是替换而非追加**
    //     → 先注册中文再单独注册 Roboto，会把中文挤掉（我踩过）
    //
    // 正解：**每个族只建一个 loader**，把该族要用到的字体按"优先在前"的顺序
    // 全部 addFont 进去，形成族内 fallback 链：
    //   Roboto/sans-serif = [Roboto-Regular（拉丁）, 文泉驿（中文）]
    //   MaterialIcons     = [MaterialIcons-Regular（图标）]
    const cjkCandidates = [
      '/usr/share/fonts/truetype/wqy/wqy-microhei.ttc',
      '/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc',
    ];
    final cjkPath = cjkCandidates.firstWhere(
      (p) => File(p).existsSync(),
      orElse: () => '',
    );
    const fontsDir =
        '/workspace/红果/.toolchain/flutter/bin/cache/artifacts/material_fonts';
    final robotoPath = ['$fontsDir/Roboto-Regular.ttf']
        .firstWhere((p) => File(p).existsSync(), orElse: () => '');
    final iconPath = '$fontsDir/MaterialIcons-Regular.otf';

    Future<void> registerFamily(String family, List<String> paths) async {
      final existing = paths.where((p) => File(p).existsSync()).toList();
      if (existing.isEmpty) return;
      final loader = FontLoader(family);
      for (final p in existing) {
        final bytes = File(p).readAsBytesSync();
        loader.addFont(Future.value(ByteData.view(bytes.buffer)));
      }
      await loader.load();
    }

    // 默认族：**中文在前**。实测：族内多字体时，第一个字体若不含该字形，
    // 才会轮到后续字体；把只含拉丁的 Roboto 放前面会让中文全部落到方框
    // （我按"拉丁在前"试过一版，中文全变方框）。文泉驿在前、Roboto 兜底，
    // 中文与拉丁就都能出。
    for (final family in ['Roboto', 'sans-serif']) {
      await registerFamily(family, [cjkPath, robotoPath]);
    }
    // 图标族单独注册（图标码位只在 MaterialIcons 里）。
    await registerFamily('MaterialIcons', [iconPath]);
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
      DuanjuApp(repository: PreviewRepository(), store: store),
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
