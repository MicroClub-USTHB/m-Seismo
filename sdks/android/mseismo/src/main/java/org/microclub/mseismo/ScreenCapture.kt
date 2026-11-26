package org.microclub.mseismo

import android.annotation.TargetApi
import android.app.Activity
import android.graphics.Bitmap
import android.graphics.Canvas
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.util.Log
import android.view.PixelCopy
import android.view.SurfaceView
import android.view.View
import android.view.ViewGroup
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
            val main = Handler(Looper.getMainLooper())
            PixelCopy.request(activity.window, bitmap, { result ->
                if (result != PixelCopy.SUCCESS) return@request done(null)
                withSurfaces(view, bitmap, main, done)
            }, main)
        } else {
            view.draw(Canvas(bitmap))
            done(bitmap)
        }
    } catch (t: Throwable) {
        Log.w(MSeismo.TAG, "screen capture failed", t)
        done(null)
    }
}

// A SurfaceView has its own surface, which the window copy leaves empty:
// Flutter, maps, video and games draw there. Each one is copied on its own
// and drawn under the window, as on screen.
@TargetApi(Build.VERSION_CODES.O)
private fun withSurfaces(root: View, window: Bitmap, main: Handler, done: (Bitmap?) -> Unit) {
    val surfaces = ArrayList<SurfaceView>().also { collectSurfaces(root, it) }
    if (surfaces.isEmpty()) return done(window)

    val screen = Bitmap.createBitmap(window.width, window.height, Bitmap.Config.ARGB_8888)
    val canvas = Canvas(screen)
    val rootAt = IntArray(2).also { root.getLocationInWindow(it) }

    fun copy(index: Int) {
        if (index == surfaces.size) {
            canvas.drawBitmap(window, 0f, 0f, null)
            window.recycle()
            return done(screen)
        }
        val surface = surfaces[index]
        val part = Bitmap.createBitmap(surface.width, surface.height, Bitmap.Config.ARGB_8888)
        try {
            PixelCopy.request(surface, part, { result ->
                if (result == PixelCopy.SUCCESS) {
                    val at = IntArray(2).also { surface.getLocationInWindow(it) }
                    canvas.drawBitmap(part, (at[0] - rootAt[0]).toFloat(), (at[1] - rootAt[1]).toFloat(), null)
                }
                part.recycle()
                copy(index + 1)
            }, main)
        } catch (e: IllegalArgumentException) {
            // The surface was released meanwhile: keep the rest.
            part.recycle()
            copy(index + 1)
        }
    }
    copy(0)
}

private fun collectSurfaces(view: View, into: MutableList<SurfaceView>) {
    if (!view.isShown) return
    if (view is SurfaceView) {
        if (view.width > 0 && view.height > 0 && view.holder.surface?.isValid == true) into += view
        return
    }
    if (view is ViewGroup) for (i in 0 until view.childCount) collectSurfaces(view.getChildAt(i), into)
}

internal fun Bitmap.toJpeg(): ByteArray =
    ByteArrayOutputStream().also { compress(Bitmap.CompressFormat.JPEG, 80, it) }.toByteArray()
