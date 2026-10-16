import 'package:hongguo_next/local_store.dart';
import 'package:hongguo_next/main.dart';
import 'package:hongguo_next/models.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:media_kit/media_kit.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'fixtures.dart';
import 'player_fixtures.dart';

void main() {
  testWidgets('debug switcher behavior with gate locked vs off', (tester) async {
    for (final gateOff in [false, true]) {
      tester.view.physicalSize = const Size(960, 540);
      tester.view.devicePixelRatio = 1;
      SharedPreferences.setMockInitialValues({
        if (gateOff) 'sourceGateOff': true,
      });
      final store = LocalStore(await SharedPreferences.getInstance());
      final repository = FixtureRepository();
      await tester.pumpWidget(
        DuanjuApp(
          repository: repository,
          store: store,
          television: true,
          playerFactory: () => Player(platformPlayer: ScriptedPlayer()),
          videoBuilder: (controls) => controls,
        ),
      );
      await tester.pumpAndSettle();
      // ignore: avoid_print
      print('gateOff=$gateOff store.sources=${store.sources.map((s) => s.id)}');
      final switcher = find.byKey(const ValueKey('source-switch'));
      final title = find
          .descendant(of: switcher, matching: find.byType(Text))
          .first;
      Focus.of(tester.element(title)).requestFocus();
      await tester.pumpAndSettle();
      // 打开弹窗
      await tester.sendKeyEvent(LogicalKeyboardKey.select);
      await tester.pumpAndSettle();
      final focusNode = FocusManager.instance.primaryFocus;
      // ignore: avoid_print
      print(
        'gateOff=$gateOff focusNode=${focusNode?.context?.widget.runtimeType} '
        'hasFocus=${focusNode?.hasFocus} canRequestFocus=${focusNode?.canRequestFocus}',
      );
      // 列出弹窗条目文本（弹窗路由覆盖层里的 Text）
      final texts = find
          .byType(Text)
          .evaluate()
          .map((e) => (e.widget as Text).data)
          .where((t) => t != null)
          .toList();
      // ignore: avoid_print
      print('gateOff=$gateOff popupTexts=$texts');
      for (var i = 1; i <= 3; i++) {
        await tester.sendKeyEvent(LogicalKeyboardKey.arrowDown);
        await tester.pumpAndSettle();
        final fn = FocusManager.instance.primaryFocus;
        String? label;
        fn?.context?.visitAncestorElements((e) {
          if (e.widget is Text) {
            label = (e.widget as Text).data;
            return false;
          }
          return true;
        });
        // ignore: avoid_print
        print(
          'gateOff=$gateOff afterDown$i label=$label '
          'popupOpen=${find.text('全部站源').evaluate().length}',
        );
      }
      await tester.sendKeyEvent(LogicalKeyboardKey.select);
      await tester.pumpAndSettle();
      // ignore: avoid_print
      print(
        'gateOff=$gateOff afterSelect popupOpen=${find.text('全部站源').evaluate().length} '
        'requests=${repository.requests} store.source=${store.source}',
      );
      await tester.pumpWidget(const SizedBox.shrink());
      store.dispose();
    }
  });
}
