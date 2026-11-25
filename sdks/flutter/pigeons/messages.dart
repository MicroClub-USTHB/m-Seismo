import 'package:pigeon/pigeon.dart';

@ConfigurePigeon(
  PigeonOptions(
    dartOut: 'lib/src/messages.g.dart',
    swiftOut: 'ios/Classes/Messages.g.swift',
    kotlinOut:
        'android/src/main/kotlin/org/microclub/mseismo/flutter/Messages.g.kt',
    kotlinOptions: KotlinOptions(package: 'org.microclub.mseismo.flutter'),
    dartPackageName: 'm_seismo',
  ),
)
class MSeismoConfig {
  MSeismoConfig({
    required this.baseUrl,
    required this.sdkKey,
    required this.environment,
    this.release,
    this.shakeEnabled = true,
    this.screenshotEnabled = false,
  });

  String baseUrl;
  String sdkKey;
  String environment;
  String? release;
  bool shakeEnabled;
  bool screenshotEnabled;
}

class CrashDetails {
  CrashDetails({
    required this.type,
    this.reason,
    required this.stackTrace,
    required this.fatal,
  });

  String type;
  String? reason;
  List<String> stackTrace;
  bool fatal;
}

class ReportDraft {
  ReportDraft({
    this.title = '',
    this.description = '',
    this.category = ReportCategory.bug,
    this.severity = ReportSeverity.medium,
    this.screenshot,
    this.trigger = ReportTrigger.manual,
    this.crash,
  });

  String title;
  String description;
  ReportCategory category;
  ReportSeverity severity;
  Uint8List? screenshot;
  ReportTrigger trigger;
  CrashDetails? crash;
}

enum ReportTrigger { shake, screenshot, manual, crash, longPress, nonFatal }

enum ReportCategory { bug, crash, ui, other }

enum ReportSeverity { low, medium, high, critical }

enum BreadcrumbKind { navigation, tap, network, lifecycle }

class NetworkRequest {
  NetworkRequest({
    required this.url,
    required this.method,
    this.statusCode,
    required this.durationMs,
    this.requestBody,
    this.responseBody,
  });

  String url;
  String method;
  int? statusCode;
  int durationMs;
  String? requestBody;
  String? responseBody;
}

class AutoReport {
  AutoReport({
    required this.id,
    required this.title,
    required this.description,
    required this.category,
    required this.severity,
    required this.trigger,
    required this.occurredAtMillis,
    this.crash,
  });

  String id;
  String title;
  String description;
  ReportCategory category;
  ReportSeverity severity;
  ReportTrigger trigger;
  int occurredAtMillis;
  CrashDetails? crash;
}

@HostApi()
abstract class MSeismoHostApi {
  void configure(MSeismoConfig config);

  void report(ReportTrigger trigger);

  String submit(ReportDraft draft);

  void recordError(String message, String? stackTrace, bool fatal);

  void checkForPendingCrash();

  void log(String message);

  void breadcrumb(BreadcrumbKind kind, String message);

  void recordNetworkRequest(NetworkRequest request);

  void setCapturesNetworkBodies(bool enabled);

  void setFeatureFlags(Map<String, Object?> flags);

  void setExperiments(Map<String, String> experiments);

  void setCrashlyticsInstallationId(String? id);

  void setRedactionAllowList(List<String> values);

  void setRedactionDenyList(List<String> values);

  int pendingReportCount();

  void retryNow();

  String getInstallationId();

  String getSessionId();

  String newTraceId();
}

@FlutterApi()
abstract class MSeismoFlutterApi {
  void onAutoReport(AutoReport report);
}
