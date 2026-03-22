package com.tabura.android

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONObject

class TaburaAppModel(application: Application) : AndroidViewModel(application) {
    data class UiState(
        val serverUrl: String = "http://127.0.0.1:8420",
        val password: String = "",
        val composerText: String = "",
        val messages: List<TaburaRenderedMessage> = emptyList(),
        val canvas: TaburaCanvasArtifact = TaburaCanvasArtifact(
            kind = "",
            title = "",
            html = "<p style=\"margin:24px;font:sans-serif;\">Connect to a Tabura server to load the canvas.</p>",
            text = "",
        ),
        val workspaces: List<TaburaWorkspace> = emptyList(),
        val selectedWorkspaceId: String = "",
        val statusText: String = "Disconnected",
        val lastError: String = "",
        val isRecording: Boolean = false,
        val inkRequestsResponse: Boolean = true,
        val discoveredServers: List<TaburaDiscoveredServer> = emptyList(),
        val isDialogueModeActive: Boolean = false,
        val isAwaitingAssistantResponse: Boolean = false,
        val companionEnabled: Boolean = false,
        val companionIdleSurface: String = TaburaCompanionIdleSurface.ROBOT.wireValue,
        val companionRuntimeState: String = TaburaDialogueRuntimeState.IDLE.name.lowercase(),
    ) {
        val dialoguePresentation: TaburaDialogueModePresentation
            get() = TaburaDialogueModePresentation(
                isActive = isDialogueModeActive,
                isRecording = isRecording,
                isAwaitingAssistant = isAwaitingAssistantResponse,
                companionEnabled = companionEnabled,
                idleSurface = companionIdleSurface,
                runtimeStateValue = companionRuntimeState,
            )
    }

    private val client = OkHttpClient()
    private val jsonMediaType = "application/json".toMediaType()
    private val _state = MutableStateFlow(UiState())
    val state: StateFlow<UiState> = _state.asStateFlow()

    private val discovery = TaburaServerDiscovery(
        context = application.applicationContext,
        onServersChanged = { servers ->
            _state.update { current -> current.copy(discoveredServers = servers) }
        },
        onError = { message -> setError(message) },
    )
    private val chatTransport = TaburaChatTransport(
        client = client,
        onEvent = ::handleChatEvent,
        onDisconnect = { message ->
            _state.update { current -> current.copy(statusText = "Chat disconnected", lastError = message) }
        },
    )
    private val canvasTransport = TaburaCanvasTransport(
        client = client,
        onArtifact = { artifact ->
            _state.update { current -> current.copy(canvas = artifact) }
        },
        onDisconnect = { message ->
            _state.update { current -> current.copy(statusText = "Canvas disconnected", lastError = message) }
        },
    )

    private var activeWorkspace: TaburaWorkspace? = null
    private var restoreCompanionEnabledOnExit: Boolean? = null

    init {
        discovery.start()
    }

    override fun onCleared() {
        chatTransport.disconnect()
        canvasTransport.disconnect()
        discovery.stop()
        super.onCleared()
    }

    fun updateServerUrl(value: String) {
        _state.update { current -> current.copy(serverUrl = value) }
    }

    fun updatePassword(value: String) {
        _state.update { current -> current.copy(password = value) }
    }

    fun updateComposerText(value: String) {
        _state.update { current -> current.copy(composerText = value) }
    }

    fun updateRecordingState(active: Boolean, message: String = "") {
        _state.update { current ->
            current.copy(
                isRecording = active,
                companionRuntimeState = if (current.isDialogueModeActive && active) {
                    TaburaDialogueRuntimeState.RECORDING.name.lowercase()
                } else {
                    current.companionRuntimeState
                },
                lastError = message.ifBlank { current.lastError },
            )
        }
    }

    fun setInkRequestsResponse(enabled: Boolean) {
        _state.update { current -> current.copy(inkRequestsResponse = enabled) }
    }

    fun useDiscoveredServer(server: TaburaDiscoveredServer) {
        _state.update { current -> current.copy(serverUrl = server.baseUrlString) }
    }

    fun connect() {
        viewModelScope.launch {
            val baseUrl = state.value.serverUrl.trim()
            if (baseUrl.isBlank()) {
                setError("Enter a valid Tabura server URL.")
                return@launch
            }
            runCatching {
                loginIfNeeded(baseUrl)
                val workspaceResponse = loadWorkspaces(baseUrl)
                val selected = workspaceResponse.workspaces.firstOrNull {
                    it.id == workspaceResponse.activeWorkspaceId
                } ?: workspaceResponse.workspaces.firstOrNull()
                _state.update { current ->
                    current.copy(
                        workspaces = workspaceResponse.workspaces,
                        selectedWorkspaceId = selected?.id.orEmpty(),
                    )
                }
                if (selected != null) {
                    attachWorkspace(baseUrl, selected)
                } else {
                    _state.update { current -> current.copy(statusText = "Authenticated") }
                }
            }.onFailure { error ->
                _state.update { current ->
                    current.copy(
                        statusText = "Connection failed",
                        lastError = error.message ?: "Connection failed",
                    )
                }
            }
        }
    }

    fun switchWorkspace(workspaceId: String) {
        val workspace = state.value.workspaces.firstOrNull { it.id == workspaceId } ?: return
        val baseUrl = state.value.serverUrl.trim()
        viewModelScope.launch {
            runCatching {
                activeWorkspace?.let { current ->
                    stopDialogueMode(baseUrl, current, restoreCompanion = true)
                }
                attachWorkspace(baseUrl, workspace)
            }.onFailure { error ->
                setError(error.message ?: "Workspace switch failed")
            }
        }
    }

    fun toggleDialogueMode() {
        val workspace = activeWorkspace ?: return
        val baseUrl = state.value.serverUrl.trim()
        viewModelScope.launch {
            runCatching {
                if (state.value.isDialogueModeActive) {
                    stopDialogueMode(baseUrl, workspace, restoreCompanion = true)
                    return@runCatching
                }
                restoreCompanionEnabledOnExit = state.value.companionEnabled
                postJson(taburaApiUrl(baseUrl, "live-policy"), livePolicyRequest("dialogue"))
                if (!state.value.companionEnabled) {
                    val config = parseCompanionConfig(
                        putJson(
                            taburaApiUrl(baseUrl, "workspaces/${workspace.id}/companion/config"),
                            companionConfigPatch(companionEnabled = true),
                        )
                    )
                    applyCompanionConfig(config)
                }
                _state.update { current ->
                    current.copy(
                        isDialogueModeActive = true,
                        isAwaitingAssistantResponse = false,
                        statusText = "Dialogue mode on",
                    )
                }
            }.onFailure { error ->
                setError(error.message ?: "Dialogue mode failed")
            }
        }
    }

    fun setDialogueIdleSurface(surface: TaburaCompanionIdleSurface) {
        val workspace = activeWorkspace ?: run {
            _state.update { current -> current.copy(companionIdleSurface = surface.wireValue) }
            return
        }
        val baseUrl = state.value.serverUrl.trim()
        viewModelScope.launch {
            runCatching {
                val config = parseCompanionConfig(
                    putJson(
                        taburaApiUrl(baseUrl, "workspaces/${workspace.id}/companion/config"),
                        companionConfigPatch(idleSurface = surface.wireValue),
                    )
                )
                applyCompanionConfig(config)
                _state.update { current ->
                    current.copy(
                        statusText = if (surface == TaburaCompanionIdleSurface.BLACK) {
                            "Black dialogue surface ready"
                        } else {
                            "Robot dialogue surface ready"
                        },
                    )
                }
            }.onFailure { error ->
                setError(error.message ?: "Surface update failed")
            }
        }
    }

    fun sendComposerMessage() {
        val workspace = activeWorkspace ?: return
        val text = state.value.composerText.trim()
        if (text.isBlank()) {
            return
        }
        viewModelScope.launch {
            runCatching {
                postJson(
                    url = taburaApiUrl(state.value.serverUrl, "chat/sessions/${workspace.chatSessionId}/messages"),
                    body = composerRequest(text),
                )
                _state.update { current ->
                    current.copy(
                        composerText = "",
                        messages = current.messages + TaburaRenderedMessage(
                            id = "local-${System.currentTimeMillis()}",
                            role = "user",
                            text = text,
                        ),
                    )
                }
            }.onFailure { error ->
                setError(error.message ?: "Message send failed")
            }
        }
    }

    fun sendAudioChunk(data: ByteArray) {
        if (!chatTransport.sendJson(audioPcmMessage(data))) {
            setError("Audio transport is not connected")
        }
    }

    fun stopAudio() {
        if (!chatTransport.sendJson(audioStopMessage())) {
            _state.update { current -> current.copy(isRecording = false) }
            return
        }
        _state.update { current ->
            current.copy(
                isAwaitingAssistantResponse = current.isDialogueModeActive,
                companionRuntimeState = if (current.isDialogueModeActive) {
                    TaburaDialogueRuntimeState.THINKING.name.lowercase()
                } else {
                    current.companionRuntimeState
                },
            )
        }
    }

    fun submitInk(strokes: List<TaburaInkStroke>) {
        if (strokes.isEmpty()) {
            return
        }
        val requestResponse = state.value.inkRequestsResponse
        if (!chatTransport.sendJson(inkCommitMessage(strokes, requestResponse))) {
            setError("Ink transport is not connected")
            return
        }
        _state.update { current ->
            current.copy(statusText = if (requestResponse) "Ink sent to Tabura" else "Ink captured")
        }
    }

    private suspend fun attachWorkspace(baseUrl: String, workspace: TaburaWorkspace) {
        activeWorkspace = workspace
        val history = loadHistory(baseUrl, workspace)
        val companionConfig = loadCompanionConfig(baseUrl, workspace)
        val companionState = loadCompanionState(baseUrl, workspace)
        _state.update { current ->
            current.copy(
                messages = history,
                selectedWorkspaceId = workspace.id,
            )
        }
        chatTransport.connect(baseUrl, workspace.chatSessionId)
        canvasTransport.connect(baseUrl, workspace.canvasSessionId)
        canvasTransport.loadSnapshot(baseUrl, workspace.canvasSessionId)?.let { artifact ->
            _state.update { current -> current.copy(canvas = artifact) }
        }
        applyCompanionConfig(companionConfig)
        applyCompanionState(companionState)
        _state.update { current ->
            current.copy(
                statusText = "Connected to ${workspace.name}",
                isDialogueModeActive = false,
                isAwaitingAssistantResponse = false,
            )
        }
        restoreCompanionEnabledOnExit = null
    }

    private suspend fun loginIfNeeded(baseUrl: String) {
        val setup = JSONObject(get(taburaApiUrl(baseUrl, "setup")))
        val authenticated = setup.optBoolean("authenticated")
        val hasPassword = setup.optBoolean("has_password")
        if (authenticated || !hasPassword) {
            return
        }
        val response = postJson(
            url = taburaApiUrl(baseUrl, "login"),
            body = loginRequest(state.value.password),
        )
        if (response.isBlank()) {
            return
        }
    }

    private suspend fun loadWorkspaces(baseUrl: String): TaburaWorkspaceListResponse {
        return parseWorkspaceListResponse(get(taburaApiUrl(baseUrl, "runtime/workspaces")))
    }

    private suspend fun loadHistory(baseUrl: String, workspace: TaburaWorkspace): List<TaburaRenderedMessage> {
        return parseChatHistory(get(taburaApiUrl(baseUrl, "chat/sessions/${workspace.chatSessionId}/history")))
    }

    private suspend fun loadCompanionConfig(baseUrl: String, workspace: TaburaWorkspace): TaburaCompanionConfig {
        return parseCompanionConfig(get(taburaApiUrl(baseUrl, "workspaces/${workspace.id}/companion/config")))
    }

    private suspend fun loadCompanionState(baseUrl: String, workspace: TaburaWorkspace): TaburaCompanionState {
        return parseCompanionState(get(taburaApiUrl(baseUrl, "workspaces/${workspace.id}/companion/state")))
    }

    private suspend fun get(url: String): String = withContext(Dispatchers.IO) {
        val request = Request.Builder().url(url).build()
        client.newCall(request).execute().use { response ->
            if (!response.isSuccessful) {
                error("HTTP ${response.code} for $url")
            }
            response.body?.string().orEmpty()
        }
    }

    private suspend fun postJson(url: String, body: String): String = withContext(Dispatchers.IO) {
        val request = Request.Builder()
            .url(url)
            .post(body.toRequestBody(jsonMediaType))
            .build()
        client.newCall(request).execute().use { response ->
            if (!response.isSuccessful) {
                error("HTTP ${response.code} for $url")
            }
            response.body?.string().orEmpty()
        }
    }

    private suspend fun putJson(url: String, body: String): String = withContext(Dispatchers.IO) {
        val request = Request.Builder()
            .url(url)
            .put(body.toRequestBody(jsonMediaType))
            .build()
        client.newCall(request).execute().use { response ->
            if (!response.isSuccessful) {
                error("HTTP ${response.code} for $url")
            }
            response.body?.string().orEmpty()
        }
    }

    private suspend fun stopDialogueMode(baseUrl: String, workspace: TaburaWorkspace, restoreCompanion: Boolean) {
        if (state.value.isRecording) {
            stopAudio()
        }
        _state.update { current ->
            current.copy(
                isDialogueModeActive = false,
                isAwaitingAssistantResponse = false,
                companionRuntimeState = TaburaDialogueRuntimeState.IDLE.name.lowercase(),
            )
        }
        if (restoreCompanion) {
            val restore = restoreCompanionEnabledOnExit
            if (restore != null && restore != state.value.companionEnabled) {
                val config = parseCompanionConfig(
                    putJson(
                        taburaApiUrl(baseUrl, "workspaces/${workspace.id}/companion/config"),
                        companionConfigPatch(companionEnabled = restore),
                    )
                )
                applyCompanionConfig(config)
            }
        }
        restoreCompanionEnabledOnExit = null
        _state.update { current -> current.copy(statusText = "Dialogue mode off") }
    }

    private fun handleChatEvent(event: TaburaChatEventPayload) {
        when (event.type) {
            "action" -> {
                if (event.actionType == "toggle_live_dialogue") {
                    toggleDialogueMode()
                }
            }

            "companion_state" -> {
                if (event.workspacePath.isBlank() || event.workspacePath == activeWorkspace?.rootPath) {
                    _state.update { current ->
                        current.copy(companionRuntimeState = TaburaDialogueRuntimeState.normalize(event.state).name.lowercase())
                    }
                }
            }

            "render_chat", "assistant_output", "message_persisted" -> {
                val content = event.markdown.ifBlank { event.message.ifBlank { event.text } }
                if (content.isBlank()) {
                    return
                }
                _state.update { current ->
                    current.copy(
                        messages = current.messages + TaburaRenderedMessage(
                            id = event.turnId.ifBlank { "event-${System.currentTimeMillis()}" },
                            role = event.role.ifBlank { "assistant" },
                            text = content,
                            html = event.html,
                        ),
                        isAwaitingAssistantResponse = false,
                        companionRuntimeState = if (current.isDialogueModeActive && !current.isRecording) {
                            TaburaDialogueRuntimeState.LISTENING.name.lowercase()
                        } else {
                            current.companionRuntimeState
                        },
                    )
                }
            }

            "stt_result" -> {
                if (event.text.isBlank()) {
                    return
                }
                _state.update { current ->
                    current.copy(
                        composerText = event.text,
                        statusText = "Transcription ready",
                    )
                }
            }

            "stt_empty" -> {
                _state.update { current ->
                    current.copy(
                        statusText = event.reason.ifBlank { "No speech detected" },
                        isAwaitingAssistantResponse = false,
                        companionRuntimeState = if (current.isDialogueModeActive) {
                            TaburaDialogueRuntimeState.LISTENING.name.lowercase()
                        } else {
                            current.companionRuntimeState
                        },
                    )
                }
            }

            "stt_error", "error" -> {
                setError(event.error.ifBlank { "Tabura server error" })
                _state.update { current ->
                    current.copy(
                        isAwaitingAssistantResponse = false,
                        companionRuntimeState = if (current.isDialogueModeActive) {
                            TaburaDialogueRuntimeState.ERROR.name.lowercase()
                        } else {
                            current.companionRuntimeState
                        },
                    )
                }
            }
        }
    }

    private fun applyCompanionConfig(config: TaburaCompanionConfig) {
        _state.update { current ->
            current.copy(
                companionEnabled = config.companionEnabled,
                companionIdleSurface = TaburaCompanionIdleSurface.normalize(config.idleSurface).wireValue,
            )
        }
    }

    private fun applyCompanionState(companionState: TaburaCompanionState) {
        _state.update { current ->
            current.copy(
                companionEnabled = companionState.companionEnabled,
                companionIdleSurface = TaburaCompanionIdleSurface.normalize(companionState.idleSurface).wireValue,
                companionRuntimeState = TaburaDialogueRuntimeState.normalize(companionState.state).name.lowercase(),
            )
        }
    }

    private fun setError(message: String) {
        _state.update { current -> current.copy(lastError = message) }
    }
}
