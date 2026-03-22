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
    baseURL.appending(path: "api/" + path)
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
