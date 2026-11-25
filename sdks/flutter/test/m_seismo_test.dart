import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:m_seismo/m_seismo.dart';

import 'fake_m_seismo_host_api.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late FakeMSeismoHostApi host;
  late MSeismo mSeismo;

  setUp(() {
    debugDefaultTargetPlatformOverride = TargetPlatform.iOS;
    host = FakeMSeismoHostApi();
    mSeismo = MSeismo.withHostApi(host);
  });

  tearDown(() {
    debugDefaultTargetPlatformOverride = null;
  });

  test('configure forwards every field', () async {
    final config = MSeismoConfig(
      baseUrl: 'https://seismo.example.com',
      sdkKey: 'msk_test',
      environment: 'staging',
      release: '1.2.3',
      shakeEnabled: false,
      screenshotEnabled: true,
    );

    await mSeismo.configure(config);

    expect(host.configuredConfig, same(config));
    expect(host.configuredConfig?.baseUrl, 'https://seismo.example.com');
    expect(host.configuredConfig?.sdkKey, 'msk_test');
    expect(host.configuredConfig?.environment, 'staging');
    expect(host.configuredConfig?.release, '1.2.3');
    expect(host.configuredConfig?.shakeEnabled, isFalse);
    expect(host.configuredConfig?.screenshotEnabled, isTrue);
  });

  test('report forwards the trigger', () async {
    await mSeismo.report(trigger: ReportTrigger.screenshot);
    expect(host.reportedTrigger, ReportTrigger.screenshot);

    await mSeismo.report();
    expect(host.reportedTrigger, ReportTrigger.manual);
  });

  test('submit maps the draft and returns the native report ID', () async {
    host.submitResult = 'report-123';
    final draft = ReportDraft(
      title: 'Checkout fails',
      description: 'The payment button does nothing.',
      category: ReportCategory.ui,
      severity: ReportSeverity.critical,
      screenshot: Uint8List.fromList(<int>[1, 2, 3]),
      trigger: ReportTrigger.longPress,
      crash: CrashDetails(
        type: 'FlutterError',
        reason: 'boom',
        stackTrace: const <String>['frame 1', 'frame 2'],
        fatal: false,
      ),
    );

    final id = await mSeismo.submit(draft);

    expect(id, 'report-123');
    expect(host.submittedDraft, same(draft));
    expect(host.submittedDraft?.title, 'Checkout fails');
    expect(
        host.submittedDraft?.description, 'The payment button does nothing.');
    expect(host.submittedDraft?.category, ReportCategory.ui);
    expect(host.submittedDraft?.severity, ReportSeverity.critical);
    expect(host.submittedDraft?.trigger, ReportTrigger.longPress);
    expect(host.submittedDraft?.screenshot, Uint8List.fromList(<int>[1, 2, 3]));
    expect(host.submittedDraft?.crash?.type, 'FlutterError');
    expect(host.submittedDraft?.crash?.reason, 'boom');
    expect(
        host.submittedDraft?.crash?.stackTrace, <String>['frame 1', 'frame 2']);
    expect(host.submittedDraft?.crash?.fatal, isFalse);
  });

  test('recordError passes the message, stack trace, and fatal flag', () async {
    await mSeismo.recordError(
      'Something broke',
      stackTrace: 'frame 1\nframe 2',
      fatal: true,
    );

    expect(host.recordedErrorMessage, 'Something broke');
    expect(host.recordedStackTrace, 'frame 1\nframe 2');
    expect(host.recordedFatal, isTrue);
  });

  test('checkForPendingCrash forwards to the native SDK', () async {
    await mSeismo.checkForPendingCrash();
    expect(host.checkedForPendingCrash, 1);
  });

  test('logs, breadcrumbs, and network requests are forwarded', () async {
    await mSeismo.log('user opened checkout');
    await mSeismo.breadcrumb(BreadcrumbKind.tap, 'tapped Pay');
    final request = NetworkRequest(
      url: 'https://api.example.com/pay',
      method: 'POST',
      statusCode: 500,
      durationMs: 321,
      requestBody: '{"amount":10}',
      responseBody: '{"error":"boom"}',
    );
    await mSeismo.recordNetworkRequest(request);
    await mSeismo.setCapturesNetworkBodies(true);

    expect(host.logs, <String>['user opened checkout']);
    expect(
      host.breadcrumbs,
      <(BreadcrumbKind, String)>[(BreadcrumbKind.tap, 'tapped Pay')],
    );
    expect(host.networkRequests, <NetworkRequest>[request]);
    expect(host.capturesNetworkBodies, isTrue);
  });

  test('feature flags, experiments, Crashlytics, and redaction are forwarded',
      () async {
    final flags = <String, Object?>{
      'enabled': true,
      'limit': 42,
      'ratio': 0.5,
      'variant': 'control',
    };
    await mSeismo.setFeatureFlags(flags);
    await mSeismo.setExperiments(<String, String>{'checkout': 'variant-b'});
    await mSeismo.setCrashlyticsInstallationId('crashlytics-id');
    await mSeismo.setRedactionAllowList(<String>['support@example.com']);
    await mSeismo.setRedactionDenyList(<String>['project-codename']);

    expect(host.featureFlags, flags);
    expect(host.experiments, <String, String>{'checkout': 'variant-b'});
    expect(host.crashlyticsInstallationId, 'crashlytics-id');
    expect(host.redactionAllowList, <String>['support@example.com']);
    expect(host.redactionDenyList, <String>['project-codename']);
  });

  test('queue metrics, retry, and identifiers are forwarded', () async {
    host.pendingCount = 3;

    expect(await mSeismo.pendingReportCount, 3);
    await mSeismo.retryNow();
    expect(host.retried, isTrue);
    expect(await mSeismo.installationId, 'installation-id');
    expect(await mSeismo.sessionId, 'session-id');
    expect(await mSeismo.newTraceId(), 'trace-0');
    expect(await mSeismo.newTraceId(), 'trace-1');
  });

  test('auto-report stream receives native callbacks', () async {
    final report = AutoReport(
      id: 'report-1',
      title: 'FlutterError',
      description: 'boom',
      category: ReportCategory.crash,
      severity: ReportSeverity.high,
      trigger: ReportTrigger.nonFatal,
      occurredAtMillis: 1700000000000,
      crash: CrashDetails(
        type: 'FlutterError',
        reason: 'boom',
        stackTrace: const <String>['frame 1'],
        fatal: false,
      ),
    );

    final emitted = expectLater(
      mSeismo.autoReports,
      emits(
        isA<AutoReport>()
            .having((value) => value.id, 'id', 'report-1')
            .having((value) => value.trigger, 'trigger', ReportTrigger.nonFatal)
            .having((value) => value.description, 'description', 'boom'),
      ),
    );

    mSeismo.onAutoReportForTesting(report);
    await emitted;
  });

  test('channel-backed members are available on Android', () async {
    debugDefaultTargetPlatformOverride = TargetPlatform.android;

    await mSeismo.log('android log');
    expect(host.logs, <String>['android log']);

    await mSeismo.report();
    expect(host.reportedTrigger, ReportTrigger.manual);
  });

  test('every channel-backed member throws on unsupported platforms', () async {
    debugDefaultTargetPlatformOverride = TargetPlatform.linux;

    final calls = <Future<void> Function()>[
      () => mSeismo.configure(
            MSeismoConfig(
              baseUrl: 'https://example.com',
              sdkKey: 'msk_test',
              environment: 'production',
            ),
          ),
      () => mSeismo.report(),
      () => mSeismo.submit(ReportDraft()),
      () => mSeismo.recordError('message'),
      () => mSeismo.checkForPendingCrash(),
      () => mSeismo.log('message'),
      () => mSeismo.breadcrumb(BreadcrumbKind.navigation, 'screen'),
      () => mSeismo.recordNetworkRequest(
            NetworkRequest(
                url: 'https://example.com', method: 'GET', durationMs: 1),
          ),
      () => mSeismo.setCapturesNetworkBodies(true),
      () => mSeismo.setFeatureFlags(<String, Object?>{'flag': true}),
      () => mSeismo.setExperiments(<String, String>{'experiment': 'a'}),
      () => mSeismo.setCrashlyticsInstallationId('id'),
      () => mSeismo.setRedactionAllowList(<String>['allowed']),
      () => mSeismo.setRedactionDenyList(<String>['denied']),
      () => mSeismo.retryNow(),
      () => mSeismo.newTraceId(),
    ];

    for (final call in calls) {
      expect(call, throwsUnsupportedError);
    }
    expect(() => mSeismo.pendingReportCount, throwsUnsupportedError);
    expect(() => mSeismo.installationId, throwsUnsupportedError);
    expect(() => mSeismo.sessionId, throwsUnsupportedError);
  });

  test('version constant matches pubspec.yaml', () {
    final pubspec = File('pubspec.yaml').readAsStringSync();
    final version = RegExp(
      r'^version:\s*(\S+)\s*$',
      multiLine: true,
    ).firstMatch(pubspec)?.group(1);

    expect(version, mSeismoVersion);
  });
}
