package com.tabura.android

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class TaburaDialogueModeTest {
    @Test
    fun blackSurfaceNeedsDialogueAndBlackIdleSurface() {
        val inactive = TaburaDialogueModePresentation(
            isActive = false,
            isRecording = false,
            isAwaitingAssistant = false,
            companionEnabled = true,
            idleSurface = "black",
            runtimeStateValue = "idle",
        )
        assertFalse(inactive.usesBlackScreen)
        assertFalse(inactive.keepScreenAwake)

        val active = TaburaDialogueModePresentation(
            isActive = true,
            isRecording = false,
            isAwaitingAssistant = false,
            companionEnabled = true,
            idleSurface = "black",
            runtimeStateValue = "idle",
        )
        assertTrue(active.usesBlackScreen)
        assertTrue(active.keepScreenAwake)
        assertEquals(TaburaDialogueRuntimeState.LISTENING, active.runtimeState)
    }

    @Test
    fun recordingAndAssistantStatesOverrideIdleCompanionState() {
        val recording = TaburaDialogueModePresentation(
            isActive = true,
            isRecording = true,
            isAwaitingAssistant = false,
            companionEnabled = true,
            idleSurface = "black",
            runtimeStateValue = "listening",
        )
        assertEquals(TaburaDialogueRuntimeState.RECORDING, recording.runtimeState)
        assertEquals("Tap to stop recording", recording.tapActionLabel)

        val thinking = TaburaDialogueModePresentation(
            isActive = true,
            isRecording = false,
            isAwaitingAssistant = true,
            companionEnabled = true,
            idleSurface = "black",
            runtimeStateValue = "listening",
        )
        assertEquals(TaburaDialogueRuntimeState.THINKING, thinking.runtimeState)
        assertEquals("Working", thinking.primaryLabel)
    }

    @Test
    fun explicitServerRuntimeStateIsPreserved() {
        val talking = TaburaDialogueModePresentation(
            isActive = true,
            isRecording = false,
            isAwaitingAssistant = false,
            companionEnabled = true,
            idleSurface = "robot",
            runtimeStateValue = "talking",
        )
        assertEquals(TaburaDialogueRuntimeState.TALKING, talking.runtimeState)
        assertEquals("Reply ready", talking.primaryLabel)
        assertEquals("Tap to record", talking.tapActionLabel)
    }
}
