import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:m_seismo/m_seismo.dart';

final RegExp _uuidPattern = RegExp(
  r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-'
  r'[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$',
);

void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();

  late HttpServer server;
  final List<Map<String, dynamic>> receivedReports = <Map<String, dynamic>>[];

  setUpAll(() async {
    server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    server.listen((request) async {
      final body = await request.fold<List<int>>(
        <int>[],
        (previous, chunk) => previous..addAll(chunk),
      );
      final report = _extractReport(request.headers.contentType, body);
      if (report != null) {
        receivedReports.add(report);
      }
      request.response.statusCode = HttpStatus.accepted;
      request.response.headers.contentType = ContentType.json;
      request.response.write(
        jsonEncode(
            <String, Object?>{'id': report?['id'], 'status': 'accepted'}),
      );
      await request.response.close();
    });
  });

  tearDownAll(() async {
    await server.close(force: true);
  });

  testWidgets('manual report and auto-reported Flutter error reach the server',
      (WidgetTester tester) async {
    await MSeismo.instance.configure(_config(server.port));

    await MSeismo.instance.setCapturesNetworkBodies(true);
    await MSeismo.instance.log('integration log');
    await MSeismo.instance.breadcrumb(
      BreadcrumbKind.navigation,
      'integration screen',
    );
    await MSeismo.instance.recordNetworkRequest(
      NetworkRequest(
        url: 'https://example.com/api/checkout',
        method: 'POST',
        statusCode: 500,
        durationMs: 42,
        requestBody: 'request body',
        responseBody: 'response body',
      ),
    );
    await MSeismo.instance.setFeatureFlags(<String, Object?>{
      'enabled': true,
      'limit': 42,
      'ratio': 0.5,
      'variant': 'control',
    });
    await MSeismo.instance.setExperiments(<String, String>{
      'checkout': 'variant-b',
    });

    final installationId = await MSeismo.instance.installationId;
    expect(installationId, matches(_uuidPattern));

    final reportId = await MSeismo.instance.submit(
      ReportDraft(
        title: 'Integration test report',
        description: 'Headless report submitted by the Flutter bridge test.',
        category: ReportCategory.bug,
        severity: ReportSeverity.high,
        trigger: ReportTrigger.manual,
      ),
    );
    expect(reportId, matches(_uuidPattern));

    final report = await _waitForReport(
      receivedReports,
      (candidate) => candidate['id'] == reportId,
    );

    final isAndroid = defaultTargetPlatform == TargetPlatform.android;
    expect(report['platform'], isAndroid ? 'android' : 'ios');
    expect(
      (report['sdk'] as Map<String, dynamic>)['name'],
      isAndroid ? 'mseismo-android' : 'mseismo-ios',
    );
    expect(report['trigger'], 'manual');
    expect(report['auto_submitted'], isFalse);
    expect(report['id'], reportId);
    expect(report['installation_id'], installationId);
    expect(report['id'], matches(_uuidPattern));

    final userInput = report['user_input'] as Map<String, dynamic>;
    expect(userInput['title'], 'Integration test report');
    expect(
      userInput['description'],
      'Headless report submitted by the Flutter bridge test.',
    );
    expect(userInput['category'], 'bug');
    expect(userInput['severity'], 'high');

    final logs = report['logs'] as List<dynamic>;
    expect(
      logs.any((entry) =>
          (entry as Map<String, dynamic>)['message'] == 'integration log'),
      isTrue,
    );

    final breadcrumbs = report['breadcrumbs'] as List<dynamic>;
    expect(
      breadcrumbs.any(
        (entry) =>
            (entry as Map<String, dynamic>)['kind'] == 'navigation' &&
            entry['message'] == 'integration screen',
      ),
      isTrue,
    );

    final networkRequests = report['network_requests'] as List<dynamic>;
    expect(networkRequests, hasLength(1));
    final networkRequest = networkRequests.single as Map<String, dynamic>;
    expect(networkRequest['url'], 'https://example.com/api/checkout');
    expect(networkRequest['method'], 'POST');
    expect(networkRequest['status_code'], 500);
    expect(networkRequest['request_body'], 'request body');
    expect(networkRequest['response_body'], 'response body');
    expect(report['bodies_captured'], isTrue);

    final flags = report['feature_flags'] as Map<String, dynamic>;
    expect(flags['enabled'], isTrue);
    expect((flags['limit'] as num).toInt(), 42);
    expect(flags['ratio'], 0.5);
    expect(flags['variant'], 'control');

    final experiments = report['experiments'] as Map<String, dynamic>;
    expect(experiments['checkout'], 'variant-b');

    final autoReportFuture = MSeismo.instance.autoReports.first.timeout(
      const Duration(seconds: 20),
    );

    await MSeismo.instance.recordError(
      'Dart failure for integration test',
      stackTrace: 'frame 1\nframe 2',
    );
    await MSeismo.instance.checkForPendingCrash();

    final autoReport = await autoReportFuture;
    expect(autoReport.id, matches(_uuidPattern));
    expect(autoReport.trigger, ReportTrigger.nonFatal);
    expect(autoReport.crash?.type, 'FlutterError');
    expect(autoReport.crash?.reason, contains('Dart failure'));
    expect(autoReport.crash?.stackTrace, contains('frame 1'));

    final crashReport = await _waitForReport(
      receivedReports,
      (candidate) =>
          candidate['trigger'] == 'non_fatal' &&
          (candidate['crash'] as Map<String, dynamic>?)?['reason']
                  ?.toString()
                  .contains('Dart failure for integration test') ==
              true,
    );

    expect(crashReport['auto_submitted'], isTrue);
    final crash = crashReport['crash'] as Map<String, dynamic>;
    expect(crash['type'], 'FlutterError');
    expect(crash['reason'], contains('Dart failure for integration test'));
    expect(crash['stack_trace'], contains('frame 1'));
    expect(crash['stack_trace'], contains('frame 2'));
  });

  testWidgets('setFeatureFlags accepts bool, int, double, and string values',
      (WidgetTester tester) async {
    await MSeismo.instance.configure(_config(server.port));
    await MSeismo.instance.setFeatureFlags(<String, Object?>{
      'bool_flag': true,
      'int_flag': 7,
      'double_flag': 1.25,
      'string_flag': 'value',
    });

    final reportId = await MSeismo.instance.submit(
      ReportDraft(
        title: 'Feature flag type coverage',
        description: 'Checks every scalar flag type.',
        category: ReportCategory.other,
        severity: ReportSeverity.low,
      ),
    );

    final report = await _waitForReport(
      receivedReports,
      (candidate) => candidate['id'] == reportId,
    );
    final flags = report['feature_flags'] as Map<String, dynamic>;

    expect(flags['bool_flag'], isTrue);
    expect((flags['int_flag'] as num).toInt(), 7);
    expect(flags['double_flag'], 1.25);
    expect(flags['string_flag'], 'value');
  });
}

MSeismoConfig _config(int port) {
  return MSeismoConfig(
    baseUrl: 'http://127.0.0.1:$port',
    sdkKey: 'msk_flutter_example',
    environment: 'integration',
    release: '0.1.0',
    shakeEnabled: false,
    screenshotEnabled: false,
  );
}

Future<Map<String, dynamic>> _waitForReport(
  List<Map<String, dynamic>> reports,
  bool Function(Map<String, dynamic> report) predicate, {
  Duration timeout = const Duration(seconds: 20),
}) async {
  final deadline = DateTime.now().add(timeout);
  while (DateTime.now().isBefore(deadline)) {
    for (final report in reports) {
      if (predicate(report)) return report;
    }
    await Future<void>.delayed(const Duration(milliseconds: 100));
  }
  throw TimeoutException(
    'Timed out waiting for a matching report. Received triggers: '
    '${reports.map((report) => report['trigger']).toList()}',
  );
}

Map<String, dynamic>? _extractReport(ContentType? contentType, List<int> body) {
  final boundary = contentType?.parameters['boundary'];
  if (boundary == null) return null;

  final text = latin1.decode(body);
  final headerStart = text.indexOf('name="report"');
  if (headerStart == -1) return null;

  final dataStart = text.indexOf('\r\n\r\n', headerStart);
  if (dataStart == -1) return null;

  final dataEnd = text.indexOf('\r\n--$boundary', dataStart);
  if (dataEnd == -1) return null;

  final jsonBytes = body.sublist(dataStart + 4, dataEnd);
  return jsonDecode(utf8.decode(jsonBytes)) as Map<String, dynamic>;
}
