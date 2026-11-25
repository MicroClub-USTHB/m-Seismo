package org.microclub.mseismo

import org.microclub.mseismo.flutter.AutoReport as PigeonAutoReport
import org.microclub.mseismo.flutter.BreadcrumbKind as PigeonBreadcrumbKind
import org.microclub.mseismo.flutter.CrashDetails
import org.microclub.mseismo.flutter.ReportCategory
import org.microclub.mseismo.flutter.ReportSeverity
import org.microclub.mseismo.flutter.ReportTrigger

internal object TypeMapping {
    fun trigger(trigger: ReportTrigger): Trigger = when (trigger) {
        ReportTrigger.SHAKE -> Trigger.SHAKE
        ReportTrigger.SCREENSHOT -> Trigger.SCREENSHOT
        ReportTrigger.MANUAL -> Trigger.MANUAL
        ReportTrigger.CRASH -> Trigger.CRASH
        ReportTrigger.LONG_PRESS -> Trigger.LONG_PRESS
        ReportTrigger.NON_FATAL -> Trigger.NON_FATAL
    }

    fun trigger(trigger: Trigger): ReportTrigger = when (trigger) {
        Trigger.SHAKE -> ReportTrigger.SHAKE
        Trigger.SCREENSHOT -> ReportTrigger.SCREENSHOT
        Trigger.MANUAL -> ReportTrigger.MANUAL
        Trigger.CRASH -> ReportTrigger.CRASH
        Trigger.LONG_PRESS -> ReportTrigger.LONG_PRESS
        Trigger.NON_FATAL -> ReportTrigger.NON_FATAL
    }

    fun category(category: ReportCategory): Category = when (category) {
        ReportCategory.BUG -> Category.BUG
        ReportCategory.CRASH -> Category.CRASH
        ReportCategory.UI -> Category.UI
        ReportCategory.OTHER -> Category.OTHER
    }

    fun category(category: Category): ReportCategory = when (category) {
        Category.BUG -> ReportCategory.BUG
        Category.CRASH -> ReportCategory.CRASH
        Category.UI -> ReportCategory.UI
        Category.OTHER -> ReportCategory.OTHER
    }

    fun severity(severity: ReportSeverity): Severity = when (severity) {
        ReportSeverity.LOW -> Severity.LOW
        ReportSeverity.MEDIUM -> Severity.MEDIUM
        ReportSeverity.HIGH -> Severity.HIGH
        ReportSeverity.CRITICAL -> Severity.CRITICAL
    }

    fun severity(severity: Severity): ReportSeverity = when (severity) {
        Severity.LOW -> ReportSeverity.LOW
        Severity.MEDIUM -> ReportSeverity.MEDIUM
        Severity.HIGH -> ReportSeverity.HIGH
        Severity.CRITICAL -> ReportSeverity.CRITICAL
    }

    fun breadcrumbKind(kind: PigeonBreadcrumbKind): BreadcrumbKind = when (kind) {
        PigeonBreadcrumbKind.NAVIGATION -> BreadcrumbKind.NAVIGATION
        PigeonBreadcrumbKind.TAP -> BreadcrumbKind.TAP
        PigeonBreadcrumbKind.NETWORK -> BreadcrumbKind.NETWORK
        PigeonBreadcrumbKind.LIFECYCLE -> BreadcrumbKind.LIFECYCLE
    }

    fun autoReport(report: AutoReport): PigeonAutoReport = PigeonAutoReport(
        id = report.id,
        title = report.title ?: "",
        description = report.description ?: "",
        category = category(report.category ?: Category.OTHER),
        severity = severity(report.severity ?: Severity.MEDIUM),
        trigger = trigger(report.trigger),
        occurredAtMillis = report.occurredAtMillis,
        crash = report.crash?.let {
            CrashDetails(
                type = it.type,
                reason = it.reason,
                stackTrace = it.stackTrace,
                fatal = report.trigger == Trigger.CRASH,
            )
        },
    )

    fun flagValues(values: Map<String, Any?>): Map<String, FlagValue> {
        val flags = mutableMapOf<String, FlagValue>()
        for ((key, value) in values) {
            if (key.isEmpty() || value == null) continue
            val mapped = when (value) {
                is String -> FlagValue.StringValue(value)
                is Boolean -> FlagValue.BooleanValue(value)
                is Number -> FlagValue.NumberValue(value.toDouble())
                else -> null
            }
            if (mapped != null) flags[key] = mapped
        }
        return flags
    }
}