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
      // B2（§4.1.1）：VIP 显隐从首屏的常驻 IconButton **移入筛选 Sheet**
      // （`⇅` 入口）。本用例考的是"VIP 过滤生效"，与入口位置无关，
      // 故先打开 Sheet 再点对应项 —— 用户控制本身没有被删除，只是换了位置。
      Future<void> tapVipFilter(String tooltip) async {
        await tester.tap(
          find.byTooltip(
            '排序与筛选 · ${local.catalogView.sort.label}',
          ),
        );
        await tester.pumpAndSettle();
        await tester.tap(find.text(tooltip));
        await tester.pumpAndSettle();
        // Sheet 里选完要按「应用」才回传。
        await tester.tap(find.text('应用'));
        await tester.pumpAndSettle();
      }

      // 目标卡可能在首屏之下（B2 首屏加了区块头，内容整体下移），
      // 故先滚入视野再断言 —— 本用例考的是"VIP 过滤是否生效"，
      // 不是"卡片是否恰好落在首屏"。
      Future<void> revealVipDrama() async {
        for (var i = 0; i < 6; i++) {
          if (find.text('会员测试剧').evaluate().isNotEmpty) return;
          await tester.drag(
            find.byType(CustomScrollView).first,
            const Offset(0, -300),
          );
          await tester.pumpAndSettle();
        }
      }

      // 注意语义：Sheet 里的 VIP 项是**状态单选**（`VIP：隐藏` = "VIP 内容
      // 处于隐藏态"），不是"点它去隐藏"。默认 hideVip=true，所以要让
      // 会员剧出现，应选「VIP：显示」；再选回「VIP：隐藏」它才消失。
      // 这与原首屏按钮的 tooltip 语义（tooltip 命名当前态）一致，
      // 只是从"点当前态翻转"换成"直接选目标态"——单选组更直白。
      await tapVipFilter('VIP：显示');
      await revealVipDrama();
      expect(find.text('会员测试剧'), findsOneWidget);
      await tapVipFilter('VIP：隐藏');
      await revealVipDrama();
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
    // B1（ANDROID-UI-REDESIGN §2.1）：手机竖屏海报从 3 列收敛为**固定 2 列**
    // 后，同样的 6 部剧内容高度增加（每行少放一张 → 多一行），
    // 原先「最多 4 次 -900px 拖动」到不了分页触发点，`repository.pages` 为空。
    // 本测试断言的是「滚到底会请求第 4 页」，与需要几次拖动无关，
    // 故改为按「滚到 maxScrollExtent」为终止条件（内容多高都能到达）。
    final scrollable = find.byType(CustomScrollView);
    for (var i = 0; i < 12 && repository.pages.isEmpty; i++) {
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
