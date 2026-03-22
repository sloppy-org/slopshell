import Foundation

@MainActor
final class TaburaAppModel: ObservableObject {
    @Published var serverURLString = "http://127.0.0.1:8420"
    @Published var password = ""
    @Published var composerText = ""
    @Published var messages: [TaburaRenderedMessage] = []
    @Published var canvas = TaburaCanvasArtifact(kind: "", title: "", html: "<p style=\"margin:24px; font: -apple-system-body;\">Connect to a Tabura server to load the canvas.</p>", text: "")
    @Published var workspaces: [TaburaWorkspace] = []
    @Published var selectedWorkspaceID = ""
    @Published var statusText = "Disconnected"
    @Published var lastError = ""
    @Published var isRecording = false
    @Published var inkRequestsResponse = true
    @Published var isDialogueModeActive = false
    @Published var isAwaitingAssistantResponse = false
    @Published var companionEnabled = false
    @Published var companionIdleSurface = TaburaCompanionIdleSurface.robot.rawValue
    @Published var companionRuntimeState = TaburaDialogueRuntimeState.idle.rawValue

    let discovery = TaburaServerDiscovery()

    private let session: URLSession
    private lazy var chatTransport = TaburaChatTransport(session: session, onEvent: { [weak self] event in
        self?.handleChatEvent(event)
    }, onDisconnect: { [weak self] message in
        self?.statusText = "Chat disconnected"
        self?.lastError = message
    })
    private lazy var canvasTransport = TaburaCanvasTransport(session: session, onArtifact: { [weak self] artifact in
        self?.canvas = artifact
    }, onDisconnect: { [weak self] message in
        self?.statusText = "Canvas disconnected"
        self?.lastError = message
    })
    private lazy var audioCapture = TaburaAudioCapture(onChunk: { [weak self] data in
        Task {
            await self?.sendAudioChunk(data)
        }
    }, onStateChange: { [weak self] running, message in
        self?.isRecording = running
        if message.isEmpty == false {
            self?.lastError = message
        }
    })

    private var activeWorkspace: TaburaWorkspace?
    private var restoreCompanionEnabledOnExit: Bool?

    init() {
        let config = URLSessionConfiguration.default
        config.httpCookieAcceptPolicy = .always
        config.httpCookieStorage = HTTPCookieStorage.shared
        config.waitsForConnectivity = true
        self.session = URLSession(configuration: config)
        discovery.start()
    }

    var dialoguePresentation: TaburaDialogueModePresentation {
        TaburaDialogueModePresentation(
            isActive: isDialogueModeActive,
            isRecording: isRecording,
            isAwaitingAssistant: isAwaitingAssistantResponse,
            companionEnabled: companionEnabled,
            idleSurface: companionIdleSurface,
            runtimeState: companionRuntimeState
        )
    }

    func useDiscoveredServer(_ server: TaburaDiscoveredServer) {
        serverURLString = server.baseURLString
    }

    func connect() async {
        guard let baseURL = normalizedBaseURL() else {
            lastError = "Enter a valid server URL."
            return
        }
        do {
            try await loginIfNeeded(baseURL: baseURL)
            let response = try await loadWorkspaces(baseURL: baseURL)
            workspaces = response.workspaces
            if let workspace = response.workspaces.first(where: { $0.id == response.activeWorkspaceID }) ?? response.workspaces.first {
                selectedWorkspaceID = workspace.id
                activeWorkspace = workspace
                try await loadHistory(baseURL: baseURL, workspace: workspace)
                try await attachRealtime(baseURL: baseURL, workspace: workspace)
                statusText = "Connected to \(workspace.name)"
            } else {
                statusText = "Authenticated"
            }
        } catch {
            lastError = error.localizedDescription
            statusText = "Connection failed"
        }
    }

    func switchWorkspace() async {
        guard let baseURL = normalizedBaseURL() else {
            return
        }
        if let workspace = activeWorkspace {
            await stopDialogueMode(baseURL: baseURL, workspace: workspace, restoreCompanion: true)
        }
        guard let workspace = workspaces.first(where: { $0.id == selectedWorkspaceID }) else {
            return
        }
        do {
            activeWorkspace = workspace
            try await loadHistory(baseURL: baseURL, workspace: workspace)
            try await attachRealtime(baseURL: baseURL, workspace: workspace)
            statusText = "Connected to \(workspace.name)"
        } catch {
            lastError = error.localizedDescription
        }
    }

    func toggleDialogueMode() async {
        guard let baseURL = normalizedBaseURL(), let workspace = activeWorkspace else {
            return
        }
        if isDialogueModeActive {
            await stopDialogueMode(baseURL: baseURL, workspace: workspace, restoreCompanion: true)
            return
        }
        do {
            restoreCompanionEnabledOnExit = companionEnabled
            try await updateLivePolicy(baseURL: baseURL, policy: "dialogue")
            if companionEnabled == false {
                let cfg = try await updateCompanionConfig(
                    baseURL: baseURL,
                    workspace: workspace,
                    patch: TaburaCompanionConfigPatch(companionEnabled: true, idleSurface: nil)
                )
                applyCompanionConfig(cfg)
            }
            isDialogueModeActive = true
            isAwaitingAssistantResponse = false
            statusText = "Dialogue mode on"
        } catch {
            lastError = error.localizedDescription
        }
    }

    func setDialogueIdleSurface(_ surface: TaburaCompanionIdleSurface) async {
        guard let baseURL = normalizedBaseURL(), let workspace = activeWorkspace else {
            companionIdleSurface = surface.rawValue
            return
        }
        do {
            let cfg = try await updateCompanionConfig(
                baseURL: baseURL,
                workspace: workspace,
                patch: TaburaCompanionConfigPatch(companionEnabled: nil, idleSurface: surface.rawValue)
            )
            applyCompanionConfig(cfg)
            statusText = surface == .black ? "Black dialogue surface ready" : "Robot dialogue surface ready"
        } catch {
            lastError = error.localizedDescription
        }
    }

    func sendComposerMessage() async {
        guard let baseURL = normalizedBaseURL(), let workspace = activeWorkspace else {
            return
        }
        let text = composerText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard text.isEmpty == false else {
            return
        }
        composerText = ""
        do {
            var request = URLRequest(url: taburaAPIURL(baseURL: baseURL, path: "chat/sessions/\(workspace.chatSessionID)/messages"))
            request.httpMethod = "POST"
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
            request.httpBody = try JSONEncoder().encode(TaburaChatSendRequest(text: text, outputMode: "voice"))
            _ = try await session.data(for: request)
            messages.append(TaburaRenderedMessage(id: UUID().uuidString, role: "user", text: text, html: ""))
        } catch {
            lastError = error.localizedDescription
        }
    }

    func toggleRecording() async {
        if isRecording {
            audioCapture.stop()
            do {
                try await chatTransport.send(TaburaAudioCaptureMessage(type: "audio_stop", mimeType: nil, data: nil))
                if isDialogueModeActive {
                    isAwaitingAssistantResponse = true
                    companionRuntimeState = TaburaDialogueRuntimeState.thinking.rawValue
                }
            } catch {
                lastError = error.localizedDescription
            }
            return
        }
        do {
            audioCapture.stop()
            try audioCapture.start()
            if isDialogueModeActive {
                isAwaitingAssistantResponse = false
                companionRuntimeState = TaburaDialogueRuntimeState.recording.rawValue
            }
        } catch {
            lastError = error.localizedDescription
        }
    }

    func submitInk(_ strokes: [TaburaInkStroke]) async {
        guard !strokes.isEmpty else {
            return
        }
        let payload = TaburaInkCommitMessage(
            type: "ink_stroke",
            artifactKind: "text",
            requestResponse: inkRequestsResponse,
            outputMode: "voice",
            totalStrokes: strokes.count,
            strokes: strokes
        )
        do {
            try await chatTransport.send(payload)
            statusText = inkRequestsResponse ? "Ink sent to Tabura" : "Ink captured"
        } catch {
            lastError = error.localizedDescription
        }
    }

    private func normalizedBaseURL() -> URL? {
        let trimmed = serverURLString.trimmingCharacters(in: .whitespacesAndNewlines)
        return URL(string: trimmed)
    }

    private func loginIfNeeded(baseURL: URL) async throws {
        var setupRequest = URLRequest(url: taburaAPIURL(baseURL: baseURL, path: "setup"))
        setupRequest.httpMethod = "GET"
        let (setupData, _) = try await session.data(for: setupRequest)
        let setupObject = try JSONSerialization.jsonObject(with: setupData) as? [String: Any]
        let authenticated = setupObject?["authenticated"] as? Bool ?? false
        let hasPassword = setupObject?["has_password"] as? Bool ?? false
        if authenticated || !hasPassword {
            return
        }
        var request = URLRequest(url: taburaAPIURL(baseURL: baseURL, path: "login"))
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        request.httpBody = try JSONEncoder().encode(TaburaLoginRequest(password: password))
        let (_, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse, (200..<300).contains(http.statusCode) else {
            throw URLError(.userAuthenticationRequired)
        }
    }

    private func loadWorkspaces(baseURL: URL) async throws -> TaburaWorkspaceListResponse {
        let (data, _) = try await session.data(from: taburaAPIURL(baseURL: baseURL, path: "runtime/workspaces"))
        return try JSONDecoder().decode(TaburaWorkspaceListResponse.self, from: data)
    }

    private func loadHistory(baseURL: URL, workspace: TaburaWorkspace) async throws {
        let (data, _) = try await session.data(from: taburaAPIURL(baseURL: baseURL, path: "chat/sessions/\(workspace.chatSessionID)/history"))
        let history = try JSONDecoder().decode(TaburaChatHistoryResponse.self, from: data)
        messages = history.messages.map {
            TaburaRenderedMessage(id: "persisted-\($0.id)", role: $0.role, text: $0.content, html: "")
        }
    }

    private func attachRealtime(baseURL: URL, workspace: TaburaWorkspace) async throws {
        chatTransport.connect(baseURL: baseURL, sessionID: workspace.chatSessionID)
        canvasTransport.connect(baseURL: baseURL, sessionID: workspace.canvasSessionID)
        try await canvasTransport.loadSnapshot(baseURL: baseURL, sessionID: workspace.canvasSessionID)
        async let config = loadCompanionConfig(baseURL: baseURL, workspace: workspace)
        async let state = loadCompanionState(baseURL: baseURL, workspace: workspace)
        applyCompanionConfig(try await config)
        applyCompanionState(try await state)
        isDialogueModeActive = false
        isAwaitingAssistantResponse = false
        restoreCompanionEnabledOnExit = nil
    }

    private func loadCompanionConfig(baseURL: URL, workspace: TaburaWorkspace) async throws -> TaburaCompanionConfig {
        let (data, _) = try await session.data(from: taburaAPIURL(baseURL: baseURL, path: "workspaces/\(workspace.id)/companion/config"))
        return try JSONDecoder().decode(TaburaCompanionConfig.self, from: data)
    }

    private func loadCompanionState(baseURL: URL, workspace: TaburaWorkspace) async throws -> TaburaCompanionStateResponse {
        let (data, _) = try await session.data(from: taburaAPIURL(baseURL: baseURL, path: "workspaces/\(workspace.id)/companion/state"))
        return try JSONDecoder().decode(TaburaCompanionStateResponse.self, from: data)
    }

    private func updateCompanionConfig(baseURL: URL, workspace: TaburaWorkspace, patch: TaburaCompanionConfigPatch) async throws -> TaburaCompanionConfig {
        var request = URLRequest(url: taburaAPIURL(baseURL: baseURL, path: "workspaces/\(workspace.id)/companion/config"))
        request.httpMethod = "PUT"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(patch)
        let (data, _) = try await session.data(for: request)
        return try JSONDecoder().decode(TaburaCompanionConfig.self, from: data)
    }

    private func updateLivePolicy(baseURL: URL, policy: String) async throws {
        var request = URLRequest(url: taburaAPIURL(baseURL: baseURL, path: "live-policy"))
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(TaburaLivePolicyRequest(policy: policy))
        let (_, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse, (200..<300).contains(http.statusCode) else {
            throw URLError(.badServerResponse)
        }
    }

    private func stopDialogueMode(baseURL: URL, workspace: TaburaWorkspace, restoreCompanion: Bool) async {
        if isRecording {
            audioCapture.stop()
            do {
                try await chatTransport.send(TaburaAudioCaptureMessage(type: "audio_stop", mimeType: nil, data: nil))
            } catch {
                lastError = error.localizedDescription
            }
        }
        isDialogueModeActive = false
        isAwaitingAssistantResponse = false
        companionRuntimeState = TaburaDialogueRuntimeState.idle.rawValue
        if restoreCompanion, let restore = restoreCompanionEnabledOnExit, restore != companionEnabled {
            do {
                let cfg = try await updateCompanionConfig(
                    baseURL: baseURL,
                    workspace: workspace,
                    patch: TaburaCompanionConfigPatch(companionEnabled: restore, idleSurface: nil)
                )
                applyCompanionConfig(cfg)
            } catch {
                lastError = error.localizedDescription
            }
        }
        restoreCompanionEnabledOnExit = nil
        statusText = "Dialogue mode off"
    }

    private func sendAudioChunk(_ data: Data) async {
        do {
            try await chatTransport.send(TaburaAudioCaptureMessage(
                type: "audio_pcm",
                mimeType: "audio/L16;rate=16000;channels=1",
                data: data.base64EncodedString()
            ))
        } catch {
            lastError = error.localizedDescription
        }
    }

    private func handleChatEvent(_ event: TaburaChatEventPayload) {
        switch event.type {
        case "action":
            if event.actionType == "toggle_live_dialogue" {
                Task { await toggleDialogueMode() }
            }
        case "companion_state":
            if event.workspacePath == nil || event.workspacePath == activeWorkspace?.rootPath {
                companionRuntimeState = TaburaDialogueRuntimeState(raw: event.state ?? "idle").rawValue
            }
        case "render_chat", "assistant_output", "message_persisted":
            let text = event.markdown ?? event.message ?? event.text ?? ""
            if text.isEmpty {
                return
            }
            messages.append(TaburaRenderedMessage(
                id: event.turnID ?? UUID().uuidString,
                role: event.role ?? "assistant",
                text: text,
                html: event.html ?? ""
            ))
            isAwaitingAssistantResponse = false
            if isDialogueModeActive && isRecording == false {
                companionRuntimeState = TaburaDialogueRuntimeState.listening.rawValue
            }
        case "stt_result":
            if let text = event.text, text.isEmpty == false {
                composerText = text
                statusText = "Transcription ready"
            }
        case "stt_empty":
            statusText = event.reason ?? "No speech detected"
            isAwaitingAssistantResponse = false
            if isDialogueModeActive {
                companionRuntimeState = TaburaDialogueRuntimeState.listening.rawValue
            }
        case "stt_error", "error":
            lastError = event.error ?? "Unknown server error"
            isAwaitingAssistantResponse = false
            if isDialogueModeActive {
                companionRuntimeState = TaburaDialogueRuntimeState.error.rawValue
            }
        default:
            break
        }
    }

    private func applyCompanionConfig(_ config: TaburaCompanionConfig) {
        companionEnabled = config.companionEnabled
        companionIdleSurface = TaburaCompanionIdleSurface(raw: config.idleSurface).rawValue
    }

    private func applyCompanionState(_ state: TaburaCompanionStateResponse) {
        companionEnabled = state.companionEnabled
        companionIdleSurface = TaburaCompanionIdleSurface(raw: state.idleSurface).rawValue
        companionRuntimeState = TaburaDialogueRuntimeState(raw: state.state).rawValue
    }
}
