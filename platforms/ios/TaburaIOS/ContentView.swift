import SwiftUI
import UIKit

struct ContentView: View {
    @StateObject private var model = TaburaAppModel()

    var body: some View {
        NavigationStack { rootContent }
            .onAppear {
                UIApplication.shared.isIdleTimerDisabled = model.dialoguePresentation.keepScreenAwake
            }
            .onDisappear {
                UIApplication.shared.isIdleTimerDisabled = false
            }
            .onChange(of: model.dialoguePresentation.keepScreenAwake) { _, enabled in
                UIApplication.shared.isIdleTimerDisabled = enabled
            }
    }

    @ViewBuilder
    private var rootContent: some View {
        if model.dialoguePresentation.usesBlackScreen {
            blackScreenDialoguePanel
        } else {
            VStack(spacing: 16) {
                connectionPanel
                workspacePicker
                dialogueControls
                canvasPanel
                chatPanel
                composerPanel
            }
            .padding(16)
            .navigationTitle("Tabura iOS")
        }
    }

    private var connectionPanel: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Server")
                .font(.headline)
            TextField("http://127.0.0.1:8420", text: $model.serverURLString)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .textFieldStyle(.roundedBorder)
            SecureField("Password", text: $model.password)
                .textFieldStyle(.roundedBorder)
            if model.discovery.servers.isEmpty == false {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 8) {
                        ForEach(model.discovery.servers) { server in
                            Button(server.name) {
                                model.useDiscoveredServer(server)
                            }
                            .buttonStyle(.bordered)
                        }
                    }
                }
            }
            HStack {
                Button("Connect") {
                    Task { await model.connect() }
                }
                .buttonStyle(.borderedProminent)
                Text(model.statusText)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                Spacer()
            }
            if model.lastError.isEmpty == false {
                Text(model.lastError)
                    .font(.footnote)
                    .foregroundStyle(.red)
            }
        }
    }

    private var workspacePicker: some View {
        HStack {
            Picker("Workspace", selection: $model.selectedWorkspaceID) {
                ForEach(model.workspaces, id: \.id) { workspace in
                    Text(workspace.name).tag(workspace.id)
                }
            }
            .pickerStyle(.menu)
            Button("Switch") {
                Task { await model.switchWorkspace() }
            }
            .buttonStyle(.bordered)
            Spacer()
            Toggle("Ink asks Tabura", isOn: $model.inkRequestsResponse)
                .toggleStyle(.switch)
                .labelsHidden()
        }
    }

    private var dialogueControls: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text("Dialogue Surface")
                    .font(.headline)
                Spacer()
                Picker("Surface", selection: Binding(
                    get: { TaburaCompanionIdleSurface(raw: model.companionIdleSurface) },
                    set: { surface in
                        Task { await model.setDialogueIdleSurface(surface) }
                    }
                )) {
                    Text("Robot").tag(TaburaCompanionIdleSurface.robot)
                    Text("Black").tag(TaburaCompanionIdleSurface.black)
                }
                .pickerStyle(.segmented)
            }
            HStack {
                Button(model.isDialogueModeActive ? "Stop Dialogue" : "Start Dialogue") {
                    Task { await model.toggleDialogueMode() }
                }
                .buttonStyle(.borderedProminent)
                VStack(alignment: .leading, spacing: 2) {
                    Text(model.dialoguePresentation.primaryLabel)
                        .font(.subheadline.weight(.semibold))
                    Text(model.dialoguePresentation.secondaryLabel)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                Spacer()
            }
        }
    }

    private var canvasPanel: some View {
        ZStack(alignment: .topTrailing) {
            TaburaCanvasWebView(html: model.canvas.html, baseURL: URL(string: model.serverURLString))
                .frame(minHeight: 260)
                .clipShape(RoundedRectangle(cornerRadius: 20))
                .overlay(
                    RoundedRectangle(cornerRadius: 20)
                        .strokeBorder(Color.secondary.opacity(0.15), lineWidth: 1)
                )
            TaburaInkCaptureView { strokes in
                Task { await model.submitInk(strokes) }
            }
            .allowsHitTesting(true)
            .clipShape(RoundedRectangle(cornerRadius: 20))
            .padding(8)
        }
    }

    private var chatPanel: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 12) {
                ForEach(model.messages) { message in
                    VStack(alignment: .leading, spacing: 4) {
                        Text(message.role.capitalized)
                            .font(.caption.weight(.semibold))
                            .foregroundStyle(.secondary)
                        Text(message.text)
                            .textSelection(.enabled)
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    .padding(12)
                    .background(message.role == "user" ? Color.blue.opacity(0.08) : Color.secondary.opacity(0.08))
                    .clipShape(RoundedRectangle(cornerRadius: 14))
                }
            }
        }
        .frame(maxHeight: 220)
    }

    private var composerPanel: some View {
        VStack(spacing: 12) {
            TextEditor(text: $model.composerText)
                .frame(minHeight: 90)
                .padding(8)
                .overlay(
                    RoundedRectangle(cornerRadius: 14)
                        .strokeBorder(Color.secondary.opacity(0.15), lineWidth: 1)
                )
            HStack {
                Button(model.isRecording ? "Stop Mic" : "Record Mic") {
                    Task { await model.toggleRecording() }
                }
                .buttonStyle(.bordered)
                Spacer()
                Button("Send") {
                    Task { await model.sendComposerMessage() }
                }
                .buttonStyle(.borderedProminent)
            }
        }
    }

    private var blackScreenDialoguePanel: some View {
        let presentation = model.dialoguePresentation
        return ZStack {
            Button {
                Task { await model.toggleRecording() }
            } label: {
                Color.black.ignoresSafeArea()
            }
            .buttonStyle(.plain)
            VStack(spacing: 18) {
                Spacer()
                Text(presentation.primaryLabel)
                    .font(.system(size: 36, weight: .semibold, design: .rounded))
                    .foregroundStyle(.white)
                Text(presentation.secondaryLabel)
                    .font(.title3)
                    .multilineTextAlignment(.center)
                    .foregroundStyle(.white.opacity(0.8))
                    .padding(.horizontal, 32)
                Text(presentation.tapActionLabel)
                    .font(.headline)
                    .foregroundStyle(.white)
                    .padding(.horizontal, 20)
                    .padding(.vertical, 12)
                    .background(.white.opacity(0.12), in: Capsule())
                if model.lastError.isEmpty == false {
                    Text(model.lastError)
                        .font(.footnote)
                        .foregroundStyle(.red.opacity(0.9))
                        .padding(.horizontal, 24)
                }
                Spacer()
                Button("Exit Dialogue") {
                    Task { await model.toggleDialogueMode() }
                }
                .buttonStyle(.borderedProminent)
                .tint(.white)
                .foregroundStyle(.black)
                .padding(.bottom, 32)
            }
        }
        .navigationBarHidden(true)
    }
}

struct TaburaFlowHarnessPreconditions {
    var tool = "pointer"
    var session = "none"
    var silent = false
    var indicatorState = ""
}

private struct TaburaFlowHarnessState {
    var activeTool = "pointer"
    var session = "none"
    var silent = false
    var circleExpanded = false
    var indicatorOverride = ""

    var taburaCircle: String {
        circleExpanded ? "expanded" : "collapsed"
    }

    var dotInnerIcon: String {
        switch activeTool {
        case "highlight":
            return "marker"
        case "ink":
            return "pen_nib"
        case "text_note":
            return "sticky_note"
        case "prompt":
            return "mic"
        default:
            return "arrow"
        }
    }

    var indicatorState: String {
        if indicatorOverride.isEmpty == false {
            return indicatorOverride
        }
        switch session {
        case "dialogue":
            return "listening"
        case "meeting":
            return "paused"
        default:
            return "idle"
        }
    }

    var bodyClass: String {
        [
            "tool-\(activeTool)",
            "session-\(session)",
            "indicator-\(indicatorState)",
            silent ? "silent-on" : "silent-off",
            circleExpanded ? "circle-expanded" : "circle-collapsed",
        ].joined(separator: " ")
    }

    var cursorClass: String {
        "tool-\(activeTool)"
    }
}

func parseTaburaFlowHarnessPreconditions(_ raw: String?) -> TaburaFlowHarnessPreconditions {
    guard
        let raw,
        let data = raw.data(using: .utf8),
        let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any]
    else {
        return TaburaFlowHarnessPreconditions()
    }
    return TaburaFlowHarnessPreconditions(
        tool: json["tool"] as? String ?? "pointer",
        session: json["session"] as? String ?? "none",
        silent: json["silent"] as? Bool ?? false,
        indicatorState: json["indicator_state"] as? String ?? ""
    )
}

private func makeTaburaFlowHarnessState(preconditions: TaburaFlowHarnessPreconditions) -> TaburaFlowHarnessState {
    TaburaFlowHarnessState(
        activeTool: ["highlight", "ink", "text_note", "prompt"].contains(preconditions.tool) ? preconditions.tool : "pointer",
        session: ["dialogue", "meeting"].contains(preconditions.session) ? preconditions.session : "none",
        silent: preconditions.silent,
        circleExpanded: false,
        indicatorOverride: ["idle", "listening", "paused", "recording", "working"].contains(preconditions.indicatorState) ? preconditions.indicatorState : ""
    )
}

struct TaburaFlowHarnessRootView: View {
    @State private var state: TaburaFlowHarnessState

    init(preconditions: TaburaFlowHarnessPreconditions) {
        _state = State(initialValue: makeTaburaFlowHarnessState(preconditions: preconditions))
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                Text("Native Flow Harness")
                    .font(.title.bold())
                HStack(spacing: 16) {
                    Button {
                        state.circleExpanded.toggle()
                    } label: {
                        ZStack {
                            Circle()
                                .fill(.black)
                                .frame(width: 72, height: 72)
                            Text(state.dotInnerIcon)
                                .foregroundStyle(.white)
                        }
                    }
                    .buttonStyle(.plain)
                    .accessibilityIdentifier("tabura_circle_dot")

                    Button {
                        state.session = "none"
                        state.indicatorOverride = ""
                    } label: {
                        Text(state.indicatorState)
                            .padding(.horizontal, 14)
                            .padding(.vertical, 10)
                            .background(.thinMaterial, in: Capsule())
                    }
                    .buttonStyle(.plain)
                    .accessibilityIdentifier("indicator_border")
                }

                if state.circleExpanded {
                    LazyVGrid(columns: [GridItem(.adaptive(minimum: 100), spacing: 8)], spacing: 8) {
                        flowHarnessSegment(id: "tabura_circle_pointer", label: "Pointer") {
                            state.activeTool = "pointer"
                        }
                        flowHarnessSegment(id: "tabura_circle_highlight", label: "Highlight") {
                            state.activeTool = "highlight"
                        }
                        flowHarnessSegment(id: "tabura_circle_ink", label: "Ink") {
                            state.activeTool = "ink"
                        }
                        flowHarnessSegment(id: "tabura_circle_text_note", label: "Text") {
                            state.activeTool = "text_note"
                        }
                        flowHarnessSegment(id: "tabura_circle_prompt", label: "Prompt") {
                            state.activeTool = "prompt"
                        }
                        flowHarnessSegment(id: "tabura_circle_dialogue", label: "Dialogue") {
                            state.session = state.session == "dialogue" ? "none" : "dialogue"
                            state.indicatorOverride = ""
                        }
                        flowHarnessSegment(id: "tabura_circle_meeting", label: "Meeting") {
                            state.session = state.session == "meeting" ? "none" : "meeting"
                            state.indicatorOverride = ""
                        }
                        flowHarnessSegment(id: "tabura_circle_silent", label: "Silent") {
                            state.silent.toggle()
                        }
                    }
                }

                RoundedRectangle(cornerRadius: 20)
                    .fill(.white)
                    .overlay {
                        RoundedRectangle(cornerRadius: 20)
                            .strokeBorder(Color.secondary.opacity(0.2), lineWidth: 1)
                    }
                    .frame(height: 180)
                    .overlay {
                        Text("Canvas")
                    }
                    .onTapGesture {
                        state.circleExpanded = false
                    }
                    .accessibilityIdentifier("canvas_viewport")

                VStack(alignment: .leading, spacing: 8) {
                    flowHarnessValue(state.activeTool, id: "flow_state_active_tool")
                    flowHarnessValue(state.session, id: "flow_state_session")
                    flowHarnessValue(state.silent ? "true" : "false", id: "flow_state_silent")
                    flowHarnessValue(state.taburaCircle, id: "flow_state_tabura_circle")
                    flowHarnessValue(state.dotInnerIcon, id: "flow_state_dot_inner_icon")
                    flowHarnessValue(state.indicatorState, id: "flow_state_indicator_state")
                    flowHarnessValue(state.bodyClass, id: "flow_state_body_class")
                    flowHarnessValue(state.cursorClass, id: "flow_state_cursor_class")
                }
            }
            .padding(16)
        }
    }

    @ViewBuilder
    private func flowHarnessSegment(id: String, label: String, action: @escaping () -> Void) -> some View {
        Button(label, action: action)
            .buttonStyle(.borderedProminent)
            .accessibilityIdentifier(id)
    }

    @ViewBuilder
    private func flowHarnessValue(_ value: String, id: String) -> some View {
        Text(value)
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityIdentifier(id)
    }
}
