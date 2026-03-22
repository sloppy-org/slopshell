import XCTest
@testable import TaburaIOSModels

final class TaburaDialogueModeTests: XCTestCase {
    func testBlackSurfaceNeedsDialogueAndBlackIdleSurface() {
        let inactive = TaburaDialogueModePresentation(
            isActive: false,
            isRecording: false,
            isAwaitingAssistant: false,
            companionEnabled: true,
            idleSurface: "black",
            runtimeState: "idle"
        )
        XCTAssertFalse(inactive.usesBlackScreen)
        XCTAssertFalse(inactive.keepScreenAwake)

        let active = TaburaDialogueModePresentation(
            isActive: true,
            isRecording: false,
            isAwaitingAssistant: false,
            companionEnabled: true,
            idleSurface: "black",
            runtimeState: "idle"
        )
        XCTAssertTrue(active.usesBlackScreen)
        XCTAssertTrue(active.keepScreenAwake)
        XCTAssertEqual(active.runtimeState, .listening)
    }

    func testRecordingAndAssistantStatesOverrideCompanionIdle() {
        let recording = TaburaDialogueModePresentation(
            isActive: true,
            isRecording: true,
            isAwaitingAssistant: false,
            companionEnabled: true,
            idleSurface: "black",
            runtimeState: "listening"
        )
        XCTAssertEqual(recording.runtimeState, .recording)
        XCTAssertEqual(recording.primaryLabel, "Recording")
        XCTAssertEqual(recording.tapActionLabel, "Tap to stop recording")

        let thinking = TaburaDialogueModePresentation(
            isActive: true,
            isRecording: false,
            isAwaitingAssistant: true,
            companionEnabled: true,
            idleSurface: "black",
            runtimeState: "listening"
        )
        XCTAssertEqual(thinking.runtimeState, .thinking)
        XCTAssertEqual(thinking.primaryLabel, "Working")
        XCTAssertEqual(thinking.tapActionLabel, "Waiting for Tabura")
    }

    func testCompanionRuntimeStateFallsBackToListeningDuringDialogue() {
        let talking = TaburaDialogueModePresentation(
            isActive: true,
            isRecording: false,
            isAwaitingAssistant: false,
            companionEnabled: true,
            idleSurface: "robot",
            runtimeState: "talking"
        )
        XCTAssertEqual(talking.runtimeState, .talking)
        XCTAssertEqual(talking.primaryLabel, "Reply ready")

        let defaulted = TaburaDialogueModePresentation(
            isActive: true,
            isRecording: false,
            isAwaitingAssistant: false,
            companionEnabled: false,
            idleSurface: "robot",
            runtimeState: "idle"
        )
        XCTAssertEqual(defaulted.runtimeState, .listening)
        XCTAssertEqual(defaulted.secondaryLabel, "Tap anywhere on the dialogue surface to record.")
    }
}
