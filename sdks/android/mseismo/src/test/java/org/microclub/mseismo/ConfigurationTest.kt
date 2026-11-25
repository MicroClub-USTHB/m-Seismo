package org.microclub.mseismo

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Test

class ConfigurationTest {
    private val valid = Configuration(endpoint = "https://seismo.example.com", appKey = "msk_test")

    @Test
    fun defaultsMatchIos() {
        assertEquals("production", valid.environment)
        assertEquals(true, valid.shakeEnabled)
        assertEquals(false, valid.screenshotEnabled)
    }

    @Test
    fun reportsUrlHandlesTrailingSlash() {
        assertEquals("https://seismo.example.com/api/v1/reports", valid.reportsUrl)
        assertEquals(
            "http://10.0.2.2:8080/api/v1/reports",
            valid.copy(endpoint = "http://10.0.2.2:8080/").reportsUrl,
        )
    }

    @Test
    fun acceptsValidConfiguration() {
        valid.validate(allowHttp = false)
        valid.copy(endpoint = "HTTPS://seismo.example.com", release = Release("1.0", "1")).validate(allowHttp = false)
        valid.copy(endpoint = "http://10.0.2.2:8080").validate(allowHttp = true)
        valid.copy(endpoint = "http://seismo_backend:8080").validate(allowHttp = true)
        valid.copy(appKey = "msk_AbC-12_xyz").validate(allowHttp = false)
    }

    @Test
    fun constructingInvalidConfigurationDoesNotThrow() {
        // Throwing here would crash the app.
        Configuration(endpoint = "", appKey = "")
    }

    @Test
    fun rejectsInvalidValues() {
        val invalid = listOf(
            valid.copy(appKey = "abc"),
            valid.copy(appKey = "msk_"),
            valid.copy(appKey = "msk_abc\n"),
            valid.copy(appKey = " msk_abc"),
            valid.copy(appKey = "msk_ab c"),
            valid.copy(endpoint = "seismo.example.com"),
            valid.copy(endpoint = "https://"),
            valid.copy(endpoint = "ftp://seismo.example.com"),
            valid.copy(endpoint = "not a url"),
            valid.copy(endpoint = "https://seismo.example.com/?tenant=a"),
            valid.copy(endpoint = "https://seismo.example.com/#x"),
            valid.copy(environment = ""),
            valid.copy(environment = "x".repeat(MAX_FIELD_LENGTH + 1)),
            valid.copy(release = Release("", "1")),
            valid.copy(release = Release("1.0", "x".repeat(MAX_FIELD_LENGTH + 1))),
        )
        for (config in invalid) {
            assertThrows(config.toString(), Exception::class.java) { config.validate(allowHttp = true) }
        }
    }

    @Test
    fun rejectsHttpUnlessAllowed() {
        assertThrows(IllegalArgumentException::class.java) {
            valid.copy(endpoint = "http://seismo.example.com").validate(allowHttp = false)
        }
    }

    @Test
    fun toStringHidesKey() {
        val text = valid.copy(appKey = "msk_secret").toString()
        assertFalse(text, "msk_secret" in text)
    }
}
