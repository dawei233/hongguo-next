import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';


class AppOrientationController {
  AppOrientationController();

  bool _disposed = false;
  Object? _playbackOwner;
  List<DeviceOrientation> _playbackOrientations = DeviceOrientation.values;
  List<DeviceOrientation>? _appliedOrientations;
  Future<void> _pending = Future<void>.value();

  /// 播放期期望的方向。
  ///
  /// B7c：原签名含 `required bool television`，并在 `television ||
  /// fullscreen && aspectRatio >= 1` 时锁横屏 —— TV 那一支随 TV 下线删除。
  /// 现只按「全屏 + 宽高比」判定（这是播放器全屏的既有语义）。
  static List<DeviceOrientation> orientations({
    bool fullscreen = false,
    double aspectRatio = 1,
  }) {
    if (fullscreen && aspectRatio >= 1) {
      return const [
        DeviceOrientation.landscapeLeft,
        DeviceOrientation.landscapeRight,
      ];
    }
    if (fullscreen) {
      return const [
        DeviceOrientation.portraitUp,
        DeviceOrientation.portraitDown,
      ];
    }
    return DeviceOrientation.values;
  }

  Future<void> setPlayback(
    Object owner, {
    required bool fullscreen,
    required double aspectRatio,
  }) async {
    final previousOwner = _playbackOwner;
    final previousOrientations = _playbackOrientations;
    _playbackOwner = owner;
    _playbackOrientations = orientations(
      fullscreen: fullscreen,
      aspectRatio: aspectRatio,
    );
    final requested = _playbackOrientations;
    try {
      await _apply();
    } catch (_) {
      if (!_disposed &&
          identical(_playbackOwner, owner) &&
          identical(_playbackOrientations, requested)) {
        _playbackOwner = previousOwner;
        _playbackOrientations = previousOrientations;
      }
      rethrow;
    }
  }

  Future<void> releasePlayback(Object owner) {
    if (!identical(_playbackOwner, owner)) return Future<void>.value();
    _playbackOwner = null;
    _playbackOrientations = DeviceOrientation.values;
    return _apply();
  }

  Future<void> refresh() => _apply(force: true);

  Future<void> _apply({bool force = false}) {
    final next = _pending.catchError((Object _) {}).then((_) async {
      if (_disposed ||
          kIsWeb ||
          (defaultTargetPlatform != TargetPlatform.android &&
              defaultTargetPlatform != TargetPlatform.iOS)) {
        return;
      }
      // B7c：原此处会向原生发 `setTelevisionMode`（TV 锁横屏），随 TV 下线删除。
      // 方向只由 `_playbackOrientations`（播放期）决定。
      if (_disposed) return;
      final preferred = _playbackOrientations;
      if (force || !listEquals(preferred, _appliedOrientations)) {
        await SystemChrome.setPreferredOrientations(preferred);
        if (!_disposed) _appliedOrientations = preferred;
      }
    });
    _pending = next;
    return next;
  }

  void dispose() {
    _disposed = true;
  }
}

class AppOrientationScope extends StatefulWidget {
  const AppOrientationScope({
    super.key,
    required this.child,
  });

  final Widget child;

  static AppOrientationController? maybeOf(BuildContext context) => context
      .dependOnInheritedWidgetOfExactType<_OrientationScope>()
      ?.controller;

  @override
  State<AppOrientationScope> createState() => _AppOrientationScopeState();
}

class _AppOrientationScopeState extends State<AppOrientationScope>
    with WidgetsBindingObserver {
  late final _controller = AppOrientationController();

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _apply(_controller.refresh());
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) _apply(_controller.refresh());
  }

  void _apply(Future<void> operation) {
    unawaited(
      operation.catchError((Object error, StackTrace stack) {
        FlutterError.reportError(
          FlutterErrorDetails(
            exception: error,
            stack: stack,
            library: 'app orientation',
          ),
        );
      }),
    );
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) =>
      _OrientationScope(controller: _controller, child: widget.child);
}

class _OrientationScope extends InheritedWidget {
  const _OrientationScope({required this.controller, required super.child});

  final AppOrientationController controller;

  @override
  bool updateShouldNotify(_OrientationScope oldWidget) =>
      controller != oldWidget.controller;
}
