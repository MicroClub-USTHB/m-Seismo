package io.mseismo.android.model

data class Report(
        val title:String="",
        val description:String="",
        val category:Category=Category.BUG,
        val severity:Severity=Severity.MEDIUM,
        val screenshot: ByteArray?=null,
        val deviceContext:Map<String, String>?=null,
        val sessionId:String?=null,
        val recentLogs:List<String>?=null,
        val breadcrumbs:List<String>?=null,
        val networkRequests: List<String>?=null
        )

enum class Category{
    BUG,
    CRASH,
    UI,
    OTHER
}

enum class Severity{
    LOW,
    MEDIUM,
    HIGH,
    CRITICAL
}