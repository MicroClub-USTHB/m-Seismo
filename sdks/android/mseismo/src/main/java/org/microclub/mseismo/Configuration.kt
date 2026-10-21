package org.microclub.mseismo

import java.net.URI

// Schema limit for environment and release fields.
internal const val MAX_FIELD_LENGTH = 64

// Same format as the backend's keys.
private val APP_KEY = Regex("msk_[A-Za-z0-9_-]+")

/**
 * @property endpoint backend base URL. `http://` only works in debug builds.
 * @property release defaults to the app's versionName and versionCode.
 */
public data class Configuration(
    val endpoint: String,
    val appKey: String,
    val environment: String = "production",
    val release: Release? = null,
    val shakeEnabled: Boolean = true,
    val screenshotEnabled: Boolean = false,
) {
    internal val reportsUrl: String
        get() = endpoint.trimEnd('/') + "/api/v1/reports"

    internal fun validate(allowHttp: Boolean) {
        val uri = URI(endpoint)
        val host = uri.host ?: uri.rawAuthority?.substringAfterLast('@')?.substringBefore(':')
        require(!host.isNullOrEmpty()) { "endpoint must be an absolute URL" }
        require(uri.rawQuery == null && uri.rawFragment == null) { "endpoint must not have a query or fragment" }
        when (uri.scheme?.lowercase()) {
            "https" -> Unit
            "http" -> require(allowHttp) { "http endpoints are only allowed in debuggable builds" }
            else -> throw IllegalArgumentException("endpoint must be an http(s) URL")
        }
        require(APP_KEY.matches(appKey)) { "appKey must be msk_ followed by the key, with no spaces or line breaks" }
        require(environment.length in 1..MAX_FIELD_LENGTH) { "environment must be 1 to $MAX_FIELD_LENGTH characters" }
        release?.let {
            require(it.version.length in 1..MAX_FIELD_LENGTH) { "release.version must be 1 to $MAX_FIELD_LENGTH characters" }
            require(it.build.length in 1..MAX_FIELD_LENGTH) { "release.build must be 1 to $MAX_FIELD_LENGTH characters" }
        }
    }

    override fun toString(): String =
        "Configuration(endpoint=$endpoint, appKey=msk_***, environment=$environment, release=$release, " +
            "shakeEnabled=$shakeEnabled, screenshotEnabled=$screenshotEnabled)"
}

public data class Release(val version: String, val build: String)
