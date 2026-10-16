import 'package:hongguo_next/local_store.dart';
import 'package:hongguo_next/main.dart';
import 'package:hongguo_next/models.dart';
import 'package:hongguo_next/core_bridge.dart';
import 'package:hongguo_next/detail_screen.dart';
import 'package:hongguo_next/remote_widgets.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:media_kit/media_kit.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'fixtures.dart';
import 'player_fixtures.dart';

void main() {
  // DuanjuApp 会一路构造到 PlayerScreen；测试环境必须注入假播放器，
  // 否则会创建 media_kit 原生播放器（需要 libmpv，测试环境没有）。
  DuanjuApp app(
    AppRepository repository, {
    LocalStore? store,
    bool television = false,
  }) => DuanjuApp(
    repository: repository,
    store: store,
    television: television,
    playerFactory: () => Player(platformPlayer: ScriptedPlayer()),
    videoBuilder: (controls) => controls,
  );

  Future<LocalStore> store() async {
    SharedPreferences.setMockInitialValues({});
    // 注入测试用 pin 哈希（testStore）：fake async 下默认 isolate 哈希永不完成，
    // 启用密码锁的用例会挂到超时（见 fixtures.dart 的说明）。
    return testStore(await SharedPreferences.getInstance());
  }

  for (final size in [const Size(390, 844), const Size(1280, 800)]) {
    testWidgets('catalog, VIP filter and fresh details at $size', (
      tester,
    ) async {
      tester.view.physicalSize = size;
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final repository = FixtureRepository();
      final local = await store();
      // 新语义（2026-10-16）：黄豆是 restricted 源，密码锁默认激活、默认隐藏；
      // 管理员先启用密码锁（启用即解锁），站源切换里才会出现黄豆。
      await local.enableSourceGate('666666');
      await tester.pumpWidget(app(repository, store: local));
      await tester.pumpAndSettle();
      expect(find.byTooltip('VIP：隐藏'), findsNothing);
      expect(find.text('测试短剧'), findsOneWidget);
      // 编译期分版已废弃（allSourcesEnabled 恒为 true），原「单源版」分支删除；
      // 黄豆需先解锁密码锁才能切换，上面的 enableSourceGate 已覆盖。
      await tester.tap(find.byKey(const ValueKey('source-switch')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('黄豆'));
      await tester.pumpAndSettle();
      expect(find.text('会员测试剧'), findsNothing);
      expect(find.byTooltip('VIP：隐藏'), findsOneWidget);
      await tester.tap(find.byTooltip('VIP：隐藏'));
      await tester.pumpAndSettle();
      expect(find.byTooltip('VIP：显示'), findsOneWidget);
      expect(find.text('会员测试剧'), findsOneWidget);
      await tester.tap(find.byTooltip('VIP：显示'));
      await tester.pumpAndSettle();
      expect(find.text('会员测试剧'), findsNothing);
      await tester.tap(find.text('测试短剧'));
      await tester.pumpAndSettle();
      // README:33 记录的行为是「普通点剧直接进入播放」，不再先开详情页。
      expect(repository.detailCalls, 1);
      expect(find.byTooltip('加入追剧'), findsNothing);
      // 详情页保留给下载与兜底入口，这里直接挂载它来验证追剧与选集。
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: DetailScreen(
            drama: FixtureRepository.free,
            repository: repository,
            store: local,
            playerFactory: () => Player(platformPlayer: ScriptedPlayer()),
            videoBuilder: (controls) => controls,
          ),
        ),
      );
      await tester.pumpAndSettle();
      // README:33：选集默认折叠，展开后才渲染分集按钮。
      await tester.tap(find.textContaining('选集 ·'));
      await tester.pumpAndSettle();
      expect(find.byKey(const ValueKey('episode-2')), findsOneWidget);
      // 追剧入口是下拉菜单（key: follow-status），选中某个状态即加入追剧。
      await tester.tap(find.byKey(const ValueKey('follow-status')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('在看').last);
      await tester.pumpAndSettle();
      expect(local.isFavorite(FixtureRepository.free.id), isTrue);
      for (final episode in [1, 2]) {
        await local.saveWatch(
          WatchEntry(
            drama: FixtureRepository.free,
            episode: episode,
            position: 18.5,
            duration: 90,
            updatedAt: DateTime.now(),
          ),
        );
        await tester.pumpAndSettle();
        // 详情页底部按钮现在直接显示续播到第几集（原为「继续第 N 集」）。
        expect(find.text('继续播放 · 第 $episode 集'), findsOneWidget);
        for (final number in [1, 2]) {
          // 分集按钮已改为 RemoteEpisodeButton，选中态由 current 表达。
          final button = tester.widget<RemoteEpisodeButton>(
            find.byKey(ValueKey('episode-$number')),
          );
          expect(button.current, number == episode);
        }
      }
      await local.clearHistory();
      await tester.pumpAndSettle();
      expect(find.text('立即播放'), findsOneWidget);
      expect(tester.takeException(), isNull);
    });
  }

  testWidgets('failed catalog can be retried', (tester) async {
    final repository = FixtureRepository()..fail = true;
    await tester.pumpWidget(
      app(repository, store: await store()),
    );
    await tester.pumpAndSettle();
    expect(find.text('暂时无法加载'), findsOneWidget);
    repository.fail = false;
    await tester.tap(find.text('重试'));
    await tester.pumpAndSettle();
    expect(find.text('测试短剧'), findsOneWidget);
  });

  testWidgets('fresh disk catalog skips the network and resumes pagination', (
    tester,
  ) async {
    final repository = FixtureRepository();
    repository.cachedPages['hongguo'] = CatalogPage(
      [
        FixtureRepository.free,
        for (var index = 0; index < 30; index++)
          Drama(
            id: 'hongguo:cached-$index',
            source: 'hongguo',
            title: '缓存短剧$index',
            episodes: 1,
            category: '合成数据',
          ),
      ],
      fresh: true,
      page: 3,
      hasMore: true,
    );
    await tester.pumpWidget(
      app(repository, store: await store()),
    );
    await tester.pumpAndSettle();
    expect(find.text('测试短剧'), findsOneWidget);
    expect(repository.requests, isEmpty);
    final scrollable = find.byType(CustomScrollView);
    for (var i = 0; i < 4 && repository.pages.isEmpty; i++) {
      await tester.drag(scrollable, const Offset(0, -900));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 50));
    }
    await tester.pumpAndSettle();
    expect(repository.pages, [4]);
    await tester.tap(find.byKey(const ValueKey('catalog-refresh')));
    await tester.pumpAndSettle();
    expect(repository.pages, [4, 1]);
    expect(repository.forced, [false, true]);
    expect(tester.takeException(), isNull);
  });

  testWidgets('stale disk catalog remains usable when refresh fails', (
    tester,
  ) async {
    final repository = FixtureRepository()..fail = true;
    repository.cachedPages['hongguo'] = CatalogPage([FixtureRepository.free]);
    await tester.pumpWidget(
      app(repository, store: await store()),
    );
    await tester.pumpAndSettle();
    expect(find.text('测试短剧'), findsOneWidget);
    expect(find.text('合成网络错误'), findsNothing);
    expect(find.text('暂时无法加载'), findsNothing);
    expect(repository.requests, ['hongguo']);
    expect(tester.takeException(), isNull);
  });

  test('watch progress and favorites survive a new store', () async {
    final local = await store();
    await local.saveWatch(
      WatchEntry(
        drama: FixtureRepository.free,
        episode: 2,
        position: 18.5,
        duration: 90,
        updatedAt: DateTime.now(),
      ),
    );
    await local.toggleFavorite(FixtureRepository.free);
    final restored = LocalStore(local.preferences);
    expect(restored.watched(FixtureRepository.free.id)?.position, 18.5);
    expect(restored.watched(FixtureRepository.free.id)?.episode, 2);
    expect(restored.isFavorite(FixtureRepository.free.id), isTrue);
  });
}
