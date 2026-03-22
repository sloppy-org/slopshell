import Foundation

struct TaburaLoginRequest: Encodable {
    let password: String
}

struct TaburaWorkspaceListResponse: Decodable {
    let ok: Bool
    let activeWorkspaceID: String
    let workspaces: [TaburaWorkspace]

    private enum CodingKeys: String, CodingKey {
        case ok
        case activeWorkspaceID = "active_workspace_id"
        case workspaces
    }
}

struct TaburaWorkspace: Decodable, Identifiable, Hashable {
    let id: String
    let name: String
    let rootPath: String
    let chatSessionID: String
    let canvasSessionID: String

    private enum CodingKeys: String, CodingKey {
        case id
        case name
        case rootPath = "root_path"
        case chatSessionID = "chat_session_id"
        case canvasSessionID = "canvas_session_id"
    }
}

struct TaburaChatHistoryResponse: Decodable {
    let messages: [TaburaPersistedMessage]
}

struct TaburaPersistedMessage: Decodable, Identifiable {
    let id: Int64
    let role: String
    let contentMarkdown: String
    let contentPlain: String

    private enum CodingKeys: String, CodingKey {
        case id
        case role
        case contentMarkdown = "content_markdown"
        case contentPlain = "content_plain"
    }

    var content: String {
        if contentMarkdown.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty == false {
            return contentMarkdown
        }
        return contentPlain
    }
}

struct TaburaChatSendRequest: Encodable {
    let text: String
    let outputMode: String

    private enum CodingKeys: String, CodingKey {
        case text
        case outputMode = "output_mode"
    }
}

struct TaburaRenderedMessage: Identifiable, Equatable {
    let id: String
    let role: String
    let text: String
    let html: String
}

struct TaburaCanvasArtifact: Equatable {
    let kind: String
    let title: String
    let html: String
    let text: String
}

struct TaburaCanvasSnapshotResponse: Decodable {
    let event: TaburaCanvasEventPayload?
}

struct TaburaCanvasEventPayload: Decodable {
    let kind: String?
    let title: String?
    let html: String?
    let text: String?
    let markdownOrText: String?
    let path: String?

    private enum CodingKeys: String, CodingKey {
        case kind
        case title
        case html
        case text
        case markdownOrText = "markdown_or_text"
        case path
    }
}

struct TaburaChatEventPayload: Decodable {
    let type: String
    let turnID: String?
    let role: String?
    let message: String?
    let markdown: String?
    let html: String?
    let error: String?
    let text: String?
    let reason: String?
    let state: String?
    let workspacePath: String?
    let actionType: String?

    private enum CodingKeys: String, CodingKey {
        case type
        case turnID = "turn_id"
        case role
        case message
        case markdown
        case html
        case error
        case text
        case reason
        case state
        case workspacePath = "workspace_path"
        case action
    }

    private struct ActionPayload: Decodable {
        let type: String?
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        type = try container.decode(String.self, forKey: .type)
        turnID = try container.decodeIfPresent(String.self, forKey: .turnID)
        role = try container.decodeIfPresent(String.self, forKey: .role)
        message = try container.decodeIfPresent(String.self, forKey: .message)
        markdown = try container.decodeIfPresent(String.self, forKey: .markdown)
        html = try container.decodeIfPresent(String.self, forKey: .html)
        error = try container.decodeIfPresent(String.self, forKey: .error)
        text = try container.decodeIfPresent(String.self, forKey: .text)
        reason = try container.decodeIfPresent(String.self, forKey: .reason)
        state = try container.decodeIfPresent(String.self, forKey: .state)
        workspacePath = try container.decodeIfPresent(String.self, forKey: .workspacePath)
        actionType = try container.decodeIfPresent(ActionPayload.self, forKey: .action)?.type
    }
}

struct TaburaCompanionConfig: Decodable {
    let companionEnabled: Bool
    let idleSurface: String

    private enum CodingKeys: String, CodingKey {
        case companionEnabled = "companion_enabled"
        case idleSurface = "idle_surface"
    }
}

struct TaburaCompanionConfigPatch: Encodable {
    let companionEnabled: Bool?
    let idleSurface: String?

    private enum CodingKeys: String, CodingKey {
        case companionEnabled = "companion_enabled"
        case idleSurface = "idle_surface"
    }
}

struct TaburaCompanionStateResponse: Decodable {
    let companionEnabled: Bool
    let idleSurface: String
    let state: String
    let reason: String

    private enum CodingKeys: String, CodingKey {
        case companionEnabled = "companion_enabled"
        case idleSurface = "idle_surface"
        case state
        case reason
    }
}

struct TaburaLivePolicyRequest: Encodable {
    let policy: String
}

enum TaburaCompanionIdleSurface: String, Equatable {
    case robot
    case black

    init(raw: String) {
        self = raw.trimmingCharacters(in: .whitespacesAndNewlines).lowercased() == "black" ? .black : .robot
    }
}

enum TaburaDialogueRuntimeState: String, Equatable {
    case idle
    case listening
    case recording
    case thinking
    case talking
    case error

    init(raw: String) {
        switch raw.trimmingCharacters(in: .whitespacesAndNewlines).lowercased() {
        case "listening":
            self = .listening
        case "recording":
            self = .recording
        case "thinking":
            self = .thinking
        case "talking":
            self = .talking
        case "error":
            self = .error
        default:
            self = .idle
        }
    }
}

struct TaburaDialogueModePresentation: Equatable {
    let isActive: Bool
    let usesBlackScreen: Bool
    let keepScreenAwake: Bool
    let runtimeState: TaburaDialogueRuntimeState
    let primaryLabel: String
    let secondaryLabel: String
    let tapActionLabel: String

    init(
        isActive: Bool,
        isRecording: Bool,
        isAwaitingAssistant: Bool,
        companionEnabled: Bool,
        idleSurface: String,
        runtimeState: String
    ) {
        let normalizedSurface = TaburaCompanionIdleSurface(raw: idleSurface)
        let derivedState: TaburaDialogueRuntimeState
        if !isActive {
            derivedState = .idle
        } else if isRecording {
            derivedState = .recording
        } else if isAwaitingAssistant {
            derivedState = .thinking
        } else {
            let serverState = TaburaDialogueRuntimeState(raw: runtimeState)
            derivedState = serverState == .idle ? .listening : serverState
        }

        self.isActive = isActive
        usesBlackScreen = isActive && normalizedSurface == .black
        keepScreenAwake = usesBlackScreen
        self.runtimeState = derivedState

        switch derivedState {
        case .idle:
            primaryLabel = companionEnabled ? "Ready" : "Disconnected"
            secondaryLabel = "Start dialogue to hand the screen to voice."
            tapActionLabel = "Start dialogue"
        case .listening:
            primaryLabel = "Listening"
            secondaryLabel = "Tap anywhere on the dialogue surface to record."
            tapActionLabel = "Tap to record"
        case .recording:
            primaryLabel = "Recording"
            secondaryLabel = "Tap again to stop and send audio."
            tapActionLabel = "Tap to stop recording"
        case .thinking:
            primaryLabel = "Working"
            secondaryLabel = "Tabura is processing your last recording."
            tapActionLabel = "Waiting for Tabura"
        case .talking:
            primaryLabel = "Reply ready"
            secondaryLabel = "Tap to interrupt and start a new recording."
            tapActionLabel = "Tap to record"
        case .error:
            primaryLabel = "Attention needed"
            secondaryLabel = "Check the connection banner for the latest error."
            tapActionLabel = "Tap to retry"
        }
    }
}

struct TaburaAudioCaptureMessage: Encodable {
    let type: String
    let mimeType: String?
    let data: String?

    private enum CodingKeys: String, CodingKey {
        case type
        case mimeType = "mime_type"
        case data
    }
}

struct TaburaInkPoint: Encodable {
    let x: Double
    let y: Double
    let pressure: Double
    let tiltX: Double
    let tiltY: Double
    let roll: Double
    let timestampMS: Double

    private enum CodingKeys: String, CodingKey {
        case x
        case y
        case pressure
        case tiltX = "tilt_x"
        case tiltY = "tilt_y"
        case roll
        case timestampMS = "timestamp_ms"
    }
}

struct TaburaInkStroke: Encodable {
    let pointerType: String
    let width: Double
    let points: [TaburaInkPoint]

    private enum CodingKeys: String, CodingKey {
        case pointerType = "pointer_type"
        case width
        case points
    }
}

struct TaburaInkCommitMessage: Encodable {
    let type: String
    let artifactKind: String
    let requestResponse: Bool
    let outputMode: String
    let totalStrokes: Int
    let strokes: [TaburaInkStroke]

    private enum CodingKeys: String, CodingKey {
        case type
        case artifactKind = "artifact_kind"
        case requestResponse = "request_response"
        case outputMode = "output_mode"
        case totalStrokes = "total_strokes"
        case strokes
    }
}

struct TaburaDiscoveredServer: Identifiable, Hashable {
    let id: String
    let name: String
    let host: String
    let port: Int

    var baseURLString: String {
        "http://\(host):\(port)"
    }
}

func taburaWSURL(baseURL: URL, path: String) -> URL? {
    guard var components = URLComponents(url: baseURL, resolvingAgainstBaseURL: false) else {
        return nil
    }
    components.scheme = components.scheme == "https" ? "wss" : "ws"
    components.path = "/ws/" + path
    return components.url
}

func taburaAPIURL(baseURL: URL, path: String) -> URL {
    baseURL.appendingPathComponent("api").appendingPathComponent(path)
}

func taburaCanvasHTML(from payload: TaburaCanvasEventPayload) -> String {
    if let html = payload.html, html.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty == false {
        return html
    }
    let text = payload.markdownOrText ?? payload.text ?? ""
    let escaped = text
        .replacingOccurrences(of: "&", with: "&amp;")
        .replacingOccurrences(of: "<", with: "&lt;")
        .replacingOccurrences(of: ">", with: "&gt;")
    return "<pre style=\"white-space: pre-wrap; font: -apple-system-body; margin: 24px;\">\(escaped)</pre>"
}
