import 'dart:async';

import 'package:flutter/foundation.dart';

import 'messages.g.dart';

const String _unsupportedPlatformMessage =
    'm-Seismo Flutter bridge currently supports iOS only; Android awaits '
    'the native Kotlin SDK.';

/// Thin Dart facade over the native m-Seismo iOS SDK.
///
/// Every channel-backed member is only available on iOS. The package can be
/// compiled for other platforms, but calling into it there throws an
/// [UnsupportedError].
class MSeismo {
  MSeismo._({MSeismoHostApi? hostApi})
      : _hostApi = hostApi ?? MSeismoHostApi() {
    MSeismoFlutterApi.setUp(_MSeismoFlutterApi(this));
  }

  /// The shared bridge instance used by applications.
  static final MSeismo instance = MSeismo._();

  /// Creates a facade backed by a fake host API. Intended for tests only.
  @visibleForTesting
  factory MSeismo.withHostApi(MSeismoHostApi hostApi) =>
      MSeismo._(hostApi: hostApi);

  final MSeismoHostApi _hostApi;
  final StreamController<AutoReport> _autoReports =
      StreamController<AutoReport>.broadcast();

  /// Automatic (crash and non-fatal) reports captured by the native SDK.
  Stream<AutoReport> get autoReports => _autoReports.stream;

  /// Connects this app to an m-Seismo project.
  Future<void> configure(MSeismoConfig config) {
    _ensureIOS();
    return _hostApi.configure(config);
  }

  /// Opens the native report sheet.
  Future<void> report({ReportTrigger trigger = ReportTrigger.manual}) {
    _ensureIOS();
    return _hostApi.report(trigger);
  }

  /// Submits a report without opening the native sheet, returning its ID.
  Future<String> submit(ReportDraft draft) {
    _ensureIOS();
    return _hostApi.submit(draft);
  }

  /// Records a Dart error to be auto-reported on the next
  /// [checkForPendingCrash] or app launch.
  Future<void> recordError(
    String message, {
    String? stackTrace,
    bool fatal = false,
  }) {
    _ensureIOS();
    return _hostApi.recordError(message, stackTrace, fatal);
  }

  /// Reports a crash or error persisted by a previous launch, if any.
  Future<void> checkForPendingCrash() {
    _ensureIOS();
    return _hostApi.checkForPendingCrash();
  }

  Future<void> log(String message) {
    _ensureIOS();
    return _hostApi.log(message);
  }

  Future<void> breadcrumb(BreadcrumbKind kind, String message) {
    _ensureIOS();
    return _hostApi.breadcrumb(kind, message);
  }

  Future<void> recordNetworkRequest(NetworkRequest request) {
    _ensureIOS();
    return _hostApi.recordNetworkRequest(request);
  }

  Future<void> setCapturesNetworkBodies(bool enabled) {
    _ensureIOS();
    return _hostApi.setCapturesNetworkBodies(enabled);
  }

  Future<void> setFeatureFlags(Map<String, Object?> flags) {
    _ensureIOS();
    return _hostApi.setFeatureFlags(flags);
  }

  Future<void> setExperiments(Map<String, String> experiments) {
    _ensureIOS();
    return _hostApi.setExperiments(experiments);
  }

  Future<void> setCrashlyticsInstallationId(String? id) {
    _ensureIOS();
    return _hostApi.setCrashlyticsInstallationId(id);
  }

  Future<void> setRedactionAllowList(List<String> values) {
    _ensureIOS();
    return _hostApi.setRedactionAllowList(values);
  }

  Future<void> setRedactionDenyList(List<String> values) {
    _ensureIOS();
    return _hostApi.setRedactionDenyList(values);
  }

  Future<int> get pendingReportCount {
    _ensureIOS();
    return _hostApi.pendingReportCount();
  }

  Future<void> retryNow() {
    _ensureIOS();
    return _hostApi.retryNow();
  }

  Future<String> get installationId {
    _ensureIOS();
    return _hostApi.getInstallationId();
  }

  Future<String> get sessionId {
    _ensureIOS();
    return _hostApi.getSessionId();
  }

  Future<String> newTraceId() {
    _ensureIOS();
    return _hostApi.newTraceId();
  }

  /// Feeds an auto-report into [autoReports]. Visible for testing so unit
  /// tests can exercise the stream without a device.
  @visibleForTesting
  void onAutoReportForTesting(AutoReport report) {
    _autoReports.add(report);
  }

  void _ensureIOS() {
    if (defaultTargetPlatform != TargetPlatform.iOS) {
      throw UnsupportedError(_unsupportedPlatformMessage);
    }
  }
}

class _MSeismoFlutterApi extends MSeismoFlutterApi {
  _MSeismoFlutterApi(this._owner);

  final MSeismo _owner;

  @override
  void onAutoReport(AutoReport report) {
    _owner._autoReports.add(report);
  }
}
