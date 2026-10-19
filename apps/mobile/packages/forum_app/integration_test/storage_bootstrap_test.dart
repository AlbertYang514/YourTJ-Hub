import 'dart:io';

import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:path_provider/path_provider.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:forum_app/src/app_config.dart';
import 'package:forum_app/src/storage/private_database.dart';
import 'package:forum_app/src/storage/storage_gate.dart';
import 'package:forum_app/src/storage/storage_providers.dart';

/// Run on an isolated simulator with the real dev preview dart-defines. This
/// exercises native backup exclusion, Keychain and encrypted SQLite without
/// contacting a server or substituting storage providers.
void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('dev storage startup and retry preserve production files', (
    tester,
  ) async {
    expect(Platform.isIOS, isTrue);
    AppConfig.validate();
    expect(AppConfig.isDev, isTrue);
    SharedPreferences.setPrefix('flutter.dev.');
    final support = await getApplicationSupportDirectory();
    final production = Directory.fromUri(
      support.uri.resolve('yourtj_private/'),
    );
    await production.create(recursive: true);
    final sentinel = File.fromUri(
      production.uri.resolve(
        'storage-bootstrap-test-${DateTime.now().microsecondsSinceEpoch}',
      ),
    );
    await sentinel.writeAsString(
      'preserve existing production data',
      flush: true,
    );
    addTearDown(sentinel.delete);
    await const MethodChannel(
      'yourtj/storage',
    ).invokeMethod<void>('excludeFromBackup', {'path': production.path});

    final container = ProviderContainer();
    addTearDown(container.dispose);
    for (var attempt = 0; attempt < 2; attempt++) {
      if (attempt > 0) container.invalidate(storageBootstrapProvider);
      await container.read(storageBootstrapProvider.future);
      expect(container.read(storageReadyProvider), isTrue);
      expect(
        (await privateStorageDirectory()).path,
        Directory.fromUri(support.uri.resolve('yourtj_private_dev/')).path,
      );
      expect(
        await sentinel.readAsString(),
        'preserve existing production data',
      );
    }
  });

  testWidgets('native backup exclusion rejects unmanaged paths', (
    tester,
  ) async {
    final support = await getApplicationSupportDirectory();
    for (final relative in [
      '.',
      'yourtj_private_dev/unrelated',
      'yourtj_private_dev_other',
      '../yourtj_private_dev',
    ]) {
      await expectLater(
        const MethodChannel('yourtj/storage').invokeMethod<void>(
          'excludeFromBackup',
          {'path': Directory.fromUri(support.uri.resolve(relative)).path},
        ),
        throwsA(
          isA<PlatformException>().having(
            (error) => error.code,
            'code',
            'invalid_path',
          ),
        ),
      );
    }
  });
}
