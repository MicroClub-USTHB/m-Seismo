package org.microclub.mseismo

import android.app.Activity
import android.content.Context
import android.hardware.Sensor
import android.hardware.SensorEvent
import android.hardware.SensorEventListener
import android.hardware.SensorManager
import android.os.Build
import android.util.Log

// Listens only while a screen of the app is showing
internal class Triggers(
    private val shakeEnabled: () -> Boolean,
    private val screenshotEnabled: () -> Boolean,
    private val onTrigger: (Activity, Trigger) -> Unit,
) : ActivityCallbacks {
    private val shake = ShakeDetector()

    // Main thread only. Split screen can show two screens at once.
    private val resumed = LinkedHashSet<Activity>()
    // Any: ScreenCaptureCallback only exists on Android 14+.
    private val screenshotCallbacks = HashMap<Activity, Any>()
    private var listening: SensorManager? = null

    private val sensorListener = object : SensorEventListener {
        override fun onSensorChanged(event: SensorEvent) {
            val activity = resumed.lastOrNull() ?: return
            if (shake.onAcceleration(event.values[0], event.values[1], event.values[2])) onTrigger(activity, Trigger.SHAKE)
        }

        override fun onAccuracyChanged(sensor: Sensor?, accuracy: Int) = Unit
    }

    // App's main thread: a failure turns the trigger off, never crashes the app.
    override fun onActivityResumed(activity: Activity) {
        if (activity is ReportActivity) return // no shake inside the report screen
        resumed += activity
        try {
            if (listening == null && shakeEnabled()) {
                // The app's, not the screen's: the listener can outlive this screen.
                val sensors = activity.applicationContext.getSystemService(Context.SENSOR_SERVICE) as? SensorManager
                sensors?.getDefaultSensor(Sensor.TYPE_ACCELEROMETER)?.let {
                    if (sensors.registerListener(sensorListener, it, SensorManager.SENSOR_DELAY_GAME)) listening = sensors
                }
            }
            if (screenshotEnabled() && Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
                val callback = Activity.ScreenCaptureCallback { onTrigger(activity, Trigger.SCREENSHOT) }
                activity.registerScreenCaptureCallback(activity.mainExecutor, callback)
                screenshotCallbacks[activity] = callback
            }
        } catch (e: Exception) {
            Log.w(MSeismo.TAG, "shake or screenshot trigger unavailable", e)
        }
    }

    override fun onActivityPaused(activity: Activity) {
        resumed -= activity
        try {
            val callback = screenshotCallbacks.remove(activity)
            if (callback != null && Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
                activity.unregisterScreenCaptureCallback(callback as Activity.ScreenCaptureCallback)
            }
            if (resumed.isEmpty()) {
                listening?.unregisterListener(sensorListener)
                listening = null
            }
        } catch (e: Exception) {
            Log.w(MSeismo.TAG, "could not stop the shake or screenshot trigger", e)
        }
    }
}
