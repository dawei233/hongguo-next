import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'ui/tokens.dart';

abstract final class AppTheme {
  static final light = _theme(Brightness.light);
  static final dark = _theme(Brightness.dark);

  /// 该亮度的设计令牌（`docs/DESIGN-SYSTEM.md` §3 的 Flutter 镜像）。
  static HongguoTokens tokensOf(Brightness brightness) =>
      brightness == Brightness.dark ? HongguoTokens.dark : HongguoTokens.light;

  static ThemeMode mode(String preference) => switch (preference) {
    'light' => ThemeMode.light,
    'dark' => ThemeMode.dark,
    _ => ThemeMode.system,
  };

  static String label(String preference) => switch (preference) {
    'light' => '浅色',
    'dark' => '深色',
    _ => '跟随系统',
  };

  static SystemUiOverlayStyle systemBars(Brightness brightness) {
    final icons = brightness == Brightness.dark
        ? Brightness.light
        : Brightness.dark;
    return SystemUiOverlayStyle(
      statusBarColor: Colors.transparent,
      statusBarIconBrightness: icons,
      statusBarBrightness: brightness,
      systemStatusBarContrastEnforced: false,
      systemNavigationBarColor: Colors.transparent,
      systemNavigationBarDividerColor: Colors.transparent,
      systemNavigationBarIconBrightness: icons,
      systemNavigationBarContrastEnforced: false,
    );
  }

  static ThemeData _theme(Brightness brightness) {
    final dark = brightness == Brightness.dark;
    final tokens = tokensOf(brightness);
    // 令牌先落进 ColorScheme：§3.1 的颜色全部显式给定（seed 也换成品牌色），
    // surface/容器/描边不再由 M3 从 seed 推导；M3 只留 ripple、elevation
    // 等平台原生行为（DESIGN-SYSTEM.md §6.1）。
    final scheme =
        ColorScheme.fromSeed(
          seedColor: tokens.primary,
          brightness: brightness,
        ).copyWith(
          primary: tokens.primary,
          onPrimary: tokens.onPrimary,
          primaryContainer: tokens.primarySubtle,
          onPrimaryContainer: tokens.onPrimarySubtle,
          secondary: tokens.secondary,
          onSecondary: tokens.onSecondary,
          secondaryContainer: tokens.surface,
          onSecondaryContainer: tokens.fg,
          // gold 承接 guoapp 的 tertiary，两个主题下都是亮色，
          // 故前景取该主题最深的文字色（浅色 fg、深色 onPrimary）。
          tertiary: tokens.gold,
          onTertiary: dark ? tokens.onPrimary : tokens.fg,
          tertiaryContainer: tokens.gold,
          onTertiaryContainer: dark ? tokens.onPrimary : tokens.fg,
          error: tokens.danger,
          onError: tokens.onDanger,
          errorContainer: tokens.danger,
          onErrorContainer: tokens.onDanger,
          surface: tokens.canvas,
          onSurface: tokens.fg,
          onSurfaceVariant: tokens.fgMuted,
          surfaceDim: tokens.canvas,
          surfaceBright: tokens.card,
          // 容器阶梯逐档取自令牌（浅↔深单调）：M3 的 Card 默认用 Low，
          // 而 §3.1 里「卡片/面板」是 --card、「次级面/inset」是 --surface。
          surfaceContainerLowest: tokens.canvas,
          surfaceContainerLow: tokens.card,
          surfaceContainer: tokens.surface,
          surfaceContainerHigh: tokens.surfaceHover,
          surfaceContainerHighest: tokens.border,
          outline: tokens.input,
          outlineVariant: tokens.borderSubtle,
          shadow: tokens.stage,
          scrim: tokens.scrim,
          inverseSurface: tokens.stage,
          onInverseSurface: tokens.danmaku,
          inversePrimary: tokens.primaryHover,
          surfaceTint: Colors.transparent,
        );
    return ThemeData(
      useMaterial3: true,
      brightness: brightness,
      colorScheme: scheme,
      extensions: [tokens],
      scaffoldBackgroundColor: tokens.canvas,
      appBarTheme: AppBarTheme(
        backgroundColor: tokens.canvas,
        foregroundColor: scheme.onSurface,
        scrolledUnderElevation: 0,
        elevation: 0,
        centerTitle: false,
        systemOverlayStyle: systemBars(brightness),
      ),
      dividerTheme: DividerThemeData(color: scheme.outlineVariant),
      navigationRailTheme: NavigationRailThemeData(
        backgroundColor: tokens.canvas,
        useIndicator: false,
        selectedIconTheme: IconThemeData(color: scheme.primary),
        unselectedIconTheme: IconThemeData(color: scheme.onSurfaceVariant),
        selectedLabelTextStyle: TextStyle(
          color: scheme.primary,
          fontWeight: FontWeight.w700,
        ),
        unselectedLabelTextStyle: TextStyle(color: scheme.onSurfaceVariant),
      ),
      inputDecorationTheme: InputDecorationTheme(
        filled: true,
        fillColor: scheme.surfaceContainer,
        border: OutlineInputBorder(
          // §3.4 的 radius-2xl（此前是裸写的 16）。
          borderRadius: BorderRadius.circular(HnSpacing.radius2xl),
          borderSide: BorderSide.none,
        ),
      ),
    );
  }
}
