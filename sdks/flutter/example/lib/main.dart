import 'dart:async';

import 'package:flutter/material.dart';
import 'package:m_seismo/m_seismo.dart';

const String _baseUrl = 'http://127.0.0.1:8787';
const String _sdkKey = 'example-project-key';

void main() {
  runApp(const MSeismoExampleApp());
}

class MSeismoExampleApp extends StatelessWidget {
  const MSeismoExampleApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'm-Seismo Flutter example',
      theme: ThemeData(
        colorScheme: ColorScheme.fromSeed(seedColor: Colors.indigo),
        useMaterial3: true,
      ),
      home: const ExamplePage(),
    );
  }
}

class ExamplePage extends StatefulWidget {
  const ExamplePage({super.key});

  @override
  State<ExamplePage> createState() => _ExamplePageState();
}

class _ExamplePageState extends State<ExamplePage> {
  final MSeismo _mSeismo = MSeismo.instance;
  final List<AutoReport> _autoReports = <AutoReport>[];

  StreamSubscription<AutoReport>? _autoReportSubscription;
  String _status = 'Not configured';
  int? _pendingReports;
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    _autoReportSubscription = _mSeismo.autoReports.listen((report) {
      if (!mounted) return;
      setState(() => _autoReports.insert(0, report));
    });
  }

  @override
  void dispose() {
    _autoReportSubscription?.cancel();
    super.dispose();
  }

  Future<void> _run(String label, Future<void> Function() action) async {
    setState(() {
      _busy = true;
      _status = label;
    });
    try {
      await action();
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _busy = false;
        _status = '$label failed: $error';
      });
      return;
    }
    if (!mounted) return;
    setState(() {
      _busy = false;
      _status = '$label completed';
    });
  }

  Future<void> _refreshPendingReports() async {
    final count = await _mSeismo.pendingReportCount;
    if (!mounted) return;
    setState(() => _pendingReports = count);
  }

  Future<void> _configure() => _run('Configure', () async {
        await _mSeismo.configure(
          MSeismoConfig(
            baseUrl: _baseUrl,
            sdkKey: _sdkKey,
            environment: 'development',
            release: '0.1.0',
            shakeEnabled: true,
            screenshotEnabled: true,
          ),
        );
        await _refreshPendingReports();
      });

  Future<void> _submitHeadlessReport() => _run(
        'Submit headless report',
        () async {
          await _mSeismo.submit(
            ReportDraft(
              title: 'Flutter example report',
              description:
                  'Submitted from the Flutter example without a sheet.',
              category: ReportCategory.bug,
              severity: ReportSeverity.medium,
              trigger: ReportTrigger.manual,
            ),
          );
          await _refreshPendingReports();
        },
      );

  Future<void> _recordError() => _run('Record Flutter error', () async {
        await _mSeismo.recordError(
          'Example non-fatal Flutter error',
          stackTrace: StackTrace.current.toString(),
        );
      });

  Future<void> _addDiagnostics() => _run('Add diagnostics', () async {
        await _mSeismo.log('Example log line');
        await _mSeismo.breadcrumb(
          BreadcrumbKind.navigation,
          'Opened the Flutter example screen',
        );
        await _mSeismo.recordNetworkRequest(
          NetworkRequest(
            url: 'https://api.example.com/checkout',
            method: 'POST',
            statusCode: 500,
            durationMs: 321,
            requestBody: '{"amount":42}',
            responseBody: '{"error":"payment declined"}',
          ),
        );
      });

  Future<void> _retryNow() => _run('Retry pending reports', () async {
        await _mSeismo.retryNow();
        await Future<void>.delayed(const Duration(seconds: 1));
        await _refreshPendingReports();
      });

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('m-Seismo Flutter example')),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: <Widget>[
          Card(
            child: Padding(
              padding: const EdgeInsets.all(16),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Text(
                    'Server: $_baseUrl',
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                  const SizedBox(height: 4),
                  const Text('Key: example-project-key'),
                  const SizedBox(height: 12),
                  Text(
                    _busy ? 'Working…' : _status,
                    style: Theme.of(context).textTheme.bodyMedium,
                  ),
                  const SizedBox(height: 8),
                  Text(
                    _pendingReports == null
                        ? 'Pending reports: —'
                        : 'Pending reports: $_pendingReports',
                  ),
                ],
              ),
            ),
          ),
          const SizedBox(height: 16),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: <Widget>[
              FilledButton(
                onPressed: _busy ? null : _configure,
                child: const Text('Configure SDK'),
              ),
              FilledButton(
                onPressed: _busy
                    ? null
                    : () => _run('Open report sheet', _mSeismo.report),
                child: const Text('Open report sheet'),
              ),
              FilledButton(
                onPressed: _busy ? null : _submitHeadlessReport,
                child: const Text('Submit headless report'),
              ),
              OutlinedButton(
                onPressed: _busy ? null : _recordError,
                child: const Text('Record Flutter error'),
              ),
              OutlinedButton(
                onPressed: _busy
                    ? null
                    : () => _run(
                          'Check pending crash',
                          _mSeismo.checkForPendingCrash,
                        ),
                child: const Text('Check pending crash'),
              ),
              OutlinedButton(
                onPressed: _busy ? null : _addDiagnostics,
                child: const Text('Add log / breadcrumb / network'),
              ),
              OutlinedButton(
                onPressed: _busy
                    ? null
                    : () => _run(
                          'Refresh pending reports',
                          _refreshPendingReports,
                        ),
                child: const Text('Refresh pending count'),
              ),
              OutlinedButton(
                onPressed: _busy ? null : _retryNow,
                child: const Text('Retry now'),
              ),
            ],
          ),
          const SizedBox(height: 24),
          Text(
            'Automatic reports',
            style: Theme.of(context).textTheme.titleLarge,
          ),
          const SizedBox(height: 8),
          if (_autoReports.isEmpty)
            const Text('No automatic reports yet.')
          else
            ..._autoReports.map(
              (report) => Card(
                child: ListTile(
                  title: Text(report.title),
                  subtitle: Text(
                    '${report.trigger.name} · ${report.severity.name}\n'
                    '${report.description}',
                  ),
                  isThreeLine: true,
                ),
              ),
            ),
        ],
      ),
    );
  }
}
