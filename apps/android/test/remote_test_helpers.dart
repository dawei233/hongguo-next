import 'package:hongguo_next/app_layout.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

/// 把遥控焦点放到 [finder] 指向的可聚焦控件上。
///
/// [finder] 既可以指向 RemoteTarget/RemoteButton 这类焦点宿主本身
/// （如 `find.byKey`），也可以指向它内部的任意节点（如 `find.text`）：
/// 先找后代里的第一个 FocusableActionDetector，找不到再向上找包裹它的那个，
/// 这样两种写法都能命中同一个遥控焦点。
void focusRemote(WidgetTester tester, Finder finder) {
  final detector = _remoteDetector(tester, finder);
  final container = find
      .descendant(of: detector, matching: find.byType(AnimatedContainer))
      .first;
  Focus.of(tester.element(container)).requestFocus();
}

Finder _remoteDetector(WidgetTester tester, Finder finder) {
  final descendant = find.descendant(
    of: finder,
    matching: find.byType(FocusableActionDetector),
  );
  if (descendant.evaluate().isNotEmpty) return descendant.first;
  return find
      .ancestor(of: finder, matching: find.byType(FocusableActionDetector))
      .first;
}

Widget televisionHost({required Widget child}) => MaterialApp(
  theme: televisionTheme(ThemeData.dark()),
  builder: (_, child) => AppLayout(
    television: true,
    child: Shortcuts(
      shortcuts: const {
        SingleActivator(LogicalKeyboardKey.select): ActivateIntent(),
        SingleActivator(LogicalKeyboardKey.goBack): DismissIntent(),
      },
      child: FocusTraversalGroup(child: child!),
    ),
  ),
  home: child,
);
