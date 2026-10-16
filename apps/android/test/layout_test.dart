import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:hongguo_next/ui/layout.dart';

/// `ANDROID-UI-REDESIGN.md` §2.1 / §7 第 5 条的回归测试。
///
/// 本文件锁住 B1 的成果：**列数算法单点**（`HnBreak`）。改造前有四处互不
/// 一致的算法（§0 诊断：同一 1200px 屏上发现页 7 列、选集 5 列、推荐 6 列），
/// 收敛后任何新写的列数算术都会让下面的取值断言失败。
void main() {
  group('HnBreak.formOf 只认宽 + 朝向（§2 形态矩阵）', () {
    test('平板横屏：宽 ≥900 且高 ≥600', () {
      expect(HnBreak.formOf(const Size(1200, 800)), HnForm.tabletLandscape);
      expect(HnBreak.formOf(const Size(960, 600)), HnForm.tabletLandscape);
    });

    test('矮横屏不掉进平板档（900×500 放不下主从结构）', () {
      // §2.1 判定顺序：先看「宽 ≥ tablet 且高 ≥ railTabletMinHeight」。
      // 这条是防回归 —— 若有人把判定简化成只看宽，本用例会红。
      expect(HnBreak.formOf(const Size(900, 500)), HnForm.phoneLandscape);
      expect(HnBreak.formOf(const Size(1280, 400)), HnForm.phoneLandscape);
    });

    test('手机竖屏 / 手机横屏', () {
      expect(HnBreak.formOf(const Size(390, 844)), HnForm.phonePortrait);
      expect(HnBreak.formOf(const Size(320, 844)), HnForm.phonePortrait);
      expect(HnBreak.formOf(const Size(844, 390)), HnForm.phoneLandscape);
    });

    test('同一尺寸旋转后是不同形态（不认设备类型）', () {
      expect(HnBreak.formOf(const Size(1200, 800)), HnForm.tabletLandscape);
      expect(HnBreak.formOf(const Size(800, 1200)), HnForm.phonePortrait);
    });

    test('左轨宽度：平板 88 / 手机横屏 72 / 竖屏 0', () {
      expect(HnBreak.railWidth(HnForm.tabletLandscape), 88);
      expect(HnBreak.railWidth(HnForm.phoneLandscape), 72);
      expect(HnBreak.railWidth(HnForm.phonePortrait), 0);
    });
  });

  group('posterColumns（§2.1）', () {
    test('竖屏固定 2 列 / 手机横屏固定 3 列', () {
      expect(HnBreak.posterColumns(350, landscape: false), 2);
      expect(HnBreak.posterColumns(700, landscape: true, screenWidth: 700), 3);
    });

    test('平板横屏每列都真正 ≥ 150px（minmax(150px,1fr) 的下界）', () {
      // 这条是本实现与规范 §2 那句「1200px 约 7 列」的分歧点：
      // 规范算式忽略列间隙，取 7 列会让每张卡缩到 136px，违反它自己引用的
      // minmax(150px,1fr)。本用例锁住「物理上放得下」这个更强的性质。
      for (final width in [900.0, 1000.0, 1100.0, 1200.0, 1280.0, 1440.0, 1600.0]) {
        final available = width - HnBreak.railWide - 48;
        final columns = HnBreak.posterColumns(
          available,
          landscape: true,
          screenWidth: width,
        );
        final tile = (available - (columns - 1) * HnBreak.posterGap) / columns;
        expect(
          tile,
          greaterThanOrEqualTo(HnBreak.posterMinWidth),
          reason: '$width px 下取 $columns 列，每列 ${tile.toStringAsFixed(1)}px 窄于 150px',
        );
      }
    });

    test('§7 第 7 条的验收：1200px 平板横屏 ≥6 列', () {
      final columns = HnBreak.posterColumns(
        1200 - HnBreak.railWide - 48,
        landscape: true,
        screenWidth: 1200,
      );
      expect(columns, greaterThanOrEqualTo(6));
    });

    test('宽度单调不减（更宽的屏不会得到更少列）', () {
      var previous = 0;
      for (var width = 900.0; width <= 2000; width += 50) {
        final columns = HnBreak.posterColumns(
          width - HnBreak.railWide - 48,
          landscape: true,
          screenWidth: width,
        );
        expect(columns, greaterThanOrEqualTo(previous));
        previous = columns;
      }
    });

    test('未传 screenWidth 时退化为用 width 判形态（TV 整屏宽路径）', () {
      // B7c：原 TV 网格（_televisionGrid）已删除；此处的 landscape/screenWidth
      // 口径保留 —— 平板横屏（无左轨扣减时）仍按同一公式判定。
      expect(HnBreak.posterColumns(1112, landscape: true), 6);
    });
  });

  group('episodeColumns（§2.1）', () {
    test('固定档位：平板横屏 8 / 手机横屏 6 / 手机竖屏 4', () {
      expect(HnBreak.episodeColumns(1000, landscape: true), 8);
      expect(HnBreak.episodeColumns(700, landscape: true), 6);
      expect(HnBreak.episodeColumns(350, landscape: false), 4);
    });

    test('minTileWidth 生效时按可用宽重算，但不低于 3 列', () {
      // 三位数集号 + 放大字号时单格需要更宽，列数应随之下降。
      expect(
        HnBreak.episodeColumns(1000, landscape: true, minTileWidth: 200),
        5,
      );
      // 极窄可用宽时不退化成单列（100 集的选集网格单列会变成 100 行滚动）。
      expect(
        HnBreak.episodeColumns(200, landscape: false, minTileWidth: 300),
        3,
      );
    });

    test('minTileWidth 不放大列数（只可能收紧）', () {
      expect(
        HnBreak.episodeColumns(2000, landscape: true, minTileWidth: 40),
        8,
      );
    });
  });

  test('固定网格常量集中定义（非响应式，但避免字面量散落）', () {
    expect(HnBreak.keyboardColumns, 6);
    expect(HnBreak.episodeSidebarColumns, 4);
  });
}
