import 'dart:async';

import 'package:hongguo_next/app_build.dart';
import 'package:hongguo_next/core_bridge.dart';
import 'package:hongguo_next/local_store.dart';
import 'package:hongguo_next/main.dart';
import 'package:hongguo_next/models.dart';
import 'package:hongguo_next/widgets.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'interface_fixtures.dart';

void main() {
  Future<LocalStore> localStore() async {
    SharedPreferences.setMockInitialValues({});
    final store = LocalStore(await SharedPreferences.getInstance());
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
    await tester.pumpWidget(DuanjuApp(repository: repository, store: store));
    await tester.pumpAndSettle();
    Future<void> select(String name) async {
      if (SourceGroup.fromSources(SourceSite.values).length <= 1) return;
      await tester.tap(find.byKey(const ValueKey('source-switch')));
      await tester.pumpAndSettle();
      await tester.tap(find.text(name).last);
      await tester.pumpAndSettle();
    }

    for (final group in SourceGroup.fromSources(SourceSite.values)) {
      await select(group.name);
      if (group.id == 'huangdou') {
        expect(find.text('会员合成剧'), findsNothing);
        await tester.tap(find.byTooltip('VIP：隐藏'));
        await tester.pumpAndSettle();
        expect(find.text('会员合成剧'), findsOneWidget);
      } else {
        expect(find.byTooltip('VIP：隐藏'), findsNothing);
        expect(find.byTooltip('VIP：显示'), findsNothing);
        expect(find.text('会员合成剧'), findsWidgets);
      }
    }
    if (!allSourcesEnabled) {
      for (final source in SourceSite.knownValues.skip(1)) {
        expect(find.widgetWithText(ChoiceChip, source.name), findsNothing);
      }
      return;
    }
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
      for (var tab = 0; tab < 3; tab++) {
        if (tab > 0) {
          final destination = layout.$3
              ? find.byKey(ValueKey('tv-nav-$tab'))
              : layout.$1.width < 840
              ? find.byKey(ValueKey('bottom-nav-$tab'))
              : find.descendant(
                  of: find.byType(NavigationRail),
                  matching: find.text(tab == 1 ? '追剧' : '最近观看'),
                );
          await tester.tap(destination);
          await tester.pumpAndSettle();
        }
        final covers = find.byType(DramaCover);
        final tiles = find.byType(DramaTile);
        expect(covers, findsNWidgets(3));
        final first = tester.getRect(covers.first);
        catalogSize ??= first.size;
        expect(first.width, closeTo(catalogSize.width, .01));
        expect(first.height, closeTo(catalogSize.height, .01));
        for (var index = 0; index < 3; index++) {
          final rect = tester.getRect(covers.at(index));
          expect(rect.width / rect.height, closeTo(2 / 3, .001));
          expect(rect.width, closeTo(first.width, .01));
          expect(rect.bottom, closeTo(first.bottom, .01));
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
