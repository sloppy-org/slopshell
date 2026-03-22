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
