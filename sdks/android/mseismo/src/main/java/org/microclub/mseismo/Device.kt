package org.microclub.mseismo

import android.app.ActivityManager
import android.content.Context
import android.content.res.Configuration as AndroidConfiguration
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import android.os.BatteryManager
import android.os.Build
import java.util.Locale

// Optional fields are null when Android can't give them.
internal data class Device(
    val osName: String,
    val osVersion: String,
    val model: String,
    val locale: String?,
    val batteryLevel: Double? = null,
    val availableMemoryBytes: Long? = null,
    val networkType: String? = null,
    val orientation: String? = null,
) {
    companion object {
        fun current(context: Context): Device = Device(
            osName = "Android",
            osVersion = Build.VERSION.RELEASE.orEmpty().ifEmpty { "unknown" },
            model = "${Build.MANUFACTURER} ${Build.MODEL}".trim().ifEmpty { "unknown" },
            locale = Locale.getDefault().toLanguageTag(),
            batteryLevel = tryOrNull { battery(context) },
            availableMemoryBytes = tryOrNull { memory(context) },
            networkType = tryOrNull { network(context) } ?: "unknown",
            orientation = when (context.resources.configuration.orientation) {
                AndroidConfiguration.ORIENTATION_PORTRAIT -> "portrait"
                AndroidConfiguration.ORIENTATION_LANDSCAPE -> "landscape"
                else -> "unknown"
            },
        )

        private fun battery(context: Context): Double? {
            val percent = context.getSystemService(BatteryManager::class.java)
                ?.getIntProperty(BatteryManager.BATTERY_PROPERTY_CAPACITY) ?: return null
            return if (percent in 0..100) percent / 100.0 else null
        }

        private fun memory(context: Context): Long? {
            val activity = context.getSystemService(ActivityManager::class.java) ?: return null
            return ActivityManager.MemoryInfo().also { activity.getMemoryInfo(it) }.availMem
        }

        private fun network(context: Context): String? {
            val connectivity = context.getSystemService(ConnectivityManager::class.java) ?: return null
            val caps = connectivity.getNetworkCapabilities(connectivity.activeNetwork) ?: return "none"
            return when {
                caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) -> "wifi"
                caps.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) -> "cellular"
                caps.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET) -> "ethernet"
                else -> "unknown"
            }
        }

        private inline fun <T> tryOrNull(block: () -> T): T? = try {
            block()
        } catch (e: Exception) {
            null
        }
    }
}
