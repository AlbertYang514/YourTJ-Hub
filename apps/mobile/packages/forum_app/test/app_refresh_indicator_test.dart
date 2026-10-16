import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forum_app/l10n/app_localizations.dart';
import 'package:forum_app/src/widgets/app_refresh_indicator.dart';

void main() {
  for (final platform in [TargetPlatform.iOS, TargetPlatform.android]) {
    for (final empty in [false, true]) {
      testWidgets(
        '$platform short pull refreshes tall ${empty ? "empty" : "populated"} viewport only on release',
        (tester) async {
          await tester.binding.setSurfaceSize(const Size(390, 900));
          addTearDown(() => tester.binding.setSurfaceSize(null));
          var calls = 0;
          final done = Completer<void>();
          await tester.pumpWidget(
            MaterialApp(
              localizationsDelegates: AppLocalizations.localizationsDelegates,
              supportedLocales: AppLocalizations.supportedLocales,
              theme: ThemeData(platform: platform),
              home: Scaffold(
                body: AppRefreshIndicator(
                  edgeOffset: 100,
                  onRefresh: () {
                    calls++;
                    return done.future;
                  },
                  child: ListView(
                    physics: const AlwaysScrollableScrollPhysics(),
                    children: [SizedBox(height: empty ? 100 : 1800)],
                  ),
                ),
              ),
            ),
          );
          final gesture = await tester.startGesture(const Offset(180, 250));
          await gesture.moveBy(const Offset(0, 20));
          await tester.pump();
          await gesture.moveBy(const Offset(0, 85));
          await tester.pump();
          expect(calls, 0);
          await gesture.up();
          for (var frame = 0; frame < 45; frame++) {
            await tester.pump(const Duration(milliseconds: 16));
          }
          expect(calls, 1);
          expect(
            tester
                .getTopLeft(find.byKey(const Key('app-refresh-indicator-logo')))
                .dy,
            greaterThanOrEqualTo(132),
          );
          final pill = tester.getRect(
            find.byKey(const Key('app-refresh-indicator-pill')),
          );
          final list = tester.getRect(find.byType(ListView));
          expect(pill.width, lessThan(list.width));
          expect(pill.center.dx, closeTo(list.center.dx, 1));
          expect(find.text('Loading…'), findsOneWidget);
          final rotation = find.byKey(
            const Key('app-refresh-indicator-rotation'),
          );
          final turns = tester.widget<RotationTransition>(rotation).turns;
          final angle = turns.value;
          await tester.pump(const Duration(milliseconds: 500));
          expect(turns.value, isNot(angle));
          await tester.drag(find.byType(ListView), const Offset(0, 220));
          await tester.pump(const Duration(milliseconds: 300));
          expect(calls, 1);
          done.complete();
          await tester.pumpAndSettle();
          expect(
            find.byKey(const Key('app-refresh-indicator-logo')),
            findsNothing,
          );
        },
      );
    }
    for (final scenario in ['small pull', 'retracted pull', 'middle of list']) {
      testWidgets('$platform ignores $scenario', (tester) async {
        final controller = ScrollController(
          initialScrollOffset: scenario == 'middle of list' ? 400 : 0,
        );
        addTearDown(controller.dispose);
        var calls = 0;
        await tester.pumpWidget(
          MaterialApp(
            localizationsDelegates: AppLocalizations.localizationsDelegates,
            supportedLocales: AppLocalizations.supportedLocales,
            theme: ThemeData(platform: platform),
            home: Scaffold(
              body: AppRefreshIndicator(
                onRefresh: () async {
                  calls++;
                },
                child: ListView(
                  controller: controller,
                  physics: const AlwaysScrollableScrollPhysics(),
                  children: const [SizedBox(height: 1800)],
                ),
              ),
            ),
          ),
        );
        final gesture = await tester.startGesture(const Offset(180, 250));
        await gesture.moveBy(const Offset(0, 20));
        await tester.pump();
        await gesture.moveBy(Offset(0, scenario == 'small pull' ? 20 : 85));
        await tester.pump();
        if (scenario == 'retracted pull') {
          await gesture.moveBy(const Offset(0, -80));
          await tester.pump();
        }
        await gesture.up();
        await tester.pumpAndSettle();
        expect(calls, 0);
      });
    }
  }

  testWidgets('reduced motion keeps a static branded refresh status', (
    tester,
  ) async {
    final done = Completer<void>();
    await tester.pumpWidget(
      MaterialApp(
        localizationsDelegates: AppLocalizations.localizationsDelegates,
        supportedLocales: AppLocalizations.supportedLocales,
        theme: ThemeData.dark(),
        home: Builder(
          builder: (context) => MediaQuery(
            data: MediaQuery.of(context).copyWith(disableAnimations: true),
            child: Scaffold(
              body: AppRefreshIndicator(
                edgeOffset: 48,
                onRefresh: () => done.future,
                child: ListView(
                  physics: const AlwaysScrollableScrollPhysics(),
                  children: const [SizedBox(height: 1800)],
                ),
              ),
            ),
          ),
        ),
      ),
    );

    final gesture = await tester.startGesture(const Offset(180, 250));
    await gesture.moveBy(const Offset(0, 20));
    await tester.pump();
    await gesture.moveBy(const Offset(0, 85));
    await tester.pump();
    await gesture.up();
    await tester.pump(const Duration(milliseconds: 300));

    final logo = find.byKey(const Key('app-refresh-indicator-logo'));
    expect(logo, findsOneWidget);
    expect(find.text('Loading…'), findsOneWidget);
    final rotation = find.byKey(const Key('app-refresh-indicator-rotation'));
    expect(rotation, findsOneWidget);
    final turns = tester.widget<RotationTransition>(rotation).turns;
    final before = turns.value;
    await tester.pump(const Duration(seconds: 2));
    expect(turns.value, before);
    expect(tester.getTopLeft(logo).dy, greaterThanOrEqualTo(80));
    done.complete();
    await tester.pumpAndSettle();
  });
}
