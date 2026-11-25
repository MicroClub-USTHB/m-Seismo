package org.microclub.mseismo

import android.app.Activity
import android.graphics.Bitmap
import android.graphics.Canvas
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.util.Log
import android.view.PixelCopy
import android.view.WindowManager
import java.io.ByteArrayOutputStream

internal fun captureScreen(activity: Activity, done: (Bitmap?) -> Unit) {
    try {
        // Secret screens (card, password): never captured.
        if (activity.window.attributes.flags and WindowManager.LayoutParams.FLAG_SECURE != 0) return done(null)
        val view = activity.window.decorView
        if (view.width == 0 || view.height == 0) return done(null)
        val bitmap = Bitmap.createBitmap(view.width, view.height, Bitmap.Config.ARGB_8888)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            PixelCopy.request(activity.window, bitmap, { result ->
                done(bitmap.takeIf { result == PixelCopy.SUCCESS })
            }, Handler(Looper.getMainLooper()))
        } else {
            view.draw(Canvas(bitmap))
            done(bitmap)
        }
    } catch (t: Throwable) {
        Log.w(MSeismo.TAG, "screen capture failed", t)
        done(null)
    }
}

internal fun Bitmap.toJpeg(): ByteArray =
    ByteArrayOutputStream().also { compress(Bitmap.CompressFormat.JPEG, 80, it) }.toByteArray()
