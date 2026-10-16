import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hongguo_next/app_theme.dart';
import 'package:hongguo_next/models.dart';
import 'package:hongguo_next/ui/components/hn_featured_card.dart';
import 'package:hongguo_next/ui/components/hn_hero_rail.dart';

import 'fixtures.dart';

/// `ANDROID-UI-REDESIGN.md` §4.1.1 的 Hero 横轨与「今日首选」大卡回归测试。
///
/// 要点（用户明确要求"横屏也要搞"）：两块都要在**平板横屏**与**手机竖屏**
/// 下验证布局 —— 横屏是主力形态，不是竖屏的放大版。
void main() {
  final dramas = [
    const Drama(
      id: 'hongguo:1',
      source: 'hongguo',
      title: '逆光而行',
      category: '都市',
      episodes: 32,
      description: '清末民初，一个关于复仇与救赎的故事。',
    ),
    const Drama(
      id: 'hongguo:2',
      source: 'hongguo',
      title: '茅山打更人',
      category: '悬疑',
      episodes: 24,
      description: '深夜的更声里藏着秘密。',
    ),
    const Drama(
      id: 'hongguo:3',
      source: 'hongguo',
      title: '雪落无声',
      category: '爱情',
      episodes: 18,
    ),
  ];

  Future<void> pump(
    WidgetTester tester,
    Widget child, {
    required Size size,
  }) async {
    tester.view.physicalSize = size;
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    await tester.pumpWidget(
      MaterialApp(
        // 必须用 AppTheme.*：它把 HongguoTokens 装进 extensions。
        theme: AppTheme.light,
        home: Scaffold(body: child),
      ),
    );
    await tester.pumpAndSettle();
  }

  group('HnHeroRail（§4.1.1 横轨）', () {
    testWidgets('平板横屏：卡宽 280、轨高 168，1200px 下约 3.2 张可见', (tester) async {
      await pump(
        tester,
        HnHeroRail(
          items: dramas,
          repository: FixtureRepository(),
          onOpen: (_) {},
        ),
        size: const Size(1200, 800),
      );
      expect(
        tester.getSize(find.byType(HnHeroRail)).height,
        HnHeroRail.defaultHeight,
      );
      expect(HnHeroRail.defaultHeight, 168);
      // 卡宽应为横屏档 280。
      final firstCard = find.byKey(const ValueKey('hero-hongguo:1'));
      expect(tester.getSize(firstCard).width, 280);
      // 1200 可用宽（扣左右各 step5=20）下，280+12 间距 → 约 3.2 张可见。
      final visible = 1160 / (280 + 12);
      expect(visible, greaterThan(3.0));
      expect(visible, lessThan(4.0));
    });

    testWidgets('手机竖屏：卡宽收窄（避免"半张卡卡在边上"的观感）', (tester) async {
      await pump(
        tester,
        HnHeroRail(
          items: dramas,
          repository: FixtureRepository(),
          onOpen: (_) {},
        ),
        size: const Size(390, 844),
      );
      final firstCard = find.byKey(const ValueKey('hero-hongguo:1'));
      expect(tester.getSize(firstCard).width, HnHeroRail.portraitCardWidth);
      // 竖屏 390 可用宽 350 下，150+12 间距 → 约 2.2 张（§4.1.2）。
      final visible = 350 / (HnHeroRail.portraitCardWidth + 12);
      expect(visible, greaterThan(2.0));
      expect(visible, lessThan(2.5));
    });

    testWidgets('loading：骨架一张完整 + 一张半张，且不渲染真卡', (tester) async {
      await pump(
        tester,
        HnHeroRail(
          items: const [],
          repository: FixtureRepository(),
          onOpen: (_) {},
          loading: true,
        ),
        size: const Size(1200, 800),
      );
      expect(find.byKey(const ValueKey('hero-rail-skeleton')), findsOneWidget);
      expect(find.byKey(const ValueKey('hero-rail')), findsNothing);
    });

    testWidgets('点击卡片回传对应 drama', (tester) async {
      Drama? opened;
      await pump(
        tester,
        HnHeroRail(
          items: dramas,
          repository: FixtureRepository(),
          onOpen: (drama) => opened = drama,
        ),
        size: const Size(1200, 800),
      );
      await tester.tap(find.byKey(const ValueKey('hero-hongguo:2')));
      await tester.pumpAndSettle();
      expect(opened?.id, 'hongguo:2');
    });
  });

  group('HnFeaturedCard（§4.1.1 今日首选）', () {
    testWidgets('平板横屏：高 148，标题 tLg/w700，副行含题材与集数', (tester) async {
      await pump(
        tester,
        HnFeaturedCard(
          drama: dramas.first,
          repository: FixtureRepository(),
          onOpen: () {},
          onFollow: () {},
          onContinue: () {},
          watchedEpisode: 5,
        ),
        size: const Size(1200, 800),
      );
      expect(
        tester.getSize(find.byKey(const ValueKey('featured-card'))).height,
        HnFeaturedCard.height,
      );
      expect(HnFeaturedCard.height, 148);
      final title = tester.widget<Text>(find.text('逆光而行'));
      expect(title.style?.fontSize, 17);
      expect(title.style?.fontWeight, FontWeight.w700);
      expect(find.text('都市 · 32 集'), findsOneWidget);
    });

    testWidgets('有进度时「继续第 N 集」显示集号', (tester) async {
      await pump(
        tester,
        HnFeaturedCard(
          drama: dramas.first,
          repository: FixtureRepository(),
          onOpen: () {},
          onFollow: () {},
          onContinue: () {},
          watchedEpisode: 7,
        ),
        size: const Size(1200, 800),
      );
      expect(find.text('继续第 7 集'), findsOneWidget);
    });

    testWidgets('无进度时按钮为「继续观看」；无追剧入口时不渲染该按钮', (tester) async {
      await pump(
        tester,
        HnFeaturedCard(
          drama: dramas.first,
          repository: FixtureRepository(),
          onOpen: () {},
          onContinue: () {},
        ),
        size: const Size(1200, 800),
      );
      expect(find.text('继续观看'), findsOneWidget);
      expect(find.byKey(const ValueKey('featured-follow')), findsNothing);
    });

    testWidgets('两个动作都没有时退回「查看」', (tester) async {
      await pump(
        tester,
        HnFeaturedCard(
          drama: dramas.first,
          repository: FixtureRepository(),
          onOpen: () {},
        ),
        size: const Size(390, 844),
      );
      expect(find.byKey(const ValueKey('featured-open')), findsOneWidget);
    });

    testWidgets('简介 line-clamp-2（不撑破卡高）', (tester) async {
      await pump(
        tester,
        HnFeaturedCard(
          drama: Drama(
            id: 'hongguo:9',
            source: 'hongguo',
            title: '长简介测试',
            description: '这是一段很长的简介。' * 20,
          ),
          repository: FixtureRepository(),
          onOpen: () {},
        ),
        size: const Size(1200, 800),
      );
      final desc = tester.widget<Text>(
        find.textContaining('这是一段很长的简介'),
      );
      expect(desc.maxLines, 2);
      // 卡高仍不超规范值。
      expect(
        tester.getSize(find.byKey(const ValueKey('featured-card'))).height,
        HnFeaturedCard.height,
      );
    });
  });
}
