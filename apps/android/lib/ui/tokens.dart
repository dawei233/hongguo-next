import 'package:flutter/material.dart';

/// `docs/DESIGN-SYSTEM.md` §3 的令牌表在 Flutter 侧的镜像（§6.1）。
///
/// **单一事实来源**：`theme.css` 的 OKLCH 为权威值，本文件的 HEX 只作对照；
/// 改一处必须同步另一处，并由 §7 的 `pnpm check:tokens` 守卫。
///
/// 与 PC 端共享的是本文件的 `HongguoTokens`，而不是 M3 的 `colorScheme.*`：
/// M3 只负责 ripple / elevation 等平台原生行为（§6.1），因此
/// `AppTheme` 把 §3.1 全部颜色显式写进 `ColorScheme`，不再让 M3 推导
/// surface / 容器色。
///
/// 未镜像项（§6.1 的完整性清单已定）：§3.3 的 `--sidebar-w` / `--toolbar-h`
/// 等布局常量是 PC 端形态专属（§6.2 明确 Dart 侧只定义尺度、不定义断点）；
/// §3.7 的 z-index 在 Flutter 由 `Stack` 子序与 `Overlay` 入队序表达；
/// reduced motion 由 [HongguoTokensX.reduceMotion] 读取无障碍开关。
@immutable
class HongguoTokens extends ThemeExtension<HongguoTokens> {
  const HongguoTokens({
    required this.canvas,
    required this.card,
    required this.popover,
    required this.surface,
    required this.surfaceHover,
    required this.fg,
    required this.fgMuted,
    required this.fgSubtle,
    required this.primary,
    required this.primaryHover,
    required this.onPrimary,
    required this.primarySubtle,
    required this.onPrimarySubtle,
    required this.secondary,
    required this.onSecondary,
    required this.muted,
    required this.mutedFg,
    required this.accent,
    required this.onAccent,
    required this.danger,
    required this.onDanger,
    required this.warning,
    required this.onWarning,
    required this.success,
    required this.onSuccess,
    required this.gold,
    required this.border,
    required this.borderSubtle,
    required this.input,
    required this.ring,
    required this.sidebar,
    required this.sidebarFg,
    required this.sidebarPrimary,
    required this.sidebarOnPrimary,
    required this.sidebarBorder,
    required this.skeleton,
    required this.scrim,
    required this.stage,
    required this.danmaku,
    required this.scrollThumb,
  });

  /// §3.1 `--background`：应用画布。
  final Color canvas;

  /// §3.1 `--card` / `--popover`：卡片与面板（两者同值）。
  final Color card;

  /// §3.1 `--popover`：浮层。
  final Color popover;

  /// §3.1 `--surface`：次级面（侧栏、inset）。
  final Color surface;

  /// §3.1 `--surface-hover`：行 hover。
  final Color surfaceHover;

  /// §3.1 `--foreground`：主文字。
  final Color fg;

  /// §3.1 `--foreground-muted`：次要文字。
  final Color fgMuted;

  /// §3.1 `--foreground-subtle`：三级文字/占位。
  final Color fgSubtle;

  /// §3.1 `--primary`：品牌红/主要动作。
  final Color primary;

  /// §3.1 `--primary-hover`。
  final Color primaryHover;

  /// §3.1 `--primary-foreground`：品牌底上的字。
  final Color onPrimary;

  /// §3.1 `--primary-subtle`：选中底/弱强调面。
  final Color primarySubtle;

  /// §3.1 `--primary-foreground-soft`：弱强调面上的字。
  final Color onPrimarySubtle;

  /// §3.1 `--secondary`：次级填充按钮。
  final Color secondary;

  /// §3.1 `--secondary-foreground`。
  final Color onSecondary;

  /// §3.1 `--muted`：弱面。
  final Color muted;

  /// §3.1 `--muted-foreground`。
  final Color mutedFg;

  /// §3.1 `--accent`：hover/焦点面。
  final Color accent;

  /// §3.1 `--accent-foreground`。
  final Color onAccent;

  /// §3.1 `--destructive`：危险/错误。
  final Color danger;

  /// §3.1 `--destructive-foreground`。
  final Color onDanger;

  /// §3.1 `--warning`。
  final Color warning;

  /// §3.1 `--warning-foreground`（表格未给 HEX，由该行 OKLCH 换算）。
  final Color onWarning;

  /// §3.1 `--success`：已下载/已完成。
  final Color success;

  /// §3.1 `--success-foreground`（表格未给 HEX，由该行 OKLCH 换算）。
  final Color onSuccess;

  /// §3.1 `--gold`：VIP/会员（承接 guoapp tertiary）。
  final Color gold;

  /// §3.1 `--border`：卡片边框。
  final Color border;

  /// §3.1 `--border-subtle`：分隔线。
  final Color borderSubtle;

  /// §3.1 `--input`：输入框边框。
  final Color input;

  /// §3.1 `--ring`：焦点环。
  final Color ring;

  /// §3.1 `--sidebar`。
  final Color sidebar;

  /// §3.1 `--sidebar-foreground`（表格未给 HEX，由该行 OKLCH 换算）。
  final Color sidebarFg;

  /// §3.1 `--sidebar-primary`：侧栏选中底。
  final Color sidebarPrimary;

  /// §3.1 `--sidebar-primary-foreground`：侧栏选中字。
  final Color sidebarOnPrimary;

  /// §3.1 `--sidebar-border`（表格未给 HEX，由该行 OKLCH 换算）。
  final Color sidebarBorder;

  /// §3.1 `--skeleton`：骨架屏。
  final Color skeleton;

  /// §3.1 `--scrim`：模态遮罩（带 alpha）。
  final Color scrim;

  /// §3.1 `--stage`：**播放台恒定暗色**，双主题同值。
  ///
  /// §6.1 骨架把它写成与实例字段重名的 `static`，这里用私有常量承载同一值。
  final Color stage;

  /// §3.1 `--danmaku`：弹幕文字（播放台上恒定）。
  ///
  /// 取表格值 `#fbf8f1` + 0.92 alpha；§6.1 骨架写的 `0xFFFDF8F0`
  /// 与 §3.1 的 OKLCH/HEX 都不符，以 §3.1（权威令牌表）为准。
  final Color danmaku;

  /// §3.1 `--scroll-thumb`：滚动条。
  final Color scrollThumb;

  /// §3.1 `--stage` 的恒定值。
  static const Color _stage = Color(0xFF090A0C);

  /// §3.1 `--danmaku` 的恒定值（`oklch(0.980 0.010 90 / 0.92)`）。
  static const Color _danmaku = Color(0xEBFBF8F1);

  static const HongguoTokens light = HongguoTokens(
    canvas: Color(0xFFFBFAF8),
    card: Color(0xFFFFFFFF),
    popover: Color(0xFFFFFFFF),
    surface: Color(0xFFF0EEEB),
    surfaceHover: Color(0xFFEAE7E2),
    fg: Color(0xFF252523),
    fgMuted: Color(0xFF686661),
    fgSubtle: Color(0xFF77736C),
    primary: Color(0xFFC83933),
    primaryHover: Color(0xFFAF2A27),
    onPrimary: Color(0xFFFDF9F8),
    primarySubtle: Color(0xFFFAE9E7),
    onPrimarySubtle: Color(0xFFA12B27),
    secondary: Color(0xFFF0EEEB),
    onSecondary: Color(0xFF252523),
    muted: Color(0xFFF0EEEB),
    mutedFg: Color(0xFF686661),
    accent: Color(0xFFEAE7E2),
    onAccent: Color(0xFF252523),
    danger: Color(0xFFBA3437),
    onDanger: Color(0xFFFDF9F8),
    warning: Color(0xFF9A681E),
    onWarning: Color(0xFFFEF9F3),
    success: Color(0xFF32745C),
    onSuccess: Color(0xFFF7FBF9),
    gold: Color(0xFFE1A447),
    border: Color(0xFFE4E1DC),
    borderSubtle: Color(0xFFEEEBE6),
    input: Color(0xFFCCC8C1),
    ring: Color(0xFFC83933),
    sidebar: Color(0xFFF0EEEB),
    sidebarFg: Color(0xFF686661),
    sidebarPrimary: Color(0xFFFAE9E7),
    sidebarOnPrimary: Color(0xFFA12B27),
    sidebarBorder: Color(0xFFE4E1DC),
    skeleton: Color(0xFFE8E5E0),
    scrim: Color(0x73362C24),
    stage: _stage,
    danmaku: _danmaku,
    scrollThumb: Color(0xFFC6C2BB),
  );

  static const HongguoTokens dark = HongguoTokens(
    canvas: Color(0xFF111213),
    card: Color(0xFF1C1D20),
    popover: Color(0xFF1C1D20),
    surface: Color(0xFF262729),
    surfaceHover: Color(0xFF33353A),
    fg: Color(0xFFEFEDE8),
    fgMuted: Color(0xFFAAA59D),
    fgSubtle: Color(0xFFC5C0B8),
    primary: Color(0xFFF96A5D),
    primaryHover: Color(0xFFFF8370),
    onPrimary: Color(0xFF1B0806),
    primarySubtle: Color(0xFF512824),
    onPrimarySubtle: Color(0xFFF5A49C),
    secondary: Color(0xFF262729),
    onSecondary: Color(0xFFEFEDE8),
    muted: Color(0xFF262729),
    mutedFg: Color(0xFFAAA59D),
    accent: Color(0xFF33353A),
    onAccent: Color(0xFFEFEDE8),
    danger: Color(0xFFF0A0A1),
    onDanger: Color(0xFF1B0806),
    warning: Color(0xFFE1B978),
    onWarning: Color(0xFF2E1B00),
    success: Color(0xFF92C8AC),
    onSuccess: Color(0xFF00170E),
    gold: Color(0xFFF6C86B),
    border: Color(0xFF46433D),
    borderSubtle: Color(0xFF3A3833),
    input: Color(0xFF6D675F),
    ring: Color(0xFFF96A5D),
    sidebar: Color(0xFF17181B),
    sidebarFg: Color(0xFFC5C0B8),
    sidebarPrimary: Color(0xFF512824),
    sidebarOnPrimary: Color(0xFFF5A49C),
    sidebarBorder: Color(0xFF46433D),
    skeleton: Color(0xFF2D2F33),
    scrim: Color(0xA8000000),
    stage: _stage,
    danmaku: _danmaku,
    scrollThumb: Color(0xFF6D675F),
  );

  @override
  HongguoTokens copyWith({
    Color? canvas,
    Color? card,
    Color? popover,
    Color? surface,
    Color? surfaceHover,
    Color? fg,
    Color? fgMuted,
    Color? fgSubtle,
    Color? primary,
    Color? primaryHover,
    Color? onPrimary,
    Color? primarySubtle,
    Color? onPrimarySubtle,
    Color? secondary,
    Color? onSecondary,
    Color? muted,
    Color? mutedFg,
    Color? accent,
    Color? onAccent,
    Color? danger,
    Color? onDanger,
    Color? warning,
    Color? onWarning,
    Color? success,
    Color? onSuccess,
    Color? gold,
    Color? border,
    Color? borderSubtle,
    Color? input,
    Color? ring,
    Color? sidebar,
    Color? sidebarFg,
    Color? sidebarPrimary,
    Color? sidebarOnPrimary,
    Color? sidebarBorder,
    Color? skeleton,
    Color? scrim,
    Color? stage,
    Color? danmaku,
    Color? scrollThumb,
  }) {
    return HongguoTokens(
      canvas: canvas ?? this.canvas,
      card: card ?? this.card,
      popover: popover ?? this.popover,
      surface: surface ?? this.surface,
      surfaceHover: surfaceHover ?? this.surfaceHover,
      fg: fg ?? this.fg,
      fgMuted: fgMuted ?? this.fgMuted,
      fgSubtle: fgSubtle ?? this.fgSubtle,
      primary: primary ?? this.primary,
      primaryHover: primaryHover ?? this.primaryHover,
      onPrimary: onPrimary ?? this.onPrimary,
      primarySubtle: primarySubtle ?? this.primarySubtle,
      onPrimarySubtle: onPrimarySubtle ?? this.onPrimarySubtle,
      secondary: secondary ?? this.secondary,
      onSecondary: onSecondary ?? this.onSecondary,
      muted: muted ?? this.muted,
      mutedFg: mutedFg ?? this.mutedFg,
      accent: accent ?? this.accent,
      onAccent: onAccent ?? this.onAccent,
      danger: danger ?? this.danger,
      onDanger: onDanger ?? this.onDanger,
      warning: warning ?? this.warning,
      onWarning: onWarning ?? this.onWarning,
      success: success ?? this.success,
      onSuccess: onSuccess ?? this.onSuccess,
      gold: gold ?? this.gold,
      border: border ?? this.border,
      borderSubtle: borderSubtle ?? this.borderSubtle,
      input: input ?? this.input,
      ring: ring ?? this.ring,
      sidebar: sidebar ?? this.sidebar,
      sidebarFg: sidebarFg ?? this.sidebarFg,
      sidebarPrimary: sidebarPrimary ?? this.sidebarPrimary,
      sidebarOnPrimary: sidebarOnPrimary ?? this.sidebarOnPrimary,
      sidebarBorder: sidebarBorder ?? this.sidebarBorder,
      skeleton: skeleton ?? this.skeleton,
      scrim: scrim ?? this.scrim,
      stage: stage ?? this.stage,
      danmaku: danmaku ?? this.danmaku,
      scrollThumb: scrollThumb ?? this.scrollThumb,
    );
  }

  @override
  HongguoTokens lerp(ThemeExtension<HongguoTokens>? other, double t) {
    if (other is! HongguoTokens) {
      return this;
    }
    return HongguoTokens(
      canvas: Color.lerp(canvas, other.canvas, t)!,
      card: Color.lerp(card, other.card, t)!,
      popover: Color.lerp(popover, other.popover, t)!,
      surface: Color.lerp(surface, other.surface, t)!,
      surfaceHover: Color.lerp(surfaceHover, other.surfaceHover, t)!,
      fg: Color.lerp(fg, other.fg, t)!,
      fgMuted: Color.lerp(fgMuted, other.fgMuted, t)!,
      fgSubtle: Color.lerp(fgSubtle, other.fgSubtle, t)!,
      primary: Color.lerp(primary, other.primary, t)!,
      primaryHover: Color.lerp(primaryHover, other.primaryHover, t)!,
      onPrimary: Color.lerp(onPrimary, other.onPrimary, t)!,
      primarySubtle: Color.lerp(primarySubtle, other.primarySubtle, t)!,
      onPrimarySubtle: Color.lerp(onPrimarySubtle, other.onPrimarySubtle, t)!,
      secondary: Color.lerp(secondary, other.secondary, t)!,
      onSecondary: Color.lerp(onSecondary, other.onSecondary, t)!,
      muted: Color.lerp(muted, other.muted, t)!,
      mutedFg: Color.lerp(mutedFg, other.mutedFg, t)!,
      accent: Color.lerp(accent, other.accent, t)!,
      onAccent: Color.lerp(onAccent, other.onAccent, t)!,
      danger: Color.lerp(danger, other.danger, t)!,
      onDanger: Color.lerp(onDanger, other.onDanger, t)!,
      warning: Color.lerp(warning, other.warning, t)!,
      onWarning: Color.lerp(onWarning, other.onWarning, t)!,
      success: Color.lerp(success, other.success, t)!,
      onSuccess: Color.lerp(onSuccess, other.onSuccess, t)!,
      gold: Color.lerp(gold, other.gold, t)!,
      border: Color.lerp(border, other.border, t)!,
      borderSubtle: Color.lerp(borderSubtle, other.borderSubtle, t)!,
      input: Color.lerp(input, other.input, t)!,
      ring: Color.lerp(ring, other.ring, t)!,
      sidebar: Color.lerp(sidebar, other.sidebar, t)!,
      sidebarFg: Color.lerp(sidebarFg, other.sidebarFg, t)!,
      sidebarPrimary: Color.lerp(sidebarPrimary, other.sidebarPrimary, t)!,
      sidebarOnPrimary: Color.lerp(
        sidebarOnPrimary,
        other.sidebarOnPrimary,
        t,
      )!,
      sidebarBorder: Color.lerp(sidebarBorder, other.sidebarBorder, t)!,
      skeleton: Color.lerp(skeleton, other.skeleton, t)!,
      scrim: Color.lerp(scrim, other.scrim, t)!,
      stage: Color.lerp(stage, other.stage, t)!,
      danmaku: Color.lerp(danmaku, other.danmaku, t)!,
      scrollThumb: Color.lerp(scrollThumb, other.scrollThumb, t)!,
    );
  }
}

/// 用法：`context.tokens.primary`；`context.primary` 保留 §6.1 的便捷访问器。
extension HongguoTokensX on BuildContext {
  HongguoTokens get tokens => Theme.of(this).extension<HongguoTokens>()!;

  Color get primary => tokens.primary;

  /// §3.6 的 reduced motion：CSS 的 `@media` 在 Flutter 无效，改读无障碍开关。
  bool get reduceMotion => MediaQuery.disableAnimationsOf(this);
}

/// §3.3 间距 + §3.4 圆角（与 Tailwind 数字档逐档等值；只定义尺度，不定义断点）。
abstract final class HnSpacing {
  static const double step1 = 4;
  static const double step2 = 8;
  static const double step3 = 12;
  static const double step4 = 16;
  static const double step5 = 20;
  static const double step6 = 24;
  static const double step8 = 32;
  static const double step10 = 40;
  static const double step12 = 48;

  /// §3.4 由 10px 基准派生（`--radius` 系列）。
  static const double radiusXs = 5;
  static const double radiusSm = 7;
  static const double radiusMd = 9;
  static const double radiusLg = 10;
  static const double radiusXl = 13;
  static const double radius2xl = 16;
  static const double radius3xl = 20;
}

/// §3.2 的 10 档字号必须逐档镜像，否则两端标题/角标密度会漂移。
abstract final class HnTypo {
  static const double t2xs = 10;
  static const double tXs = 11;
  static const double tSm = 12;
  static const double tBase = 13;
  static const double tMd = 15;
  static const double tLg = 17;
  static const double tXl = 20;
  static const double t2xl = 26;
  static const double t3xl = 32;
  static const double tDisplay = 44;

  /// 数字一律等宽（对应 CSS 的 `--font-numeric` / `tabular-nums`）。
  static const List<FontFeature> tabular = [FontFeature.tabularFigures()];
}

/// §3.6 的动效令牌同值镜像（Flutter 无 CSS 过渡，用 Duration + Curve 表达）。
abstract final class HnMotion {
  static const Duration instant = Duration(milliseconds: 50);
  static const Duration fast = Duration(milliseconds: 100);
  static const Duration base = Duration(milliseconds: 150);
  static const Duration slow = Duration(milliseconds: 250);
  static const Duration player = Duration(milliseconds: 200);

  /// `--duration-base` / `--duration-slow` 的 `cubic-bezier(0.2, 0, 0, 1)`。
  static const Cubic ease = Cubic(0.2, 0, 0, 1);
  static const Cubic easeOutExpo = Cubic(0.16, 1, 0.3, 1);
  static const Cubic easeSpring = Cubic(0.34, 1.56, 0.64, 1);
}
