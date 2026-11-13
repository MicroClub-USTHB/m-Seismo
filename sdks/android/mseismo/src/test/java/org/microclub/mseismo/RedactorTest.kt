package org.microclub.mseismo

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class RedactorTest {
    private val redactor = Redactor(Configuration.DEFAULT_REDACTION_RULES, emptyList(), emptyList())

    @Test
    fun personalDataIsRedacted() {
        assertEquals("mail [REDACTED] now", redactor.redact("mail jane.doe@example.com now"))
        assertEquals("call [REDACTED]", redactor.redact("call +213 555 12 34 56"))
        assertEquals("card [REDACTED]", redactor.redact("card 4111 1111 1111 1111"))
        assertEquals("auth [REDACTED]", redactor.redact("auth Bearer abc.def-123"))
        assertEquals("Cart loaded with 3 items", redactor.redact("Cart loaded with 3 items"))
    }

    @Test
    fun allowListIsKeptAndDenyListIsRedacted() {
        val custom = Redactor(Configuration.DEFAULT_REDACTION_RULES, listOf("support@example.com"), listOf("ProjectX"))
        assertEquals("write support@example.com about [REDACTED], not [REDACTED]", custom.redact("write support@example.com about ProjectX, not jane@example.com"))
    }

    @Test
    fun shortListEntriesDoNotBreakTheAllowList() {
        val custom = Redactor(Configuration.DEFAULT_REDACTION_RULES, listOf("support@example.com", "LOW", "0"), listOf("ALLOW"))
        assertEquals("mail support@example.com", custom.redact("mail support@example.com"))
    }

    @Test
    fun longTokensAreRedactedQuickly() {
        // Before the email rule was anchored, this took about 8 s (time grew with the square of the length).
        val token = "a".repeat(64_000)
        redactor.redact(token)
        val start = System.nanoTime()
        redactor.redact(token)
        assertTrue((System.nanoTime() - start) / 1_000_000 < 1_000)
    }

    @Test
    fun emailsInsideOtherTextAreStillFound() {
        assertEquals("?email=[REDACTED]&x=1", redactor.redact("?email=jane@example.com&x=1"))
        assertEquals("<[REDACTED]>", redactor.redact("<jane.doe+test@mail.example.co>"))
    }

    @Test
    fun everyFreeTextOfTheReportIsRedacted() {
        val email = "jane@example.com"
        val report = Report(
            id = "id", installationId = "i", sessionId = "s", occurredAtMillis = 0, environment = "e",
            release = Release("1", "1"), trigger = Trigger.MANUAL, autoSubmitted = false,
            userInput = UserInput(title = email, description = email),
            device = Device("Android", "15", "m", null),
            logs = listOf(LogLine(0, email)),
            breadcrumbs = listOf(Breadcrumb(0, BreadcrumbKind.TAP, email)),
            networkRequests = listOf(NetworkRequest(0, "https://a.b/?email=$email", "GET", 200, 1, email, email)),
        )
        val json = redactor.redact(report).toString()
        assertEquals(false, email in json)
    }

    // Stack lines are redacted too, for the messages in "Caused by:". The rules must leave the frames readable.
    @Test
    fun stackFramesAreLeftIntact() {
        val frames = listOf(
            "org.microclub.mseismo.example.MainActivity.onCreate\$lambda\$5\$0(MainActivity.kt:65)",
            "android.view.View.performClick(View.java:8083)",
            "com.android.internal.os.ZygoteInit.main(ZygoteInit.java:972)",
            "a.b.c.d(SourceFile:1234567)", // R8: short names, big line numbers
            "x0.a\$b.invokeSuspend(Unknown Source:12)",
            "java.lang.reflect.Method.invoke(Native Method)",
        )
        assertEquals(frames, frames.map(redactor::redact))
        assertEquals("Caused by: java.lang.IllegalStateException: no account for [REDACTED]", redactor.redact("Caused by: java.lang.IllegalStateException: no account for ali@example.com"))
    }
}
