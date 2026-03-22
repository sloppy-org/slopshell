package com.tabura.android

import android.content.Context
import android.graphics.Color
import android.graphics.Rect
import android.util.AttributeSet
import android.view.View
import com.onyx.android.sdk.pen.RawInputCallback
import com.onyx.android.sdk.pen.TouchHelper
import com.onyx.android.sdk.pen.data.TouchPoint
import com.onyx.android.sdk.pen.data.TouchPointList

class TaburaBooxInkSurfaceView @JvmOverloads constructor(
    context: Context,
    attrs: AttributeSet? = null,
) : View(context, attrs) {
    private var touchHelper: TouchHelper? = null
    private val rawPoints = mutableListOf<TaburaInkPoint>()
    private var onCommit: (List<TaburaInkStroke>) -> Unit = {}

    private val callback = object : RawInputCallback() {
        override fun onBeginRawDrawing(active: Boolean, touchPoint: TouchPoint) {
            rawPoints.clear()
            rawPoints += touchPoint.toInkPoint()
            TaburaBooxEinkController.configureInkView(this@TaburaBooxInkSurfaceView)
        }

        override fun onEndRawDrawing(active: Boolean, touchPoint: TouchPoint) {
            rawPoints += touchPoint.toInkPoint()
            emitStroke()
        }

        override fun onRawDrawingTouchPointMoveReceived(touchPoint: TouchPoint) {
            rawPoints += touchPoint.toInkPoint()
        }

        override fun onRawDrawingTouchPointListReceived(touchPointList: TouchPointList) {
            rawPoints += touchPointList.toInkPoints()
        }

        override fun onBeginRawErasing(active: Boolean, touchPoint: TouchPoint) {
        }

        override fun onEndRawErasing(active: Boolean, touchPoint: TouchPoint) {
        }

        override fun onRawErasingTouchPointMoveReceived(touchPoint: TouchPoint) {
        }

        override fun onRawErasingTouchPointListReceived(touchPointList: TouchPointList) {
        }
    }

    init {
        setBackgroundColor(Color.TRANSPARENT)
        isClickable = true
        isFocusable = true
        addOnLayoutChangeListener { _, left, top, right, bottom, oldLeft, oldTop, oldRight, oldBottom ->
            if (right - left != oldRight - oldLeft || bottom - top != oldBottom - oldTop) {
                restartRawDrawing()
            }
        }
    }

    fun setOnCommit(listener: (List<TaburaInkStroke>) -> Unit) {
        onCommit = listener
    }

    override fun onAttachedToWindow() {
        super.onAttachedToWindow()
        post { ensureTouchHelper() }
    }

    override fun onDetachedFromWindow() {
        shutdownTouchHelper()
        super.onDetachedFromWindow()
    }

    override fun onWindowVisibilityChanged(visibility: Int) {
        super.onWindowVisibilityChanged(visibility)
        touchHelper?.setRawDrawingEnabled(visibility == VISIBLE)
    }

    private fun restartRawDrawing() {
        if (!isAttachedToWindow) {
            return
        }
        shutdownTouchHelper()
        post { ensureTouchHelper() }
    }

    private fun ensureTouchHelper() {
        if (touchHelper != null || width == 0 || height == 0) {
            return
        }
        val limit = Rect()
        getLocalVisibleRect(limit)
        val helper = TouchHelper.create(this, callback)
        helper.setStrokeWidth(DEFAULT_STROKE_WIDTH)
        helper.setRawInputReaderEnable(true)
        helper.setLimitRect(limit, emptyList<Rect>())
        helper.openRawDrawing()
        helper.setRawDrawingEnabled(true)
        if (windowVisibility != VISIBLE) {
            helper.setRawDrawingEnabled(false)
        }
        touchHelper = helper
        TaburaBooxEinkController.configureInkView(this)
    }

    private fun shutdownTouchHelper() {
        val helper = touchHelper ?: return
        runCatching { helper.setRawDrawingEnabled(false) }
        runCatching { helper.closeRawDrawing() }
        touchHelper = null
    }

    private fun emitStroke() {
        val points = rawPoints
            .distinctBy { listOf(it.x, it.y, it.timestampMs) }
            .toList()
        rawPoints.clear()
        if (points.isEmpty()) {
            return
        }
        onCommit(
            listOf(
                TaburaInkStroke(
                    pointerType = "stylus",
                    width = points.maxOf { it.pressure.coerceAtLeast(1f) } * 2.4f,
                    points = points,
                )
            )
        )
    }

    private fun TouchPointList.toInkPoints(): List<TaburaInkPoint> {
        val points = readIterable(this, "getPoints", "points")
        return points
            ?.mapNotNull { point -> (point as? TouchPoint)?.toInkPoint() }
            .orEmpty()
    }

    private fun TouchPoint.toInkPoint(): TaburaInkPoint {
        val timestamp = readLong(this, "getTimestamp", "timestamp", "getEventTime", "eventTime")
            ?: System.currentTimeMillis()
        return TaburaInkPoint(
            x = readFloat(this, "getX", "x") ?: 0f,
            y = readFloat(this, "getY", "y") ?: 0f,
            pressure = readFloat(this, "getPressure", "pressure") ?: 1f,
            tiltX = readFloat(this, "getTiltX", "tiltX") ?: 0f,
            tiltY = readFloat(this, "getTiltY", "tiltY") ?: 0f,
            roll = readFloat(this, "getOrientation", "orientation") ?: 0f,
            timestampMs = timestamp,
        )
    }

    private fun readFloat(target: Any, vararg accessors: String): Float? {
        return readNumber(target, *accessors)?.toFloat()
    }

    private fun readLong(target: Any, vararg accessors: String): Long? {
        return readNumber(target, *accessors)?.toLong()
    }

    private fun readNumber(target: Any, vararg accessors: String): Number? {
        for (accessor in accessors) {
            runCatching {
                target.javaClass.methods
                    .firstOrNull { it.name == accessor && it.parameterCount == 0 }
                    ?.invoke(target) as? Number
            }.getOrNull()?.let { return it }
            runCatching {
                target.javaClass.getField(accessor).get(target) as? Number
            }.getOrNull()?.let { return it }
            runCatching {
                target.javaClass.getDeclaredField(accessor).apply {
                    isAccessible = true
                }.get(target) as? Number
            }.getOrNull()?.let { return it }
        }
        return null
    }

    private fun readIterable(target: Any, vararg accessors: String): Iterable<*>? {
        for (accessor in accessors) {
            runCatching {
                target.javaClass.methods
                    .firstOrNull { it.name == accessor && it.parameterCount == 0 }
                    ?.invoke(target) as? Iterable<*>
            }.getOrNull()?.let { return it }
            runCatching {
                target.javaClass.getField(accessor).get(target) as? Iterable<*>
            }.getOrNull()?.let { return it }
            runCatching {
                target.javaClass.getDeclaredField(accessor).apply {
                    isAccessible = true
                }.get(target) as? Iterable<*>
            }.getOrNull()?.let { return it }
        }
        return null
    }

    private companion object {
        const val DEFAULT_STROKE_WIDTH = 3.0f
    }
}
