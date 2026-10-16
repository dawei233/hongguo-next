import 'dart:async';

import 'package:hongguo_next/core_bridge.dart';
import 'package:hongguo_next/local_store.dart';
import 'package:hongguo_next/main.dart';
import 'package:hongguo_next/models.dart';
import 'package:hongguo_next/widgets.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'fixtures.dart';
import 'interface_fixtures.dart';

void main() {
  Future<LocalStore> localStore() async {
    SharedPreferences.setMockInitialValues({});
    // 注入测试用 pin 哈希（testStore）：fake async 下默认 isolate 哈希永不完成，
    // 启用密码锁的用例会挂到超时（见 fixtures.dart 的说明）。
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    return store;
  }

  void viewport(WidgetTester tester, Size size, [double scale = 1]) {
    tester.view.physicalSize = size;
    tester.view.devicePixelRatio = 1;
    tester.platformDispatcher.textScaleFactorTestValue = scale;
    addTearDown(tester.view.reset);
    addTearDown(tester.platformDispatcher.clearTextScaleFactorTestValue);
  }

  testWidgets(
    'refresh rotates during a request and stops after success or failure',
    (tester) async {
      // 刷新按钮只在宽度 >= 400 时渲染（窄屏收起，见 home_screen 里
      // catalog-refresh 的条件）；用 390 会让 finder 落空。
      viewport(tester, const Size(430, 844));
      final repository = InterfaceRepository();
      await tester.pumpWidget(
        DuanjuApp(repository: repository, store: await localStore()),
      );
      await tester.pumpAndSettle();
      final refresh = find.byKey(const ValueKey('catalog-refresh'));
      final rotation = find.descendant(
        of: refresh,
        matching: find.byType(RotationTransition),
      );
      final button = find.descendant(
        of: refresh,
        matching: find.byType(IconButton),
      );
      for (final fails in [false, true]) {
        final pending = Completer<CatalogPage>();
        repository.pendingCatalog = pending;
        final requests = repository.requests.length;
        await tester.tap(refresh);
        await tester.pump();
        await tester.pump(const Duration(milliseconds: 150));
        final firstAngle = tester
            .widget<RotationTransition>(rotation)
            .turns
            .value;
        await tester.pump(const Duration(milliseconds: 250));
        expect(
          tester.widget<RotationTransition>(rotation).turns.value,
          isNot(firstAngle),
        );
        expect(tester.widget<IconButton>(button).onPressed, isNull);
        expect(find.text('短标题'), findsOneWidget);
        await tester.tap(refresh);
        expect(repository.requests.length, requests + 1);
        repository.pendingCatalog = null;
        if (fails) {
          pending.completeError(AppFailure('合成更新失败'));
        } else {
          pending.complete(CatalogPage(repository.dramas('hongguo')));
        }
        await tester.pumpAndSettle();
        expect(tester.widget<RotationTransition>(rotation).turns.value, 0);
        expect(tester.widget<IconButton>(button).onPressed, isNotNull);
        expect(find.text('短标题'), findsOneWidget);
        // 刷新失败时列表仍保留既有内容，错误面板只在列表为空时出现
        // （见 home_screen 里 `_items.isEmpty && _error != null`），
        // 所以这里不应期望错误文案上屏。
        if (fails) expect(find.text('合成更新失败'), findsNothing);
      }
      expect(tester.takeException(), isNull);
    },
  );

  testWidgets('only Huangdou exposes and applies the persistent VIP filter', (
    tester,
  ) async {
    viewport(tester, const Size(390, 844));
    final repository = InterfaceRepository();
    final store = await localStore();
    // 新语义（2026-10-16）：黄豆是 restricted 源，密码锁默认激活、默认隐藏；
    // 管理员先启用密码锁（启用即解锁），遍历站源分组时才能到达黄豆。
    await store.enableSourceGate('666666');
    await tester.pumpWidget(DuanjuApp(repository: repository, store: store));
    await tester.pumpAndSettle();
    Future<void> select(String name) async {
      if (SourceGroup.fromSources(SourceSite.values).length <= 1) return;
      await tester.tap(find.byKey(const ValueKey('source-switch')));
      await tester.pumpAndSettle();
      await tester.tap(find.text(name).last);
      await tester.pumpAndSettle();
    }

    // B1（ANDROID-UI-REDESIGN §2.1）把手机竖屏收敛为**固定 2 列**后，
    // 夹具 3 部剧里的第 3 部（会员合成剧）不再落在首屏 —— 3 列时它与前两部
    // 同排可见，2 列时必须下滚才可见。本测试断言的是「VIP 内容在各站源下的
    // 可见性」，与"一行放几张"无关，故把目标卡滚入视野再断言。
    Future<void> revealVipCard() async {
      // B2 首屏重构在网格上方加了区块头（72），同一张卡的位置整体下移，
      // 原先"一次 -400"的固定拖动不再总能滚到。改为**循环拖到出现为止**
      // （最多 6 次），这样布局再变也不会让这条断言变成"调参游戏"。
      for (var i = 0; i < 6; i++) {
        if (find.text('会员合成剧').evaluate().isNotEmpty) return;
        await tester.drag(
          find.byType(CustomScrollView).first,
          const Offset(0, -300),
        );
        await tester.pumpAndSettle();
      }
    }

    for (final group in SourceGroup.fromSources(SourceSite.values)) {
      await select(group.name);
      if (group.id == 'huangdou') {
        expect(find.text('会员合成剧'), findsNothing);
        await tester.tap(find.byTooltip('VIP：隐藏'));
        await tester.pumpAndSettle();
        // 隐藏 VIP 后该卡重新出现（可能在首屏，也可能需下滚）。
        await revealVipCard();
        expect(find.text('会员合成剧'), findsOneWidget);
      } else {
        expect(find.byTooltip('VIP：隐藏'), findsNothing);
        expect(find.byTooltip('VIP：显示'), findsNothing);
        await revealVipCard();
        expect(find.text('会员合成剧'), findsWidgets);
      }
    }
    // 编译期分版已废弃（allSourcesEnabled 恒为 true），
    // 原「单源版提前 return」分支删除；黄豆需先解锁密码锁才能到达，
    // 上面的 enableSourceGate 已覆盖。
    await select('黄豆');
    expect(find.byTooltip('VIP：显示'), findsOneWidget);
    await tester.tap(find.byTooltip('VIP：显示'));
    await tester.pumpAndSettle();
    expect(store.hideVip, isTrue);
    await select('红果');
    expect(find.text('会员合成剧'), findsOneWidget);
    expect(find.textContaining('VIP：'), findsNothing);
  });

  for (final layout in [
    (const Size(390, 844), 1.0, false),
    (const Size(320, 844), 2.0, false),
    (const Size(1280, 800), 1.0, false),
    (const Size(960, 540), 1.0, true),
  ]) {
    testWidgets('posters align across catalog and saved pages at $layout', (
      tester,
    ) async {
      viewport(tester, layout.$1, layout.$2);
      final repository = InterfaceRepository();
      final store = await localStore();
      for (final drama in repository.dramas('hongguo')) {
        await store.toggleFavorite(drama);
        await store.saveWatch(
          WatchEntry(
            drama: drama,
            episode: 1,
            position: 15,
            duration: 60,
            updatedAt: DateTime.now(),
          ),
        );
      }
      await tester.pumpWidget(
        DuanjuApp(repository: repository, store: store, television: layout.$3),
      );
      await tester.pumpAndSettle();
      Size? catalogSize;
      // B2（IA 重排）：导航路径从「3 个平级 tab」变成两层 ——
      // - 竖屏：底栏 3 目的地（发现 / 我的 / 设置），后两个页面在「我的」的
      //   三分段（`my-seg-N`：追剧 / 最近 / 下载）里
      // - 横屏：左轨 5 项（发现 / 追剧 / 最近 / 下载 / 设置），中间三项直入分段
      // - TV：侧边栏 4 项（`tv-nav-N`）**未改**（TV 不在范围）
      //
      // 本用例要比较的是「同一张海报在 catalog 与两个 saved 页上的尺寸是否一致」，
      // 与走哪条导航路径无关，故把路径改写成显式步骤，断言部分原样保留。
      final tv = layout.$3;
      final narrow = layout.$1.width < 840;
      final steps = <Future<void> Function()>[
        () async {}, // 步骤 0：起步就在「发现」页
        () async {
          // 到「追剧」（saved，非 history）
          if (tv) {
            await tester.tap(find.byKey(const ValueKey('tv-nav-1')));
          } else if (narrow) {
            await tester.tap(find.byKey(const ValueKey('bottom-nav-1')));
            await tester.pumpAndSettle();
            await tester.tap(find.byKey(const ValueKey('my-seg-0')));
          } else {
            await tester.tap(find.byKey(const ValueKey('rail-1')));
          }
          await tester.pumpAndSettle();
        },
        () async {
          // 到「最近观看」（saved，history）
          if (tv) {
            await tester.tap(find.byKey(const ValueKey('tv-nav-2')));
          } else if (narrow) {
            await tester.tap(find.byKey(const ValueKey('my-seg-1')));
          } else {
            await tester.tap(find.byKey(const ValueKey('rail-2')));
          }
          await tester.pumpAndSettle();
        },
      ];
      for (final step in steps) {
        await step();
        final covers = find.byType(DramaCover);
        final tiles = find.byType(DramaTile);
        // 断言「所有渲染出的海报尺寸一致、比例 2:3」。
        //
        // 原断言写死 `findsNWidgets(3)` + 遍历 `covers.at(0..2)` 并要求 three 张
        // 同 bottom —— 那是在断言「一行放得下 3 张」，即把**旧的 3 列竖屏密度**
        // 当成了本测试的意图。B1（ANDROID-UI-REDESIGN §2.1）把手机竖屏收敛为
        // **固定 2 列**，3 张卡必然折成两行，`bottom` 不再相等，于是原断言失败。
        //
        // 本测试真正的意图（见用例名）是「catalog 与 saved 两页的海报对齐」
        // —— 即同一张卡在两个页面里的尺寸一致，与一行放几张无关。故改为：
        // 逐张校验比例，并与 catalog 首张的尺寸对比。这样在 2/3/6 列下都成立。
        expect(covers, findsAtLeastNWidgets(2));
        final first = tester.getRect(covers.first);
        catalogSize ??= first.size;
        expect(first.width, closeTo(catalogSize.width, .01));
        expect(first.height, closeTo(catalogSize.height, .01));
        final visible = covers.evaluate().length;
        for (var index = 0; index < visible; index++) {
          final rect = tester.getRect(covers.at(index));
          expect(rect.width / rect.height, closeTo(2 / 3, .001));
          expect(rect.width, closeTo(first.width, .01));
          expect(
            tester.getSize(tiles.at(index)).height,
            tester.getSize(tiles.first).height,
          );
        }
        expect(tester.takeException(), isNull);
      }
    });
  }
}
