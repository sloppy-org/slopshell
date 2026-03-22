package com.tabura.android

import android.content.Context
import android.graphics.Color
import android.util.AttributeSet
import android.util.SparseArray
import android.view.MotionEvent
import android.view.View
import android.widget.FrameLayout
import androidx.ink.brush.Brush
import androidx.ink.brush.StockBrushes
import androidx.ink.authoring.InProgressStrokeId
import androidx.ink.authoring.InProgressStrokesFinishedListener
import androidx.ink.authoring.InProgressStrokesView
import androidx.ink.strokes.Stroke
import androidx.input.motionprediction.MotionEventPredictor

class TaburaInkSurfaceView @JvmOverloads constructor(
    context: Context,
    attrs: AttributeSet? = null,
) : FrameLayout(context, attrs), View.OnTouchListener, InProgressStrokesFinishedListener {
    private val predictor: MotionEventPredictor? = MotionEventPredictor.newInstance(this)
    private val inProgressStrokesView = InProgressStrokesView(context)
    private val pointerToStrokeId = SparseArray<InProgressStrokeId>()
    private val pointerToPoints = mutableMapOf<Int, MutableList<TaburaInkPoint>>()
    private var onCommit: (List<TaburaInkStroke>) -> Unit = {}
    private val strokeBrush = Brush.createWithColorIntArgb(
        StockBrushes.pressurePen(),
        Color.BLACK,
        3.0f,
        0.1f,
    )

    init {
        setBackgroundColor(Color.TRANSPARENT)
        isClickable = true
        isFocusable = true
        addView(
            inProgressStrokesView,
            LayoutParams(LayoutParams.MATCH_PARENT, LayoutParams.MATCH_PARENT),
        )
        inProgressStrokesView.addFinishedStrokesListener(this)
        setOnTouchListener(this)
    }

    fun setOnCommit(listener: (List<TaburaInkStroke>) -> Unit) {
        onCommit = listener
    }

    override fun onTouch(v: View, event: MotionEvent): Boolean {
        predictor?.record(event)
        return when (event.actionMasked) {
            MotionEvent.ACTION_DOWN,
            MotionEvent.ACTION_POINTER_DOWN -> handleDown(event)
            MotionEvent.ACTION_MOVE -> handleMove(event)
            MotionEvent.ACTION_UP,
            MotionEvent.ACTION_POINTER_UP -> handleUp(event)
            MotionEvent.ACTION_CANCEL -> handleCancel(event)
            else -> false
        }
    }

    override fun onStrokesFinished(strokes: Map<InProgressStrokeId, Stroke>) {
        inProgressStrokesView.removeFinishedStrokes(strokes.keys)
    }

    private fun handleDown(event: MotionEvent): Boolean {
        val pointerIndex = event.actionIndex
        val pointerId = event.getPointerId(pointerIndex)
        requestUnbufferedDispatch(event)
        pointerToStrokeId.put(pointerId, inProgressStrokesView.startStroke(event, pointerId, strokeBrush))
        pointerToPoints[pointerId] = mutableListOf()
        collectSamples(event, pointerIndex, pointerId)
        return true
    }

    private fun handleMove(event: MotionEvent): Boolean {
        val predictedEvent = predictor?.predict()
        try {
            for (pointerIndex in 0 until event.pointerCount) {
                val pointerId = event.getPointerId(pointerIndex)
                val strokeId = pointerToStrokeId[pointerId] ?: continue
                collectSamples(event, pointerIndex, pointerId)
                inProgressStrokesView.addToStroke(event, pointerId, strokeId, predictedEvent)
            }
        } finally {
            predictedEvent?.recycle()
        }
        return true
    }

    private fun handleUp(event: MotionEvent): Boolean {
        val pointerIndex = event.actionIndex
        val pointerId = event.getPointerId(pointerIndex)
        val strokeId = pointerToStrokeId[pointerId] ?: return false
        collectSamples(event, pointerIndex, pointerId)
        inProgressStrokesView.finishStroke(event, pointerId, strokeId)
        emitStroke(pointerId, event.getToolType(pointerIndex))
        pointerToStrokeId.remove(pointerId)
        return true
    }

    private fun handleCancel(event: MotionEvent): Boolean {
        for (index in 0 until pointerToStrokeId.size()) {
            val pointerId = pointerToStrokeId.keyAt(index)
            val strokeId = pointerToStrokeId.valueAt(index)
            inProgressStrokesView.cancelStroke(strokeId, event)
        }
        pointerToStrokeId.clear()
        pointerToPoints.clear()
        return true
    }

    private fun collectSamples(event: MotionEvent, pointerIndex: Int, pointerId: Int) {
        val points = pointerToPoints.getOrPut(pointerId) { mutableListOf() }
        for (historyIndex in 0 until event.historySize) {
            points += pointFromEvent(
                x = event.getHistoricalX(pointerIndex, historyIndex),
                y = event.getHistoricalY(pointerIndex, historyIndex),
                pressure = event.getHistoricalPressure(pointerIndex, historyIndex),
                tilt = event.getHistoricalAxisValue(MotionEvent.AXIS_TILT, pointerIndex, historyIndex),
                orientation = event.getHistoricalAxisValue(MotionEvent.AXIS_ORIENTATION, pointerIndex, historyIndex),
                timestampMs = event.getHistoricalEventTime(historyIndex),
            )
        }
        points += pointFromEvent(
            x = event.getX(pointerIndex),
            y = event.getY(pointerIndex),
            pressure = event.getPressure(pointerIndex),
            tilt = event.getAxisValue(MotionEvent.AXIS_TILT, pointerIndex),
            orientation = event.getAxisValue(MotionEvent.AXIS_ORIENTATION, pointerIndex),
            timestampMs = event.eventTime,
        )
    }

    private fun pointFromEvent(
        x: Float,
        y: Float,
        pressure: Float,
        tilt: Float,
        orientation: Float,
        timestampMs: Long,
    ): TaburaInkPoint {
        return TaburaInkPoint(
            x = x,
            y = y,
            pressure = pressure,
            tiltX = tilt,
            tiltY = 0f,
            roll = orientation,
            timestampMs = timestampMs,
        )
    }

    private fun emitStroke(pointerId: Int, toolType: Int) {
        val points = pointerToPoints.remove(pointerId)?.distinctBy { listOf(it.x, it.y, it.timestampMs) }.orEmpty()
        if (points.isEmpty()) {
            return
        }
        onCommit(
            listOf(
                TaburaInkStroke(
                    pointerType = when (toolType) {
                        MotionEvent.TOOL_TYPE_STYLUS -> "stylus"
                        MotionEvent.TOOL_TYPE_FINGER -> "touch"
                        MotionEvent.TOOL_TYPE_MOUSE -> "mouse"
                        else -> "unknown"
                    },
                    width = points.maxOf { it.pressure.coerceAtLeast(1f) } * 2.4f,
                    points = points,
                )
            )
        )
    }
}
