package org.microclub.mseismo

// Hides personal data before it leaves the phone (FR-25).
internal class Redactor(
    private val rules: List<Regex>,
    allowList: List<String>,
    denyList: List<String>,
) {
    private val allowList = allowList.filter { it.isNotEmpty() }
    private val denyList = denyList.filter { it.isNotEmpty() }

    fun redact(text: String): String {
        if (text.isEmpty()) return text
        var working = text
        // Allowed text is swapped for placeholders made only of private-use characters, which
        // no rule or list entry can match, then put back.
        val kept = allowList.withIndex().filter { it.value in working }.map { (i, allowed) ->
            val placeholder = "\uE000${(0xE100 + i).toChar()}\uE001"
            working = working.replace(allowed, placeholder)
            placeholder to allowed
        }
        for (rule in rules) working = rule.replace(working, REDACTED)
        for (denied in denyList) working = working.replace(denied, REDACTED)
        for ((placeholder, allowed) in kept) working = working.replace(placeholder, allowed)
        return working
    }

    fun redact(report: Report): Report = report.copy(
        userInput = report.userInput?.let { it.copy(title = it.title?.let(::redact), description = it.description?.let(::redact)) },
        logs = report.logs.map { it.copy(message = redact(it.message)) },
        breadcrumbs = report.breadcrumbs.map { it.copy(message = redact(it.message)) },
        networkRequests = report.networkRequests.map {
            it.copy(url = redact(it.url), requestBody = it.requestBody?.let(::redact), responseBody = it.responseBody?.let(::redact))
        },
    )

    companion object {
        const val REDACTED = "[REDACTED]"
    }
}
