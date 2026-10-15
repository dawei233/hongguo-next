import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/gestures.dart';
import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';
import 'package:media_kit/media_kit.dart';

import 'app_layout.dart';
import 'widgets.dart';

enum SwipeAction { none, brightness, volume, episode }

class GestureHudState {
  const GestureHudState({
    this.type = SwipeAction.none,
    this.value = 0.0,
  });
  final SwipeAction type;
  final double value; // 0.0 ~ 1.0
}

class PlayerInteractions extends ChangeNotifier {
  PlayerInteractions({
    required this.player,
    required this.available,
    required this.baseSpeed,
    required this.onTogglePlayback,
    required this.onFullscreen,
    required this.onEpisode,
    this.onSeek,
    this.onVolumeChanged,
  }) {
    _playing = player.stream.playing.listen((playing) {
      if (!playing) cancel();
    });
    AppDevice.getBrightness().then((val) {
      if (!_disposed) {
        _brightness = val;
        notifyListeners();
      }
    }).catchError((_) {});
    AppDevice.getVolume().then((val) {
      // 构造期间的异步读取可能晚于外部恢复的已存音量；一旦有显式写入就以它为准，
      // 否则会把刚恢复的音量盖回系统旧值。
      if (!_disposed && !_volumePinned) {
        _volume = val;
        notifyListeners();
      }
    }).catchError((_) {});
  }

  final Player player;
  final bool Function() available;
  final double Function() baseSpeed;
  final VoidCallback onTogglePlayback;
  final VoidCallback onFullscreen;
  final String Function(int direction) onEpisode;
  final Future<void> Function(Duration)? onSeek;
  /// 音量变化后的回调，用于把新音量持久化。参数是 0.0~1.0 的系统媒体音量。
  final Future<void> Function(double volume)? onVolumeChanged;
  late final StreamSubscription<bool> _playing;
  Timer? _holdTimer;
  Timer? _hintTimer;
  Future<void> _rates = Future<void>.value();
  final Set<int> _pointers = {};
  int? _pointer;
  Offset? _origin;
  Offset? _lastPosition;
  Duration _started = Duration.zero;
  double _swipeThreshold = 70;
  bool _swipeEnabled = false;
  bool _moved = false;
  bool _held = false;
  bool _boosting = false;
  bool _keyboardHold = false;
  bool _cancelUntilRelease = false;
  bool _disposed = false;
  double _unmutedVolume = 1.0;
  String _feedback = '';
  DateTime _ignoreTapUntil = DateTime(2000);
  SwipeAction _swipeAction = SwipeAction.none;
  GestureHudState _hudState = const GestureHudState();
  double _brightness = 0.5;
  double _initialBrightness = 0.5;
  double _volume = 1.0;
  double _initialVolume = 1.0;
  bool _volumePinned = false;
  int _volumeWrite = 0;
  Timer? _persistTimer;
  bool _sideLocked = false;
  double _sideTrigger = 30;
  double _viewWidth = 0.0;
  double _viewHeight = 0.0;
  Timer? _hudTimer;

  GestureHudState get hudState => _hudState;
  double get brightness => _brightness;
  bool get isBrightnessActive => _hudState.type == SwipeAction.brightness;

  /// 系统媒体音量，0.0~1.0。控件与手势都以它为准，不再读播放器的软件增益 ——
  /// media_kit 的 setVolume 只缩放内部增益，音量上限会被系统音量锁死。
  double get volume => _volume;

  void setBrightnessDirect(double value) {
    if (_disposed) return;
    _brightness = value.clamp(0.01, 1.0);
    AppDevice.setBrightness(_brightness);
    _showHud(SwipeAction.brightness, _brightness);
  }

  /// 只同步控件显示的系统音量，不回写系统。用于"用户从未在本应用调过音量"的场景：
  /// 打开播放器不应该顺手改掉手机的音量档位。
  void adoptVolume(double value) {
    if (_disposed) return;
    _volumePinned = true;
    _volume = value.clamp(0.0, 1.0);
    notifyListeners();
  }

  /// 音量唯一的写入出口：写系统媒体音量，并把实际生效值回填给控件。
  /// 所有调整入口（手势、滑块、静音键、快捷键）都必须走这里，避免各自为政。
  void setVolumeDirect(double value) {
    if (_disposed) return;
    _volumePinned = true;
    final requested = value.clamp(0.0, 1.0);
    _volume = requested;
    notifyListeners();
    final ticket = ++_volumeWrite;
    unawaited(
      AppDevice.setVolume(requested).then((applied) {
        // 拖动会连续写入，系统响应可能乱序回来；只认最后一次请求的回填，
        // 否则控件数值会被过期的响应拽回去。
        if (_disposed || ticket != _volumeWrite) return;
        // 系统按档位量化，回填实际值，避免显示与听感不一致。
        _volume = applied;
        notifyListeners();
        _schedulePersist(applied);
      }),
    );
  }

  /// 拖动过程中每帧都会写系统音量，但没必要每帧都落盘。
  /// 停止调整后再持久化，避免高频写存储。
  void _schedulePersist(double volume) {
    _persistTimer?.cancel();
    _persistTimer = Timer(const Duration(milliseconds: 400), () {
      _persistTimer = null;
      if (_disposed) return;
      unawaited(onVolumeChanged?.call(_volume) ?? Future<void>.value());
    });
  }

  /// 立即持久化（用于收尾），跳过防抖等待。
  void flushVolume() {
    if (_disposed || _persistTimer == null) return;
    _persistTimer!.cancel();
    _persistTimer = null;
    unawaited(onVolumeChanged?.call(_volume) ?? Future<void>.value());
  }

  /// 用增量调整音量（键盘/遥控的 ±），并给出文字反馈。
  void changeVolume(double delta) {
    if (!available()) return;
    final target = (_volume + delta / 100.0).clamp(0.0, 1.0);
    setVolumeDirect(target);
    if (target > 0) _unmutedVolume = target;
    hint(target == 0 ? '已静音' : '音量 ${(target * 100).round()}%');
  }

  void toggleMute() {
    if (!available()) return;
    final current = _volume;
    if (current > 0) _unmutedVolume = current;
    final target = current > 0 ? 0.0 : _unmutedVolume;
    setVolumeDirect(target);
    hint(target == 0 ? '已静音' : '音量 ${(target * 100).round()}%');
  }

  void dismissBrightnessHud() {
    _scheduleDismissHud();
  }
  String get feedback => _feedback;
  bool get boosting => _boosting;
  bool get suppressTap => DateTime.now().isBefore(_ignoreTapUntil);
  Future<void> get pendingRates => _rates;

  void hint(String message, {bool persistent = false}) {
    if (_disposed) return;
    _hintTimer?.cancel();
    if (_feedback != message) {
      _feedback = message;
      notifyListeners();
    }
    if (!persistent && message.isNotEmpty) {
      _hintTimer = Timer(const Duration(milliseconds: 1200), () {
        hint(_boosting ? '3 倍速 · 松开恢复' : '', persistent: true);
      });
    }
  }

  Future<void> applySpeed() => _setRate(baseSpeed());

  Future<void> _setRate(double value) {
    _rates = _rates
        .catchError((Object _) {})
        .then((_) => player.setRate(value));
    unawaited(
      _rates.catchError((Object _) {
        hint('倍速调整失败，请重试');
      }),
    );
    return _rates;
  }

  void _beginHold({bool keyboard = false}) {
    if (!available() || _holdTimer != null || _boosting) return;
    _keyboardHold = keyboard;
    _holdTimer = Timer(const Duration(milliseconds: 350), () {
      _holdTimer = null;
      if (_disposed ||
          !available() ||
          !player.state.playing ||
          player.state.completed) {
        return;
      }
      _boosting = true;
      _held = true;
      unawaited(_setRate(3));
      hint('3 倍速 · 松开恢复', persistent: true);
    });
  }

  void _endHold({bool tap = false, bool silent = false}) {
    final wasKeyboard = _keyboardHold;
    final boosted = _boosting;
    _holdTimer?.cancel();
    _holdTimer = null;
    _keyboardHold = false;
    _boosting = false;
    if (boosted) {
      unawaited(_setRate(baseSpeed()));
      if (!silent) hint('恢复 ${baseSpeed()} 倍速');
    } else if (tap && wasKeyboard) {
      seek(5);
    }
  }

  void cancel() {
    if (_disposed) return;
    if (_pointers.isNotEmpty) {
      _cancelUntilRelease = true;
      _ignoreTapUntil = DateTime.now().add(const Duration(milliseconds: 600));
    }
    _pointer = null;
    _origin = null;
    _lastPosition = null;
    _sideLocked = false;
    _swipeAction = SwipeAction.none;
    _endHold(silent: true);
    hint('');
  }

  void pointerDown(
    PointerDownEvent event, {
    required bool swipeEnabled,
    double width = 0.0,
    required double height,
  }) {
    _pointers.add(event.pointer);
    if (_pointers.length != 1 || _cancelUntilRelease) {
      cancel();
      return;
    }
    if (!available() || event.buttons != kPrimaryButton) return;
    _pointer = event.pointer;
    _origin = _lastPosition = event.localPosition;
    _started = event.timeStamp;
    _swipeEnabled = swipeEnabled && event.kind == PointerDeviceKind.touch;
    _swipeThreshold = math.max(30, math.min(80, height * .08));
    _sideTrigger = math.max(18, math.min(48, height * .05));
    _viewWidth = width;
    _viewHeight = height;
    _moved = _held = false;
    _sideLocked = false;
    _swipeAction = SwipeAction.none;

    if (_swipeEnabled && width > 0) {
      // 落点只决定候选动作，不立即触发：
      // 左侧 20% -> 亮度，右侧 20% -> 音量，其余 -> 切集。
      // 亮度/音量必须再满足「纵向位移占优且超过阈值」才锁定生效，避免一碰就误触。
      if (event.localPosition.dx < width * 0.2) {
        _swipeAction = SwipeAction.brightness;
        _initialBrightness = _brightness;
        AppDevice.getBrightness().then((val) {
          if (_pointer == event.pointer && !_sideLocked) {
            _initialBrightness = _brightness = val;
          }
        });
      } else if (event.localPosition.dx > width * 0.8) {
        _swipeAction = SwipeAction.volume;
        _initialVolume = _volume;
      } else {
        _swipeAction = SwipeAction.episode;
      }
    }
    _beginHold();
  }

  void pointerMove(PointerMoveEvent event) {
    if (_pointer != event.pointer || _origin == null) return;
    _lastPosition = event.localPosition;
    final diff = event.localPosition - _origin!;
    if (diff.distance > 12) {
      _moved = true;
      _endHold();
    }
    if (!_swipeEnabled || !_moved || _viewHeight <= 0) return;
    if (_swipeAction == SwipeAction.episode) return;

    if (!_sideLocked) {
      // 纵向位移必须占优且超过阈值，才把候选动作锁定为亮度/音量。
      if (diff.dy.abs() < _sideTrigger || diff.dy.abs() <= diff.dx.abs()) return;
      _sideLocked = true;
    }

    final dy = _origin!.dy - event.localPosition.dy; // 向上滑动为增加，向下滑动为减少
    final deltaRatio = dy / (_viewHeight * 0.6);

    if (_swipeAction == SwipeAction.brightness) {
      _brightness = (_initialBrightness + deltaRatio).clamp(0.01, 1.0);
      AppDevice.setBrightness(_brightness);
      _showHud(SwipeAction.brightness, _brightness);
    } else if (_swipeAction == SwipeAction.volume) {
      final volume = (_initialVolume + deltaRatio).clamp(0.0, 1.0);
      setVolumeDirect(volume);
      _showHud(SwipeAction.volume, volume);
    }
  }

  void pointerUp(PointerUpEvent event) {
    _pointers.remove(event.pointer);
    if (_cancelUntilRelease) {
      _ignoreTapUntil = DateTime.now().add(const Duration(milliseconds: 600));
      if (_pointers.isEmpty) _cancelUntilRelease = false;
      return;
    }
    if (_pointer != event.pointer || _origin == null) return;
    final delta = (_lastPosition ?? event.localPosition) - _origin!;
    final isVertical = delta.dy.abs() >= _swipeThreshold && delta.dy.abs() > delta.dx.abs() * 1.5;

    if (_swipeAction == SwipeAction.episode &&
        _swipeEnabled &&
        !_held &&
        _moved &&
        isVertical &&
        event.timeStamp - _started < const Duration(milliseconds: 1500)) {
      if (available()) hint(onEpisode(delta.dy < 0 ? 1 : -1));
    } else if (_swipeAction == SwipeAction.brightness ||
        _swipeAction == SwipeAction.volume) {
      // 滑动结束立即落盘，不等防抖窗口。
      if (_swipeAction == SwipeAction.volume) flushVolume();
      _scheduleDismissHud();
    }

    if (_moved || _held) {
      _ignoreTapUntil = DateTime.now().add(const Duration(milliseconds: 600));
    }
    _pointer = null;
    _origin = null;
    _endHold();
  }

  void pointerCancel(PointerCancelEvent event) {
    _pointers.remove(event.pointer);
    cancel();
    _ignoreTapUntil = DateTime.now().add(const Duration(milliseconds: 600));
    if (_pointers.isEmpty) _cancelUntilRelease = false;
  }

  void _showHud(SwipeAction action, double value) {
    if (_disposed) return;
    _hudTimer?.cancel();
    _hudState = GestureHudState(type: action, value: value);
    notifyListeners();
  }

  void _scheduleDismissHud() {
    _hudTimer?.cancel();
    _hudTimer = Timer(const Duration(milliseconds: 1000), () {
      if (_disposed) return;
      _hudState = const GestureHudState();
      notifyListeners();
    });
  }

  void seek(int seconds) {
    if (!available() || player.state.duration <= Duration.zero) return;
    _endHold();
    final target = (player.state.position.inMilliseconds + seconds * 1000)
        .clamp(0, player.state.duration.inMilliseconds);
    unawaited((onSeek ?? player.seek)(Duration(milliseconds: target)));
    hint('${seconds > 0 ? '快进至' : '后退至'} ${formatPosition(target / 1000)}');
  }

  KeyEventResult key(KeyEvent event) {
    final key = event.logicalKey;
    if (event is KeyUpEvent) {
      if (key == LogicalKeyboardKey.arrowRight && _keyboardHold) {
        _endHold(tap: available());
        return KeyEventResult.handled;
      }
      return KeyEventResult.ignored;
    }
    final hardware = HardwareKeyboard.instance;
    if (hardware.isAltPressed ||
        hardware.isMetaPressed ||
        hardware.isShiftPressed) {
      cancel();
      return KeyEventResult.ignored;
    }
    if (key == LogicalKeyboardKey.f11 || key == LogicalKeyboardKey.keyF) {
      if (event is KeyDownEvent) {
        cancel();
        onFullscreen();
      }
      return KeyEventResult.handled;
    }
    if (hardware.isControlPressed || !available()) {
      return KeyEventResult.ignored;
    }
    if (key != LogicalKeyboardKey.arrowRight) _endHold();
    if (key == LogicalKeyboardKey.arrowRight) {
      if (event is KeyDownEvent) _beginHold(keyboard: true);
    } else if (key == LogicalKeyboardKey.arrowLeft) {
      seek(-5);
    } else if (key == LogicalKeyboardKey.arrowUp) {
      changeVolume(5);
    } else if (key == LogicalKeyboardKey.arrowDown) {
      changeVolume(-5);
    } else if (key == LogicalKeyboardKey.space ||
        key == LogicalKeyboardKey.mediaPlayPause) {
      if (event is KeyDownEvent) onTogglePlayback();
    } else if (key == LogicalKeyboardKey.keyM) {
      if (event is KeyDownEvent) toggleMute();
    } else if (key == LogicalKeyboardKey.mediaTrackNext ||
        key == LogicalKeyboardKey.mediaTrackPrevious) {
      if (event is KeyDownEvent) {
        hint(onEpisode(key == LogicalKeyboardKey.mediaTrackNext ? 1 : -1));
      }
    } else {
      return KeyEventResult.ignored;
    }
    return KeyEventResult.handled;
  }

  @override
  void dispose() {
    // 先把还在防抖窗口里的音量落盘，再置 _disposed —— 顺序反了 flushVolume
    // 会直接返回，用户最后一次调整就丢了。
    if (_persistTimer != null) {
      _persistTimer!.cancel();
      _persistTimer = null;
      unawaited(onVolumeChanged?.call(_volume) ?? Future<void>.value());
    }
    _disposed = true;
    _holdTimer?.cancel();
    _hintTimer?.cancel();
    _hudTimer?.cancel();
    AppDevice.resetBrightness(); // 离开播放器时自动恢复手机/平板系统默认亮度
    if (_boosting) unawaited(_setRate(baseSpeed()));
    _boosting = false;
    _playing.cancel();
    _pointers.clear();
    super.dispose();
  }
}
