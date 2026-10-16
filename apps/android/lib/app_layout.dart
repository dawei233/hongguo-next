import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

export 'app_build.dart';

const appVersion = '0.3.2';

// B7c：`televisionTheme()` 已删除 —— 它给 TV 放大的焦点边框（3px）、
// 最小 52×48 的按钮与 17 号字。TV 下线后全应用只用 `AppTheme.light/dark`。


class AppDevice {
  const AppDevice({this.version = appVersion});
  final String version;
  static const channel = MethodChannel('duanju/device');

  static Future<AppDevice> detect({
    AppDevice fallback = const AppDevice(),
  }) async {
    if (defaultTargetPlatform != TargetPlatform.android) {
      return const AppDevice();
    }
    try {
      final data = await channel
          .invokeMapMethod<String, dynamic>('deviceInfo')
          .timeout(const Duration(seconds: 2));
      // B7c：原先这里还会读 `data['television']` 做 TV 检测，随 TV 下线删除。
      // 保留 `version` —— 关于页与局域网设备互联仍在用。
      final version = data?['version'];
      return AppDevice(
        version: version is String && version.isNotEmpty
            ? version
            : fallback.version,
      );
    } on PlatformException {
      return fallback;
    } on MissingPluginException {
      return fallback;
    } on TimeoutException {
      return fallback;
    }
  }

  static Future<double> getBrightness() async {
    if (defaultTargetPlatform != TargetPlatform.android) return 0.5;
    try {
      final value = await channel.invokeMethod<double>('getBrightness');
      return (value ?? 0.5).clamp(0.0, 1.0);
    } catch (_) {
      return 0.5;
    }
  }

  static Future<void> setBrightness(double brightness) async {
    if (defaultTargetPlatform != TargetPlatform.android) return;
    try {
      await channel.invokeMethod('setBrightness', {'brightness': brightness.clamp(0.01, 1.0)});
    } catch (_) {}
  }

  static Future<void> resetBrightness() async {
    if (defaultTargetPlatform != TargetPlatform.android) return;
    try {
      await channel.invokeMethod('resetBrightness');
    } catch (_) {}
  }

  /// 读取系统媒体音量，归一化到 0.0~1.0。非 Android 或读取失败按满音量处理。
  static Future<double> getVolume() async {
    if (defaultTargetPlatform != TargetPlatform.android) return 1.0;
    try {
      final value = await channel.invokeMethod<double>('getVolume');
      return (value ?? 1.0).clamp(0.0, 1.0);
    } catch (_) {
      return 1.0;
    }
  }

  /// 写入系统媒体音量并返回实际生效值（系统按档位量化后可能与请求值略有出入）。
  static Future<double> setVolume(double volume) async {
    final requested = volume.clamp(0.0, 1.0);
    if (defaultTargetPlatform != TargetPlatform.android) return requested;
    try {
      final applied = await channel.invokeMethod<double>('setVolume', {
        'volume': requested,
      });
      return (applied ?? requested).clamp(0.0, 1.0);
    } catch (_) {
      return requested;
    }
  }
}

class AppLayout extends InheritedWidget {
  const AppLayout({
    super.key,
    this.version = appVersion,
    required super.child,
  });
  final String version;

  /// B7c：原 `static bool isTelevision(BuildContext)` 已删除 —— 它读取本
  /// InheritedWidget 的 `television` 字段（设备级 TV 标志）。TV 全线下线后
  /// 形态判定统一由 `HnBreak.formOf`（宽 + 朝向）承担，**只有一处**。
  static String versionOf(BuildContext context) =>
      context.dependOnInheritedWidgetOfExactType<AppLayout>()?.version ??
      appVersion;

  @override
  bool updateShouldNotify(AppLayout oldWidget) =>
      version != oldWidget.version;
}
