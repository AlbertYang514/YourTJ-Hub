import 'dart:io';
import 'dart:typed_data';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:ui_kit/ui_kit.dart';
import '../helpers.dart';

void main() {
  testWidgets(
    'custom frame scale stays centered with unchanged avatar geometry',
    (tester) async {
      final bytes = File(
        '../../../../apps/gooseforum/resource/static/decorations/tea-garden-v1.png',
      ).readAsBytesSync();
      for (final scale in [
        0.5,
        1.4,
        2.0,
        double.nan,
        double.infinity,
        0.0,
        2.1,
      ]) {
        await tester.pumpWidget(
          gfApp(
            GfMediaScope(
              identity: 'frame-scale-$scale',
              factory:
                  (
                    url, {
                    width,
                    height,
                    policy = ResizeImagePolicy.exact,
                    allowedOrigins,
                  }) => MemoryImage(bytes),
              child: GfAvatar(
                src: '',
                size: 40,
                frameScale: scale,
                frameAssetUrl:
                    'https://forum.test/static/decorations/tea-garden-v1.png',
              ),
            ),
          ),
        );
        await tester.runAsync(
          () => Future<void>.delayed(const Duration(milliseconds: 50)),
        );
        await tester.pumpAndSettle();
        final expectedScale = scale.isFinite && scale >= 0.5 && scale <= 2
            ? scale
            : 1.25;
        expect(tester.getSize(find.byType(GfAvatar)), const Size(40, 40));
        final frame = tester.getRect(find.byType(Image));
        final avatar = tester.getRect(find.byType(GfAvatar));
        expect(frame.size, Size(40 * expectedScale, 40 * expectedScale));
        expect(frame.center, avatar.center);
        expect(tester.takeException(), isNull);
      }
    },
  );
  testWidgets(
    'static image frame paints at 1.25 size with badges above and unchanged geometry',
    (tester) async {
      final bytes = File(
        '../../../../apps/gooseforum/resource/static/decorations/tea-garden-v1.png',
      ).readAsBytesSync();
      final loads = <String>[];
      await tester.pumpWidget(
        gfApp(
          GfMediaScope(
            identity: 'frame-render-test',
            factory:
                (
                  url, {
                  width,
                  height,
                  policy = ResizeImagePolicy.exact,
                  allowedOrigins,
                }) {
                  loads.add(url);
                  return ResizeImage(
                    MemoryImage(bytes),
                    width: width,
                    height: height,
                    policy: policy,
                  );
                },
            child: const GfAvatar(
              src: '',
              size: 40,
              frameColors: [Colors.orange, Colors.green],
              frameAssetUrl:
                  'https://forum.test/static/decorations/tea-garden-v1.png',
              badge: SizedBox(key: Key('frame-badge'), width: 8, height: 8),
            ),
          ),
        ),
      );
      await tester.runAsync(
        () => Future<void>.delayed(const Duration(milliseconds: 50)),
      );
      await tester.pumpAndSettle();
      expect(
        loads.single,
        'https://forum.test/static/decorations/tea-garden-v1.png',
      );
      expect(tester.getSize(find.byType(GfAvatar)), const Size(40, 40));
      expect(tester.getSize(find.byType(Image)), const Size(50, 50));
      expect(tester.widget<RawImage>(find.byType(RawImage)).image, isNotNull);
      final outer = tester.widget<Stack>(
        find
            .ancestor(
              of: find.byKey(const Key('frame-badge')),
              matching: find.byType(Stack),
            )
            .first,
      );
      expect(outer.children.last, isA<Positioned>());
      expect(tester.takeException(), isNull);
    },
  );
  testWidgets('colour frame draws a 3px ring outside an unchanged avatar', (
    tester,
  ) async {
    await tester.pumpWidget(
      gfApp(
        const GfAvatar(
          src: '',
          size: 40,
          frameColors: [Colors.orange, Colors.green],
        ),
      ),
    );
    final Finder avatar = find.byType(GfAvatar);
    final Finder ring = find.byWidgetPredicate(
      (widget) =>
          widget is Container &&
          (widget.decoration as BoxDecoration?)?.gradient != null,
    );
    final Finder portrait = find.byWidgetPredicate(
      (widget) =>
          widget is Container &&
          (widget.decoration as BoxDecoration?)?.gradient == null,
    );
    expect(ring, findsOneWidget);
    expect(portrait, findsOneWidget);
    final Rect box = tester.getRect(avatar);
    expect(box.size, const Size(40, 40));
    expect(tester.getRect(portrait).size, const Size(40, 40));
    // Web `.gf-avatar-frame` 现在是 `inset: 7.5%`：40px 头像 → 3px 环。
    expect(tester.getRect(ring).size, const Size(46, 46));
    expect(tester.getRect(ring).center, box.center);
    expect(tester.getRect(portrait).center, box.center);
    expect(tester.takeException(), isNull);
  });

  testWidgets('colour frame ring thickness scales with the avatar size', (
    tester,
  ) async {
    // 列表模式小头像（20/24px）与大头像（96px）必须保持同一比例，
    // 否则会出现「列表里环过粗、详情页环看不见」（issue #1246）。
    for (final double size in <double>[20, 24, 32, 40, 48, 96]) {
      await tester.pumpWidget(
        gfApp(
          GfAvatar(
            src: '',
            size: size,
            frameColors: const <Color>[Colors.orange, Colors.green],
          ),
        ),
      );
      await tester.pumpAndSettle();
      final Rect avatar = tester.getRect(find.byType(GfAvatar));
      final Rect ring = tester.getRect(
        find
            .byWidgetPredicate(
              (widget) =>
                  widget is Container &&
                  (widget.decoration as BoxDecoration?)?.gradient != null,
            )
            .first,
      );
      expect(avatar.size, Size(size, size));
      expect(ring.width, closeTo(size * 1.15, 0.01), reason: 'size=$size');
      expect(
        (ring.width - avatar.width) / 2,
        closeTo(size * 0.075, 0.01),
        reason: 'size=$size',
      );
      expect(ring.center, avatar.center, reason: 'size=$size');
    }
  });

  testWidgets('frame artwork never falls back to a gradient ring', (
    tester,
  ) async {
    final loads = <String>[];
    Future<void> render(String asset) async {
      await tester.pumpWidget(
        gfApp(
          GfMediaScope(
            identity: asset,
            factory:
                (
                  url, {
                  width,
                  height,
                  policy = ResizeImagePolicy.exact,
                  allowedOrigins,
                }) {
                  loads.add(url);
                  return MemoryImage(Uint8List.fromList([1, 2, 3]));
                },
            child: GfAvatar(
              src: '',
              size: 40,
              frameColors: const [Colors.orange, Colors.green],
              frameAssetUrl: asset,
              frameScale: 2,
            ),
          ),
        ),
      );
      await tester.runAsync(
        () => Future<void>.delayed(const Duration(milliseconds: 50)),
      );
      await tester.pumpAndSettle();
      expect(tester.getSize(find.byType(GfAvatar)), const Size(40, 40));
      expect(tester.takeException(), isNull);
    }

    Finder gradients() => find.byWidgetPredicate(
      (widget) =>
          widget is Container &&
          (widget.decoration as BoxDecoration?)?.gradient != null,
    );

    // The artwork itself fails to decode: the frame stays empty rather than
    // showing a colour ring.
    await tester.pumpWidget(
      gfApp(
        GfMediaScope(
          identity: 'trusted-frame',
          factory:
              (
                url, {
                width,
                height,
                policy = ResizeImagePolicy.exact,
                allowedOrigins,
              }) {
                loads.add(url);
                return MemoryImage(Uint8List.fromList([1, 2, 3]));
              },
          child: const GfAvatar(
            src: '',
            size: 40,
            frameColors: [Colors.orange, Colors.green],
            frameAssetUrl:
                'https://forum.test/static/decorations/tea-garden-v1.png',
            frameScale: 2,
          ),
        ),
      ),
    );
    expect(gradients(), findsNothing, reason: 'still loading the artwork');
    await tester.runAsync(
      () => Future<void>.delayed(const Duration(milliseconds: 50)),
    );
    await tester.pumpAndSettle();
    expect(loads, ['https://forum.test/static/decorations/tea-garden-v1.png']);
    expect(find.byType(Image), findsOneWidget);
    expect(gradients(), findsNothing, reason: 'artwork failed to decode');

    await render('https://outside.test/untrusted.png');
    expect(loads.length, 1);
    expect(find.byType(Image), findsNothing);
    expect(gradients(), findsOneWidget);
  });
  testWidgets(
    'decorations keep avatar geometry and plain accessible text fallback',
    (tester) async {
      for (final brightness in Brightness.values) {
        await tester.pumpWidget(
          gfApp(
            const Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                GfAvatar(
                  src: '',
                  size: 32,
                  frameColors: [Colors.red, Colors.blue],
                ),
                GfDecoratedName('Alice', colors: [Colors.red, Colors.blue]),
                GfDecoratedName('Plain'),
              ],
            ),
            brightness: brightness,
          ),
        );
        expect(tester.getSize(find.byType(GfAvatar)), const Size(32, 32));
        expect(find.text('Alice'), findsOneWidget);
        expect(find.text('Plain'), findsOneWidget);
        expect(find.byType(ShaderMask), findsOneWidget);
        expect(tester.takeException(), isNull);
      }
    },
  );
}
