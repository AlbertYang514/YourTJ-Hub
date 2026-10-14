import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:ui_kit/ui_kit.dart';

void main() {
  group('Twemoji sequence matching', () {
    test('matches complete mixed-language and composed emoji sequences', () {
      const text = '中文 A😀 🐦‍🔥 🇺🇸 1️⃣ 👩🏽‍💻!';
      final segments = parseTwemojiText(text);
      final emoji = segments
          .where((segment) => segment.isEmoji)
          .map((segment) => segment.value)
          .toList();

      expect(emoji, <String>['😀', '🐦‍🔥', '🇺🇸', '1️⃣', '👩🏽‍💻']);
      expect(segments.map((segment) => segment.value).join(), text);
      expect(twemojiVersion, '17.0.3');
    });

    test(
      'matches optional variation selectors without changing source text',
      () {
        expect(parseTwemojiText('❤\uFE0F'), <TwemojiSegment>[
          const TwemojiSegment.emoji('❤\uFE0F', '2764.png'),
        ]);
      },
    );

    testWidgets('fast candidate scan keeps symbol, flag and keycap artwork', (
      tester,
    ) async {
      await tester.pumpWidget(
        const MaterialApp(
          home: Scaffold(body: GfText('© ® ™ ❤️ 🐦‍🔥 🇺🇸 1️⃣ #️⃣')),
        ),
      );
      await tester.pump();

      expect(find.byType(Image), findsNWidgets(8));
      expect(find.bySemanticsLabel('🐦‍🔥'), findsOneWidget);
      expect(find.bySemanticsLabel('🇺🇸'), findsOneWidget);
    });

    testWidgets('ordinary digits are not treated as keycap Emoji', (tester) async {
      await tester.pumpWidget(
        const MaterialApp(home: Scaffold(body: GfText('2026 and 514'))),
      );
      await tester.pump();
      expect(find.byType(Image), findsNothing);
      expect(find.text('2026 and 514'), findsOneWidget);
    });

    test('keeps unsupported joined sequences together as original text', () {
      expect(parseTwemojiText('🐦‍🛹'), <TwemojiSegment>[
        const TwemojiSegment.text('🐦‍🛹'),
      ]);
    });

    test(
      'copy maps an inline image placeholder to its complete source sequence',
      () {
        const span = TextSpan(text: '前文 🐦‍🔥 后文');
        // "前文 " occupies three UTF-16 units; the image is one visual unit.
        expect(twemojiCopyTextForDisplayRange(span, 3, 4), '🐦‍🔥');
        expect(twemojiCopyTextForDisplayRange(span, 0, 4), '前文 🐦‍🔥');
      },
    );
  });

  testWidgets('GfText paints one accessible inline image per full sequence', (
    tester,
  ) async {
    final semantics = tester.ensureSemantics();
    await tester.pumpWidget(
      const MaterialApp(home: Scaffold(body: GfText('中文 🐦‍🔥!'))),
    );
    await tester.pump();

    expect(find.byType(Image), findsOneWidget);
    final image = tester.widget<Image>(find.byType(Image));
    final resizedImage = image.image as ResizeImage;
    final assetImage = resizedImage.imageProvider as AssetImage;
    expect(assetImage.assetName, contains('1f426-200d-1f525.png'));
    expect(assetImage.package, 'ui_kit');
    expect(resizedImage.width, 72);
    expect(resizedImage.height, 72);
    expect(find.bySemanticsLabel('🐦‍🔥'), findsOneWidget);
    semantics.dispose();
  });

  testWidgets('decorated names keep Twemoji artwork colors', (tester) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: Column(
            children: [
              GfDecoratedName(
                '昵称 🐦‍🔥',
                colors: <Color>[Colors.red, Colors.blue],
              ),
              GfDecoratedName(
                '渐变昵称',
                colors: <Color>[Colors.red, Colors.blue],
              ),
            ],
          ),
        ),
      ),
    );
    await tester.pump();

    expect(find.byType(Image), findsOneWidget);
    expect(find.bySemanticsLabel('🐦‍🔥'), findsOneWidget);
    expect(find.byType(ShaderMask), findsOneWidget);
    final emojiElement = tester.element(find.byType(Image));
    expect(emojiElement.findAncestorWidgetOfExactType<ShaderMask>(), isNull);
  });

  testWidgets('selectable Emoji copy restores its complete source sequence', (
    tester,
  ) async {
    String? copied;
    tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
      SystemChannels.platform,
      (call) async {
        if (call.method == 'Clipboard.setData') {
          copied = (call.arguments as Map)['text'] as String?;
        }
        return null;
      },
    );
    addTearDown(
      () => tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
        SystemChannels.platform,
        null,
      ),
    );

    await tester.pumpWidget(
      const MaterialApp(home: Scaffold(body: GfSelectableText('中文 A🐦‍🔥B'))),
    );
    await tester.pump();

    final textContext = tester.element(find.byType(RichText));
    Actions.invoke(
      textContext,
      const SelectAllTextIntent(SelectionChangedCause.keyboard),
    );
    await tester.pump();
    Actions.invoke(textContext, CopySelectionTextIntent.copy);
    await tester.pump();

    expect(copied, '中文 A🐦‍🔥B');
  });
}
