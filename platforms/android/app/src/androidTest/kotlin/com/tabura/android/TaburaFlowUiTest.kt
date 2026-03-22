package com.tabura.android

import android.content.Intent
import androidx.test.core.app.ActivityScenario
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.uiautomator.By
import androidx.test.uiautomator.UiDevice
import androidx.test.uiautomator.UiObject2
import androidx.test.uiautomator.Until
import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class TaburaFlowUiTest {
    private data class FlowBundle(
        val platform: String,
        val flows: List<FlowDefinition>,
        val selectors: Map<String, String>,
    )

    private data class FlowDefinition(
        val name: String,
        val preconditionsJSON: String,
        val steps: List<FlowStep>,
    )

    private data class FlowStep(
        val action: String,
        val target: String?,
        val durationMs: Long,
        val platforms: Set<String>,
        val expect: JSONObject?,
    )

    @Test
    fun sharedFlowsExecuteOnAndroidHarness() {
        val bundle = loadBundle()
        assertEquals("android", bundle.platform)
        assertTrue(bundle.flows.isNotEmpty())

        bundle.flows.forEach { flow ->
            ActivityScenario.launch<MainActivity>(launchIntent(flow.preconditionsJSON)).use {
                runFlow(bundle.selectors, flow)
                println("android-ui PASS ${flow.name}")
            }
        }
    }

    private fun runFlow(selectors: Map<String, String>, flow: FlowDefinition) {
        val device = UiDevice.getInstance(InstrumentationRegistry.getInstrumentation())
        requireObject(device, "tabura_circle_dot")
        flow.steps.forEach { step ->
            if (step.platforms.isNotEmpty() && "android" !in step.platforms) {
                return@forEach
            }
            when (step.action) {
                "tap" -> requireObject(device, selectorFor(selectors, step.target)).click()
                "tap_outside" -> requireObject(device, selectorFor(selectors, "canvas_viewport")).click()
                "verify" -> {
                    if (!step.target.isNullOrBlank()) {
                        requireObject(device, selectorFor(selectors, step.target))
                    }
                }
                "wait" -> Thread.sleep(step.durationMs)
                else -> error("unsupported action ${step.action}")
            }
            assertExpectations(device, step.expect)
        }
    }

    private fun assertExpectations(device: UiDevice, expect: JSONObject?) {
        if (expect == null) {
            return
        }
        expect.optString("active_tool").takeIf { it.isNotBlank() }?.let {
            assertEquals(it, stateValue(device, "flow_state_active_tool"))
        }
        if (expect.has("session")) {
            assertEquals(expect.getString("session"), stateValue(device, "flow_state_session"))
        }
        if (expect.has("silent")) {
            assertEquals(expect.getBoolean("silent").toString(), stateValue(device, "flow_state_silent"))
        }
        expect.optString("tabura_circle").takeIf { it.isNotBlank() }?.let {
            assertEquals(it, stateValue(device, "flow_state_tabura_circle"))
        }
        expect.optString("dot_inner_icon").takeIf { it.isNotBlank() }?.let {
            assertEquals(it, stateValue(device, "flow_state_dot_inner_icon"))
        }
        expect.optString("indicator_state").takeIf { it.isNotBlank() }?.let {
            assertEquals(it, stateValue(device, "flow_state_indicator_state"))
        }
        expect.optString("body_class_contains").takeIf { it.isNotBlank() }?.let {
            assertTrue(
                "expected body_class to contain $it",
                stateValue(device, "flow_state_body_class").contains(it),
            )
        }
        expect.optString("cursor_class").takeIf { it.isNotBlank() }?.let {
            assertEquals(it, stateValue(device, "flow_state_cursor_class"))
        }
    }

    private fun stateValue(device: UiDevice, id: String): String {
        return requireObject(device, id).text.orEmpty()
    }

    private fun launchIntent(preconditionsJSON: String): Intent {
        return Intent(ApplicationProvider.getApplicationContext(), MainActivity::class.java).apply {
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TASK)
            putExtra("tabura.flow_harness", true)
            putExtra("tabura.flow_preconditions_json", preconditionsJSON)
        }
    }

    private fun selectorFor(selectors: Map<String, String>, logicalTarget: String?): String {
        require(!logicalTarget.isNullOrBlank()) { "missing target" }
        return selectors[logicalTarget] ?: logicalTarget
    }

    private fun requireObject(device: UiDevice, contentDescription: String): UiObject2 {
        val selector = By.desc(contentDescription)
        device.wait(Until.hasObject(selector), 5_000)
        return device.findObject(selector)
            ?: error("missing object with content description $contentDescription")
    }

    private fun loadBundle(): FlowBundle {
        val context = InstrumentationRegistry.getInstrumentation().context
        val raw = context.assets.open("flow-fixtures.json").bufferedReader().use { it.readText() }
        val json = JSONObject(raw)
        val flows = mutableListOf<FlowDefinition>()
        val flowArray = json.getJSONArray("flows")
        for (index in 0 until flowArray.length()) {
            val flow = flowArray.getJSONObject(index)
            val preconditions = flow.optJSONObject("preconditions") ?: JSONObject()
            flows += FlowDefinition(
                name = flow.getString("name"),
                preconditionsJSON = preconditions.toString(),
                steps = parseSteps(flow.getJSONArray("steps")),
            )
        }
        return FlowBundle(
            platform = json.getString("platform"),
            flows = flows,
            selectors = jsonObjectToStringMap(json.getJSONObject("selectors")),
        )
    }

    private fun parseSteps(array: JSONArray): List<FlowStep> {
        val out = mutableListOf<FlowStep>()
        for (index in 0 until array.length()) {
            val step = array.getJSONObject(index)
            out += FlowStep(
                action = step.getString("action"),
                target = step.optString("target").ifBlank { null },
                durationMs = step.optLong("duration_ms", 0L),
                platforms = jsonArrayToStringSet(step.optJSONArray("platforms")),
                expect = step.optJSONObject("expect"),
            )
        }
        return out
    }

    private fun jsonObjectToStringMap(json: JSONObject): Map<String, String> {
        val out = linkedMapOf<String, String>()
        val keys = json.keys()
        while (keys.hasNext()) {
            val key = keys.next()
            out[key] = json.getString(key)
        }
        return out
    }

    private fun jsonArrayToStringSet(array: JSONArray?): Set<String> {
        if (array == null) {
            return emptySet()
        }
        val out = linkedSetOf<String>()
        for (index in 0 until array.length()) {
            out += array.getString(index)
        }
        return out
    }
}
