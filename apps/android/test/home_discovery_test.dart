import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hongguo_next/local_store.dart';
import 'package:hongguo_next/main.dart';
import 'package:hongguo_next/models.dart';
import 'package:hongguo_next/ui/components/hn_featured_card.dart';
import 'package:hongguo_next/ui/components/hn_hero_rail.dart';
import 'package:hongguo_next/ui/components/hn_section_header.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'fixtures.dart';

/// 发现页首屏**集成**测试（§4.1.1）：验证 Hero 轨 / 今日首选 / 区块头
/// 在真实页面里按序渲染，而不只是组件单测能过。
///
/// 组件单测（`hn_hero_featured_test.dart`）证明组件本身正确；本文件证明
/// **接线正确** —— 这是两类不同的失败，B5 那次"组件对但没被调用"就是例证。
///
/// 注意用的是 [HeroFixtureRepository]：`FixtureRepository` 没实现
/// `recommendations`，首屏会（按设计）静默降级为空，那样测不到接线。
/// 提供推荐位的夹具 —— `FixtureRepository` 没实现 `recommendations`，
/// 而首屏 Hero/今日首选的数据正来自它。这里补上，否则首屏会（正确地）
/// 静默降级为空，就测不到接线。
class HeroFixtureRepository extends FixtureRepository {
  @override
  Future<CatalogPage> recommendations(
    String genre, {
    bool more = false,
    bool force = false,
  }) async => CatalogPage(const [
    Drama(
      id: 'hongguo:hero1',
      source: 'hongguo',
      title: '正在热播一',
      category: '都市',
      episodes: 32,
      description: '首屏横轨第一条。',
    ),
    Drama(
      id: 'hongguo:hero2',
      source: 'hongguo',
      title: '正在热播二',
      category: '悬疑',
      episodes: 24,
    ),
  ]);
}

void main() {
  Future<LocalStore> mount(WidgetTester tester, Size size) async {
    tester.view.physicalSize = size;
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    SharedPreferences.setMockInitialValues({});
    final store = testStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    await tester.pumpWidget(
      DuanjuApp(repository: HeroFixtureRepository(), store: store),
    );
    await tester.pumpAndSettle();
    return store;
  }

  testWidgets('§4.1.1：平板横屏首屏自上而下为 Hero → 今日首选 → 区块头', (tester) async {
    await mount(tester, const Size(1200, 800));
    // 三块都应在（FixtureRepository 的 recommendations 返回数据）。
    final hero = find.byType(HnHeroRail);
    final featured = find.byType(HnFeaturedCard);
    final header = find.byType(HnSectionHeader);
    expect(hero, findsOneWidget, reason: '首屏应有「正在热播」横轨');
    expect(featured, findsOneWidget, reason: '首屏应有「今日首选」大卡');
    expect(header, findsOneWidget, reason: '网格上方应有区块头');

    // 顺序：Hero 在 Featured 之上，Featured 在区块头之上。
    final heroTop = tester.getTopLeft(hero).dy;
    final featuredTop = tester.getTopLeft(featured).dy;
    final headerTop = tester.getTopLeft(header).dy;
    expect(heroTop, lessThan(featuredTop));
    expect(featuredTop, lessThan(headerTop));
  });

  testWidgets('手机竖屏同样有这三块（横屏不是唯一形态）', (tester) async {
    await mount(tester, const Size(390, 844));
    expect(find.byType(HnHeroRail), findsOneWidget);
    expect(find.byType(HnFeaturedCard), findsOneWidget);
    expect(find.byType(HnSectionHeader), findsOneWidget);
  });

  testWidgets('搜索态不显示 Hero（过滤态首屏展示过滤结果）', (tester) async {
    await mount(tester, const Size(1200, 800));
    expect(find.byType(HnHeroRail), findsOneWidget);
    await tester.tap(find.byKey(const ValueKey('toggle-search')));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField), '测试');
    await tester.testTextInput.receiveAction(TextInputAction.search);
    await tester.pumpAndSettle();
    // 进入搜索态后横轨收起（`_loadHero` 只在无过滤时刷新，且搜索态走
    // 独立的推荐/结果视图）。断言「不再显示」以免首屏被搜索无关内容占满。
    expect(find.byType(HnHeroRail), findsNothing);
  });
}
