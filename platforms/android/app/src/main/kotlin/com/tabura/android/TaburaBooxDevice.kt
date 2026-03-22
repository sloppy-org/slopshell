package com.tabura.android

import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import android.view.View
import android.webkit.WebView

data class TaburaDisplayProfile(
    val isBoox: Boolean,
)

fun detectTaburaDisplayProfile(context: Context): TaburaDisplayProfile {
    return TaburaDisplayProfile(isBoox = isBooxDevice(context))
}

private fun isBooxDevice(context: Context): Boolean {
    return shouldTreatAsBooxDevice(
        manufacturer = Build.MANUFACTURER,
        hasOnyxSdkPackage = hasOnyxSdkPackage(context),
        hasTouchHelperClass = hasClass("com.onyx.android.sdk.pen.TouchHelper"),
    )
}

internal fun shouldTreatAsBooxDevice(
    manufacturer: String,
    hasOnyxSdkPackage: Boolean,
    hasTouchHelperClass: Boolean,
): Boolean {
    val normalizedManufacturer = manufacturer.trim().lowercase()
    return normalizedManufacturer == "onyx" ||
        hasOnyxSdkPackage ||
        hasTouchHelperClass
}

private fun hasOnyxSdkPackage(context: Context): Boolean {
    val packageManager = context.packageManager
    return if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
        runCatching {
            packageManager.getPackageInfo(
                "com.onyx.android.sdk",
                PackageManager.PackageInfoFlags.of(0),
            )
            true
        }.getOrDefault(false)
    } else {
        @Suppress("DEPRECATION")
        runCatching {
            packageManager.getPackageInfo("com.onyx.android.sdk", 0)
            true
        }.getOrDefault(false)
    }
}

private fun hasClass(name: String): Boolean {
    return runCatching {
        Class.forName(name)
        true
    }.getOrDefault(false)
}

object TaburaBooxEinkController {
    fun configureInkView(view: View) {
        setViewDefaultUpdateMode(view, "DU")
    }

    fun configureContentView(view: View) {
        setViewDefaultUpdateMode(view, "GC16", "GC")
    }

    fun refreshContentView(view: View) {
        if (invokeController("applyGCOnce", arrayOf(View::class.java), arrayOf(view))) {
            return
        }
        if (invokeController("applyGCOnce", emptyArray<Class<*>>(), emptyArray<Any>())) {
            return
        }
        invalidate(view, "GC16", "GC")
    }

    fun setWebViewContrastOptimize(view: WebView, enabled: Boolean) {
        invokeController(
            "setWebViewContrastOptimize",
            arrayOf(WebView::class.java, Boolean::class.javaPrimitiveType ?: Boolean::class.java),
            arrayOf(view, enabled),
        )
    }

    private fun setViewDefaultUpdateMode(view: View, vararg modeNames: String) {
        val mode = resolveUpdateMode(*modeNames) ?: return
        invokeController(
            "setViewDefaultUpdateMode",
            arrayOf(View::class.java, mode.javaClass),
            arrayOf(view, mode),
        )
    }

    private fun invalidate(view: View, vararg modeNames: String) {
        val mode = resolveUpdateMode(*modeNames) ?: return
        invokeController(
            "invalidate",
            arrayOf(View::class.java, mode.javaClass),
            arrayOf(view, mode),
        )
    }

    private fun resolveUpdateMode(vararg modeNames: String): Any? {
        val enumClass = runCatching {
            Class.forName("com.onyx.android.sdk.api.device.epd.EpdController\$UpdateMode")
        }.getOrNull() ?: return null
        val constants = enumClass.enumConstants ?: return null
        for (modeName in modeNames) {
            constants.firstOrNull { (it as Enum<*>).name == modeName }?.let { return it }
        }
        return null
    }

    private fun invokeController(
        methodName: String,
        parameterTypes: Array<Class<*>>,
        args: Array<Any>,
    ): Boolean {
        val controllerClass = runCatching {
            Class.forName("com.onyx.android.sdk.api.device.epd.EpdController")
        }.getOrNull() ?: return false
        return runCatching {
            controllerClass.getMethod(methodName, *parameterTypes).invoke(null, *args)
            true
        }.getOrDefault(false)
    }
}
