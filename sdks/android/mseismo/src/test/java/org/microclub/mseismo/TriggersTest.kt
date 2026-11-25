package org.microclub.mseismo

import android.app.Activity
import android.hardware.Sensor
import android.hardware.SensorManager
import android.os.Looper
import java.time.Duration
import androidx.test.core.app.ApplicationProvider
import android.app.Application
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config
import org.robolectric.RobolectricTestRunner
import org.robolectric.shadows.SensorEventBuilder
import org.robolectric.shadows.SensorBuilder

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class TriggersTest {
    private val app: Application = ApplicationProvider.getApplicationContext()
    private val sensors = app.getSystemService(SensorManager::class.java)
    private val triggered = mutableListOf<Pair<Activity, Trigger>>()

    init {
        shadowOf(sensors).addSensor(SensorBuilder.newBuilder().setType(Sensor.TYPE_ACCELEROMETER).build())
    }

    private fun shake() = repeat(4) {
        val accelerometer = sensors.getDefaultSensor(Sensor.TYPE_ACCELEROMETER)!!
        for (values in listOf(floatArrayOf(30f, 0f, 9.8f), floatArrayOf(0f, 0f, 9.8f))) {
            shadowOf(sensors).sendSensorEventToListeners(SensorEventBuilder.newBuilder(accelerometer, values).build())
        }
        shadowOf(Looper.getMainLooper()).idleFor(Duration.ofMillis(200))
    }

    @Test
    fun aFailingSetupDoesNotCrashTheApp() {
        app.registerActivityLifecycleCallbacks(Triggers({ error("sensor service broken") }, { true }, { a, t -> triggered += a to t }))
        Robolectric.buildActivity(Activity::class.java).setup().pause()
    }

    @Test
    fun withTwoScreensResumedPausingOneKeepsShakeWorkingOnTheOther() {
        app.registerActivityLifecycleCallbacks(Triggers({ true }, { false }, { a, t -> triggered += a to t }))
        val left = Robolectric.buildActivity(Activity::class.java).setup()
        val right = Robolectric.buildActivity(Activity::class.java).setup() // split screen: both resumed
        left.pause()

        shake()

        assertEquals(listOf(right.get() to Trigger.SHAKE), triggered)
    }

    @Test
    fun nothingListensOnceEveryScreenIsPaused() {
        app.registerActivityLifecycleCallbacks(Triggers({ true }, { false }, { a, t -> triggered += a to t }))
        Robolectric.buildActivity(Activity::class.java).setup().pause()

        shake()

        assertEquals(emptyList<Pair<Activity, Trigger>>(), triggered)
    }
}
