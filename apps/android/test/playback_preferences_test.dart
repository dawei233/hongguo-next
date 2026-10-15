import 'package:hongguo_next/playback_preferences.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('volume survives a round trip through serialization', () {
    const preferences = PlaybackPreferences(speed: 1.5, volume: 0.42);
    expect(PlaybackPreferences.fromJson(preferences.toJson()).volume, 0.42);
  });

  test('volume defaults to unrecorded so first launch keeps the phone volume', () {
    // 负值是哨兵：表示"从来没记录过"。启动流程据此决定沿用系统当前音量，
    // 而不是强行把用户的手机音量改成某个默认档。
    final preferences = PlaybackPreferences.fromJson(const <String, dynamic>{});
    expect(preferences.volume, lessThan(0));
  });

  test('toggling another setting does not clobber the recorded volume', () {
    // 这是真实存在的隐患：_preferences getter 若不带 volume，切换弹幕/倍速
    // 会把已存的音量覆盖回默认值。
    const saved = PlaybackPreferences(volume: 0.3);
    final toggled = saved.copyWith(danmaku: false, speed: 2);
    expect(toggled.volume, 0.3);
    expect(PlaybackPreferences.fromJson(toggled.toJson()).volume, 0.3);
  });

  test('out-of-range volume is clamped and invalid payloads still rejected', () {
    expect(
      PlaybackPreferences.fromJson(const <String, dynamic>{'volume': 5}).volume,
      1.0,
    );
    expect(
      PlaybackPreferences.fromJson(const <String, dynamic>{'volume': -5}).volume,
      lessThan(0),
    );
    expect(
      () => PlaybackPreferences.fromJson(const <String, dynamic>{'speed': 7}),
      throwsFormatException,
    );
  });
}
