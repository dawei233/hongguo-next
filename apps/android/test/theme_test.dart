import 'dart:convert';

import 'package:hongguo_next/app_bottom_navigation.dart';
import 'package:hongguo_next/app_theme.dart';
import 'package:hongguo_next/core_bridge.dart';
import 'package:hongguo_next/home_screen.dart';
import 'package:hongguo_next/local_store.dart';
import 'package:hongguo_next/main.dart';
import 'package:hongguo_next/player_screen.dart';
import 'package:hongguo_next/settings_screen.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:media_kit/media_kit.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'fixtures.dart';
import 'player_fixtures.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  // DuanjuApp 会一路构造到 PlayerScreen；测试环境必须注入假播放器，
  // 否则会创建 media_kit 原生播放器（需要 libmpv，测试环境没有）。
  DuanjuApp app(AppRepository repository, {LocalStore? store}) => DuanjuApp(
    repository: repository,
    store: store,
    playerFactory: () => Player(platformPlayer: ScriptedPlayer()),
    videoBuilder: (controls) => controls,
  );

  Future<LocalStore> localStore() async {
    SharedPreferences.setMockInitialValues({});
    return LocalStore(await SharedPreferences.getInstance());
  }

  void phone(WidgetTester tester, [double width = 390]) {
    tester.view.physicalSize = Size(width, 844);
    tester.view.devicePixelRatio = 1;
    tester.view.padding = const FakeViewPadding(top: 24, bottom: 24);
    addTearDown(tester.view.reset);
  }

  test(
    'theme survives restart and backup, with compatible old backups',
    () async {
      final store = await localStore();
      expect(store.themeMode, 'system');
      await store.setThemeMode('light');
      final restored = LocalStore(store.preferences);
      expect(restored.themeMode, 'light');
      final backup = await store.exportBackup();
      await store.setThemeMode('system');
      await store.importBackup(backup);
      expect(store.themeMode, 'light');
      final old = jsonDecode(backup) as Map<String, dynamic>;
      old.remove('themeMode');
      await store.setThemeMode('system');
      await store.importBackup(jsonEncode(old));
      expect(store.themeMode, 'system');
      old['themeMode'] = 'invalid';
      await expectLater(
        store.importBackup(jsonEncode(old)),
        throwsFormatException,
      );
      expect(store.themeMode, 'system');
      store.dispose();
      restored.dispose();
    },
  );

  testWidgets(
    'default theme follows the system from bootstrap through browsing',
    (tester) async {
      phone(tester);
      addTearDown(tester.platformDispatcher.clearPlatformBrightnessTestValue);
      final repository = FixtureRepository();
      tester.platformDispatcher.platformBrightnessTestValue = Brightness.light;
      await tester.pumpWidget(app(repository));
      await tester.pump();
      expect(
        tester.widget<MaterialApp>(find.byType(MaterialApp)).themeMode,
        ThemeMode.system,
      );
      final store = await localStore();
      await tester.pumpWidget(app(repository, store: store));
      await tester.pumpAndSettle();
      for (final brightness in [Brightness.dark, Brightness.light]) {
        tester.platformDispatcher.platformBrightnessTestValue = brightness;
        await tester.pumpAndSettle();
        expect(
          Theme.of(tester.element(find.byType(HomeScreen))).brightness,
          brightness,
        );
      }
      await store.setThemeMode('dark');
      await tester.pumpAndSettle();
      expect(
        Theme.of(tester.element(find.byType(HomeScreen))).brightness,
        Brightness.dark,
      );
      expect(LocalStore(store.preferences).themeMode, 'dark');
      await tester.pumpWidget(const SizedBox.shrink());
      store.dispose();
    },
  );

  testWidgets('settings changes open routes without losing the active tab', (
    tester,
  ) async {
    phone(tester);
    final store = await localStore();
    final repository = FixtureRepository();
    await tester.pumpWidget(app(repository, store: store));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('bottom-nav-1')));
    await tester.pumpAndSettle();
    // 标题实际渲染为「我的追剧 · N」，用精确匹配永远命中不到。
    expect(find.textContaining('我的追剧'), findsOneWidget);
    await tester.tap(find.byTooltip('更多'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('设置与备份'));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('theme-setting')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('浅色'));
    await tester.pumpAndSettle();
    expect(store.themeMode, 'light');
    expect(
      Theme.of(tester.element(find.byType(SettingsScreen))).brightness,
      Brightness.light,
    );
    expect(
      SystemChrome.latestStyle?.systemNavigationBarIconBrightness,
      Brightness.dark,
    );
    expect(
      SystemChrome.latestStyle?.systemNavigationBarContrastEnforced,
      isFalse,
    );
    await tester.tap(find.byType(BackButton).last);
    await tester.pumpAndSettle();
    // 标题实际渲染为「我的追剧 · N」，用精确匹配永远命中不到。
    expect(find.textContaining('我的追剧'), findsOneWidget);
    expect(
      tester
          .widget<AppBottomNavigation>(find.byType(AppBottomNavigation))
          .selectedIndex,
      1,
    );
    final requests = repository.requests.length;
    await store.setThemeMode('system');
    tester.platformDispatcher.platformBrightnessTestValue = Brightness.dark;
    await tester.pumpAndSettle();
    expect(
      Theme.of(tester.element(find.byType(HomeScreen))).brightness,
      Brightness.dark,
    );
    tester.platformDispatcher.platformBrightnessTestValue = Brightness.light;
    await tester.pumpAndSettle();
    expect(
      Theme.of(tester.element(find.byType(HomeScreen))).brightness,
      Brightness.light,
    );
    expect(repository.requests.length, requests);
    // 标题实际渲染为「我的追剧 · N」，用精确匹配永远命中不到。
    expect(find.textContaining('我的追剧'), findsOneWidget);
    tester.platformDispatcher.clearPlatformBrightnessTestValue();
    await tester.pumpWidget(const SizedBox.shrink());
    store.dispose();
  });

  for (final count in [3, 4]) {
    testWidgets(
      '$count navigation destinations support large text and keyboard input',
      (tester) async {
        phone(tester, 320);
        final semantics = tester.ensureSemantics();
        var selected = 0;
        await tester.pumpWidget(
          MaterialApp(
            theme: AppTheme.light,
            home: StatefulBuilder(
              builder: (context, setState) => MediaQuery(
                data: MediaQuery.of(
                  context,
                ).copyWith(textScaler: TextScaler.linear(2)),
                child: Scaffold(
                  bottomNavigationBar: AppBottomNavigation(
                    selectedIndex: selected,
                    onDestinationSelected: (value) =>
                        setState(() => selected = value),
                    destinations: [
                      const NavigationDestination(
                        icon: Icon(Icons.explore_outlined),
                        label: '发现',
                      ),
                      const NavigationDestination(
                        icon: Icon(Icons.bookmark_border),
                        label: '追剧',
                      ),
                      const NavigationDestination(
                        icon: Icon(Icons.history),
                        label: '最近观看',
                      ),
                      if (count == 4)
                        const NavigationDestination(
                          icon: Icon(Icons.download_outlined),
                          label: '下载',
                        ),
                    ],
                  ),
                ),
              ),
            ),
          ),
        );
        await tester.pumpAndSettle();
        for (var i = 0; i < count; i++) {
          final size = tester.getSize(find.byKey(ValueKey('bottom-nav-$i')));
          expect(size.width, greaterThanOrEqualTo(48));
          expect(size.height, greaterThanOrEqualTo(48));
        }
        await tester.tap(find.byKey(const ValueKey('bottom-nav-1')));
        await tester.pumpAndSettle();
        expect(selected, 1);
        for (var i = 0; i < count; i++) {
          await tester.sendKeyEvent(LogicalKeyboardKey.tab);
          await tester.sendKeyEvent(LogicalKeyboardKey.enter);
          await tester.pumpAndSettle();
          expect(selected, i);
        }
        expect(find.text('最近观看'), findsOneWidget);
        semantics.dispose();
        expect(tester.takeException(), isNull);
      },
    );
  }

  testWidgets(
    'player shell follows light theme while video controls stay dark',
    (tester) async {
      phone(tester);
      debugDefaultTargetPlatformOverride = TargetPlatform.android;
      addTearDown(() => debugDefaultTargetPlatformOverride = null);
      final store = await localStore();
      await store.setThemeMode('light');
      final repository = RouteRepository();
      final platform = ScriptedPlayer();
      final detail = await repository.detail(FixtureRepository.free);
      await tester.pumpWidget(
        MaterialApp(
          theme: AppTheme.light,
          home: PlayerScreen(
            detail: detail,
            initialIndex: 0,
            repository: repository,
            store: store,
            playerFactory: () => Player(platformPlayer: platform),
            videoBuilder: (controls) =>
                SizedBox(key: const ValueKey('video-theme'), child: controls),
          ),
        ),
      );
      for (var i = 0; i < 15; i++) {
        await tester.pump(const Duration(milliseconds: 40));
      }
      expect(
        Theme.of(
          tester.element(find.byKey(const ValueKey('video-theme'))),
        ).brightness,
        Brightness.dark,
      );
      expect(
        Theme.of(tester.element(find.byType(PlayerScreen))).brightness,
        Brightness.light,
      );
      expect(
        SystemChrome.latestStyle?.systemNavigationBarIconBrightness,
        Brightness.dark,
      );
      expect(
        SystemChrome.latestStyle?.systemNavigationBarContrastEnforced,
        isFalse,
      );
      expect(find.text('选集'), findsOneWidget);
      expect(find.byKey(const ValueKey('play-episode-1')), findsOneWidget);
      await tester.pumpWidget(const SizedBox.shrink());
      for (var i = 0; i < 12; i++) {
        await tester.pump(const Duration(milliseconds: 10));
      }
      debugDefaultTargetPlatformOverride = null;
      expect(platform.disposed, isTrue);
      expect(tester.takeException(), isNull);
      store.dispose();
    },
  );
}
