import 'package:m_seismo/src/messages.g.dart';

class FakeMSeismoHostApi extends MSeismoHostApi {
  MSeismoConfig? configuredConfig;
  ReportTrigger? reportedTrigger;
  ReportDraft? submittedDraft;
  String submitResult = 'fake-report-id';

  String? recordedErrorMessage;
  String? recordedStackTrace;
  bool? recordedFatal;

  final List<String> logs = <String>[];
  final List<(BreadcrumbKind, String)> breadcrumbs =
      <(BreadcrumbKind, String)>[];
  final List<NetworkRequest> networkRequests = <NetworkRequest>[];
  bool? capturesNetworkBodies;

  Map<String, Object?>? featureFlags;
  Map<String, String>? experiments;
  String? crashlyticsInstallationId;
  List<String>? redactionAllowList;
  List<String>? redactionDenyList;

  int pendingCount = 0;
  bool retried = false;
  int checkedForPendingCrash = 0;

  String installationIdValue = 'installation-id';
  String sessionIdValue = 'session-id';
  int traceIdCounter = 0;

  @override
  Future<void> configure(MSeismoConfig config) async {
    configuredConfig = config;
  }

  @override
  Future<void> report(ReportTrigger trigger) async {
    reportedTrigger = trigger;
  }

  @override
  Future<String> submit(ReportDraft draft) async {
    submittedDraft = draft;
    return submitResult;
  }

  @override
  Future<void> recordError(
    String message,
    String? stackTrace,
    bool fatal,
  ) async {
    recordedErrorMessage = message;
    recordedStackTrace = stackTrace;
    recordedFatal = fatal;
  }

  @override
  Future<void> checkForPendingCrash() async {
    checkedForPendingCrash += 1;
  }

  @override
  Future<void> log(String message) async {
    logs.add(message);
  }

  @override
  Future<void> breadcrumb(BreadcrumbKind kind, String message) async {
    breadcrumbs.add((kind, message));
  }

  @override
  Future<void> recordNetworkRequest(NetworkRequest request) async {
    networkRequests.add(request);
  }

  @override
  Future<void> setCapturesNetworkBodies(bool enabled) async {
    capturesNetworkBodies = enabled;
  }

  @override
  Future<void> setFeatureFlags(Map<String, Object?> flags) async {
    featureFlags = flags;
  }

  @override
  Future<void> setExperiments(Map<String, String> experiments) async {
    this.experiments = experiments;
  }

  @override
  Future<void> setCrashlyticsInstallationId(String? id) async {
    crashlyticsInstallationId = id;
  }

  @override
  Future<void> setRedactionAllowList(List<String> values) async {
    redactionAllowList = values;
  }

  @override
  Future<void> setRedactionDenyList(List<String> values) async {
    redactionDenyList = values;
  }

  @override
  Future<int> pendingReportCount() async => pendingCount;

  @override
  Future<void> retryNow() async {
    retried = true;
  }

  @override
  Future<String> getInstallationId() async => installationIdValue;

  @override
  Future<String> getSessionId() async => sessionIdValue;

  @override
  Future<String> newTraceId() async => 'trace-${traceIdCounter++}';
}
