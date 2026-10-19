import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forum_app/l10n/app_localizations.dart';
import 'package:forum_app/src/updates/preview_status.dart';

void main() {
  final now = DateTime.fromMillisecondsSinceEpoch(1000000);
  const sha = 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa';
  Map<String, dynamic> status() => {
    'schema': 1,
    'updatedAt': 999,
    'active': {
      'source': sha,
      'phase': 'testing',
      'until': 2000,
      'artifacts': {
        'dev': ['web', 'android'],
      },
    },
  };
  PreviewValidity classify(Object? data) => previewValidity(
    data,
    source: sha,
    purpose: 'dev',
    artifact: 'android',
    now: now,
  );

  test(
    'fixed SHA remains valid across rebuilds; superseded/ended previews are stale',
    () {
      expect(classify(status()), PreviewValidity.current);
      final changed = status();
      (changed['active'] as Map)['source'] = 'b' * 40;
      expect(classify(changed), PreviewValidity.stale);
      changed['active'] = null;
      expect(classify(changed), PreviewValidity.stale);
      for (final phase in [
        'paused',
        'waiting',
        'aborted',
        'closed',
        'cancelled',
      ]) {
        final ended = status();
        (ended['active'] as Map)['phase'] = phase;
        expect(classify(ended), PreviewValidity.stale);
      }
      final expired = status();
      (expired['active'] as Map)['until'] = 900;
      expect(classify(expired), PreviewValidity.stale);
    },
  );

  test('missing, malformed, old and future status never claims current', () {
    for (final data in [
      null,
      {},
      {'schema': 1},
      status()..['updatedAt'] = -100000,
      status()..['updatedAt'] = 999999,
    ]) {
      expect(classify(data), PreviewValidity.unknown);
    }
  });

  testWidgets(
    'offline previews show an actionable notice without blocking the app',
    (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          localizationsDelegates: AppLocalizations.localizationsDelegates,
          supportedLocales: AppLocalizations.supportedLocales,
          home: PreviewStatusHost(
            fetchStatus: () async => throw Exception('offline'),
            child: const Text('Forum remains available'),
          ),
        ),
      );
      await tester.pump();
      expect(find.text('Forum remains available'), findsOneWidget);
      expect(find.text('Preview status'), findsOneWidget);
      expect(find.textContaining('Cannot verify'), findsOneWidget);
      await tester.pumpWidget(const SizedBox());
    },
  );
}
