package app.noema.android.ui

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import app.noema.android.data.ClientSettings
import app.noema.android.data.NoemaWsClient
import app.noema.android.data.SettingsStore
import app.noema.android.data.SocketState
import app.noema.android.data.capUiText
import app.noema.android.data.payloadText
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import java.util.UUID

data class ChatLine(
    val id: String,
    val role: String,
    val text: String,
)

data class ChatUiState(
    val settings: ClientSettings = ClientSettings(),
    val socket: SocketState = SocketState.Idle,
    val lines: List<ChatLine> = emptyList(),
    val draft: String = "",
    val running: Boolean = false,
    val error: String? = null,
    val requestId: String? = null,
)

class ChatViewModel(app: Application) : AndroidViewModel(app) {
    private val store = SettingsStore(app)
    private val ws = NoemaWsClient(store)
    private val _ui = MutableStateFlow(ChatUiState(settings = store.snapshot()))
    val ui: StateFlow<ChatUiState> = _ui.asStateFlow()

    init {
        viewModelScope.launch {
            ws.events.collect { env ->
                when (env.type) {
                    "content" -> appendAssistant(env.payloadText())
                    "done" -> {
                        val rest = env.payloadText()
                        if (rest.isNotBlank()) appendAssistant(rest)
                        _ui.update { it.copy(running = false, requestId = null) }
                    }
                    "cancelled" -> _ui.update { it.copy(running = false, requestId = null, error = "已取消") }
                    "error" -> _ui.update { it.copy(running = false, error = env.payloadText().ifBlank { "error" }) }
                    "hello" -> _ui.update { it.copy(settings = store.snapshot()) }
                }
            }
        }
        viewModelScope.launch { ws.state.collect { state -> _ui.update { it.copy(socket = state) } } }
        viewModelScope.launch { ws.lastError.collect { err -> _ui.update { it.copy(error = err) } } }
        ws.connect()
    }

    fun updateDraft(value: String) { _ui.update { it.copy(draft = value) } }

    fun saveEndpoint(wsUrl: String, apiKey: String) {
        store.save(wsUrl, apiKey)
        _ui.update { it.copy(settings = store.snapshot()) }
        ws.disconnect()
        ws.connect()
    }

    fun send() {
        val text = _ui.value.draft.trim()
        if (text.isEmpty() || _ui.value.running) return
        val requestId = UUID.randomUUID().toString()
        val sent = ws.sendChat(text, requestId)
        if (sent == null) {
            _ui.update { it.copy(error = "未连接") }
            return
        }
        _ui.update {
            it.copy(
                draft = "",
                running = true,
                requestId = sent,
                error = null,
                lines = it.lines + ChatLine(UUID.randomUUID().toString(), "user", capUiText(text)),
            )
        }
    }

    fun stop() {
        val id = _ui.value.requestId ?: return
        ws.cancel(id)
    }

    override fun onCleared() {
        ws.disconnect()
        super.onCleared()
    }

    private fun appendAssistant(piece: String) {
        if (piece.isEmpty()) return
        _ui.update { state ->
            val last = state.lines.lastOrNull()
            val next = if (last?.role == "assistant") {
                state.lines.dropLast(1) + last.copy(text = capUiText(last.text + piece))
            } else {
                state.lines + ChatLine(UUID.randomUUID().toString(), "assistant", capUiText(piece))
            }
            state.copy(lines = next)
        }
    }
}
