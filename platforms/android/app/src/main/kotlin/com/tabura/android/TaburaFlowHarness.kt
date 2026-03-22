package com.tabura.android

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import org.json.JSONObject

internal data class TaburaFlowHarnessPreconditions(
    val tool: String = "pointer",
    val session: String = "none",
    val silent: Boolean = false,
    val indicatorState: String = "",
)

internal fun parseTaburaFlowHarnessPreconditions(raw: String?): TaburaFlowHarnessPreconditions {
    val trimmed = raw?.trim().orEmpty()
    if (trimmed.isEmpty()) {
        return TaburaFlowHarnessPreconditions()
    }
    return runCatching {
        val json = JSONObject(trimmed)
        TaburaFlowHarnessPreconditions(
            tool = json.optString("tool", "pointer"),
            session = json.optString("session", "none"),
            silent = json.optBoolean("silent", false),
            indicatorState = json.optString("indicator_state", ""),
        )
    }.getOrDefault(TaburaFlowHarnessPreconditions())
}

private data class TaburaFlowHarnessState(
    val activeTool: String = "pointer",
    val session: String = "none",
    val silent: Boolean = false,
    val circleExpanded: Boolean = false,
    val indicatorOverride: String = "",
) {
    val taburaCircle: String
        get() = if (circleExpanded) "expanded" else "collapsed"

    val dotInnerIcon: String
        get() = when (activeTool) {
            "highlight" -> "marker"
            "ink" -> "pen_nib"
            "text_note" -> "sticky_note"
            "prompt" -> "mic"
            else -> "arrow"
        }

    val indicatorState: String
        get() = when {
            indicatorOverride.isNotBlank() -> indicatorOverride
            session == "dialogue" -> "listening"
            session == "meeting" -> "paused"
            else -> "idle"
        }

    val bodyClass: String
        get() = listOf(
            "tool-$activeTool",
            "session-$session",
            "indicator-$indicatorState",
            if (silent) "silent-on" else "silent-off",
            if (circleExpanded) "circle-expanded" else "circle-collapsed",
        ).joinToString(" ")

    val cursorClass: String
        get() = "tool-$activeTool"
}

private fun TaburaFlowHarnessPreconditions.toState(): TaburaFlowHarnessState {
    return TaburaFlowHarnessState(
        activeTool = when (tool) {
            "highlight", "ink", "text_note", "prompt" -> tool
            else -> "pointer"
        },
        session = when (session) {
            "dialogue", "meeting" -> session
            else -> "none"
        },
        silent = silent,
        circleExpanded = false,
        indicatorOverride = when (indicatorState) {
            "idle", "listening", "paused", "recording", "working" -> indicatorState
            else -> ""
        },
    )
}

private fun Modifier.flowHarnessId(id: String): Modifier {
    return semantics {
        contentDescription = id
    }
}

@Composable
internal fun TaburaFlowHarnessScreen(preconditions: TaburaFlowHarnessPreconditions) {
    var state by remember(preconditions) {
        mutableStateOf(preconditions.toState())
    }

    Column(
        modifier = Modifier.padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Text("Native Flow Harness", style = MaterialTheme.typography.headlineMedium)
        Row(horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.CenterVertically) {
            Box(
                modifier = Modifier
                    .size(72.dp)
                    .background(Color.Black, CircleShape)
                    .border(2.dp, Color.DarkGray, CircleShape)
                    .clickable { state = state.copy(circleExpanded = !state.circleExpanded) }
                    .flowHarnessId("tabura_circle_dot"),
                contentAlignment = Alignment.Center,
            ) {
                Text(state.dotInnerIcon, color = Color.White)
            }
            Button(
                onClick = {
                    state = state.copy(session = "none", indicatorOverride = "")
                },
                modifier = Modifier.flowHarnessId("indicator_border"),
            ) {
                Text(state.indicatorState)
            }
        }

        if (state.circleExpanded) {
            FlowRow(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                FlowHarnessSegment("tabura_circle_pointer", "Pointer") {
                    state = state.copy(activeTool = "pointer")
                }
                FlowHarnessSegment("tabura_circle_highlight", "Highlight") {
                    state = state.copy(activeTool = "highlight")
                }
                FlowHarnessSegment("tabura_circle_ink", "Ink") {
                    state = state.copy(activeTool = "ink")
                }
                FlowHarnessSegment("tabura_circle_text_note", "Text") {
                    state = state.copy(activeTool = "text_note")
                }
                FlowHarnessSegment("tabura_circle_prompt", "Prompt") {
                    state = state.copy(activeTool = "prompt")
                }
                FlowHarnessSegment("tabura_circle_dialogue", "Dialogue") {
                    state = state.copy(
                        session = if (state.session == "dialogue") "none" else "dialogue",
                        indicatorOverride = "",
                    )
                }
                FlowHarnessSegment("tabura_circle_meeting", "Meeting") {
                    state = state.copy(
                        session = if (state.session == "meeting") "none" else "meeting",
                        indicatorOverride = "",
                    )
                }
                FlowHarnessSegment("tabura_circle_silent", "Silent") {
                    state = state.copy(silent = !state.silent)
                }
            }
        }

        Box(
            modifier = Modifier
                .fillMaxWidth()
                .height(180.dp)
                .background(Color.White, RoundedCornerShape(16.dp))
                .border(1.dp, Color.LightGray, RoundedCornerShape(16.dp))
                .clickable {
                    state = state.copy(circleExpanded = false)
                }
                .flowHarnessId("canvas_viewport"),
            contentAlignment = Alignment.Center,
        ) {
            Text("Canvas")
        }

        Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            FlowHarnessStateValue("flow_state_active_tool", state.activeTool)
            FlowHarnessStateValue("flow_state_session", state.session)
            FlowHarnessStateValue("flow_state_silent", state.silent.toString())
            FlowHarnessStateValue("flow_state_tabura_circle", state.taburaCircle)
            FlowHarnessStateValue("flow_state_dot_inner_icon", state.dotInnerIcon)
            FlowHarnessStateValue("flow_state_indicator_state", state.indicatorState)
            FlowHarnessStateValue("flow_state_body_class", state.bodyClass)
            FlowHarnessStateValue("flow_state_cursor_class", state.cursorClass)
        }
    }
}

@Composable
private fun FlowHarnessSegment(id: String, label: String, onClick: () -> Unit) {
    Button(
        onClick = onClick,
        modifier = Modifier.flowHarnessId(id),
    ) {
        Text(label)
    }
}

@Composable
private fun FlowHarnessStateValue(id: String, value: String) {
    Text(
        text = value,
        modifier = Modifier.flowHarnessId(id),
    )
}
