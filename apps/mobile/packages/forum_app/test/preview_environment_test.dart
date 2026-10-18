import 'package:flutter_test/flutter_test.dart';
import 'package:forum_app/src/app_config.dart';
import 'package:forum_app/src/pages/schedule/scheduler_web_tip.dart';

void main() {
  test(
    'preview environment binds badges, session namespace and public links',
    () {
      AppConfig.validate();
      if (AppConfig.isDev) {
        expect(AppConfig.preview, isTrue);
        expect(AppConfig.sessionNamespace, 'https://dev.yourtj.de');
        expect(AppConfig.previewLabel, 'DEV aaaaaaa');
        expect(fullSchedulerUri.toString(), 'https://dev.yourtj.de/schedule');
      } else {
        expect(AppConfig.sessionNamespace, isEmpty);
        expect(fullSchedulerUri.toString(), 'https://f.yourtj.de/schedule');
      }
    },
  );
}
