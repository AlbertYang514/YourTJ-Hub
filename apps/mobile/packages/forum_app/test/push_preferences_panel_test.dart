import 'package:core/core.dart';
import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forum_app/l10n/app_localizations.dart';
import 'package:forum_app/src/providers.dart';
import 'package:forum_app/src/push/push_preferences_panel.dart';

class PreferencesRepository extends PushRepository {
  PreferencesRepository()
    : super(GfApiClient(dio: Dio(), tokenStorage: _Storage()));
  PushPreferences value = const PushPreferences(chat: false, activity: true);
  final updates = <Map<String, bool>>[];
  @override
  Future<PushRepository> forSession() async => this;
  @override
  Future<PushPreferences> preferences() async => value;
  @override
  Future<PushPreferences> updatePreferences(Map<String, bool> changes) async {
    updates.add(changes);
    value = PushPreferences.fromJson({...value.toJson(), ...changes});
    return value;
  }
}

class _Storage implements TokenStorage {
  @override
  Future<String?> read() async => 'session';
  @override
  Future<void> write(String token) async {}
  @override
  Future<void> clear() async {}
}

void main() {
  testWidgets('push switches use server preferences and update one group', (
    tester,
  ) async {
    final repository = PreferencesRepository();
    final container = ProviderContainer(
      overrides: [pushRepositoryProvider.overrideWithValue(repository)],
    );
    addTearDown(container.dispose);
    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: MaterialApp(
          localizationsDelegates: AppLocalizations.localizationsDelegates,
          supportedLocales: AppLocalizations.supportedLocales,
          home: const Scaffold(body: PushPreferencesPanel()),
        ),
      ),
    );
    await tester.pumpAndSettle();
    List<bool> states() => tester
        .widgetList<Switch>(find.byType(Switch))
        .map((switchWidget) => switchWidget.value)
        .toList();
    expect(states(), [false, true, true, false, true]);
    await tester.tap(find.byType(Switch).first);
    await tester.pumpAndSettle();
    expect(repository.updates, [
      {'chat': true},
    ]);
    expect(states(), [true, true, true, false, true]);
    repository.value = const PushPreferences();
    container.read(offlineCacheEpochProvider.notifier).invalidate();
    await tester.pumpAndSettle();
    expect(states(), [true, true, false, false, true]);
    expect(tester.takeException(), isNull);
  });
}
