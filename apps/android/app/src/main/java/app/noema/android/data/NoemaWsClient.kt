package app.noema.android.data

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import java.util.UUID
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicReference

enum class SocketState { Idle, Connecting, Connected, Running, Reconnecting, Error, Closed }

class NoemaWsClient(
    private val settings: SettingsStore,
) {
    private val json = Json { ignoreUnknownKeys = true; encodeDefaults = true }
    private val client = OkHttpClient.Builder()
        .connectTimeout(15, TimeUnit.SECONDS)
        .readTimeout(0, TimeUnit.MILLISECONDS)
        .pingInterval(25, TimeUnit.SECONDS)
        .build()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val socketRef = AtomicReference<WebSocket?>(null)
    private val userClosed = AtomicBoolean(false)
    private val reconnectAttempt = AtomicInteger(0)
    private var reconnectJob: Job? = null

    private val _state = MutableStateFlow(SocketState.Idle)
    val state: StateFlow<SocketState> = _state.asStateFlow()

    private val _events = MutableSharedFlow<Envelope>(extraBufferCapacity = 128)
    val events: SharedFlow<Envelope> = _events.asSharedFlow()

    private val _lastError = MutableStateFlow<String?>(null)
    val lastError: StateFlow<String?> = _lastError.asStateFlow()

    fun connect() {
        userClosed.set(false)
        open(isReconnect = false)
    }

    fun disconnect() {
        userClosed.set(true)
        reconnectJob?.cancel()
        socketRef.getAndSet(null)?.close(1000, "closed")
        _state.value = SocketState.Closed
    }

    fun sendChat(message: String, requestId: String = UUID.randomUUID().toString()): String? {
        val ws = socketRef.get() ?: return null
        val env = chatEnvelope(requestId, settings.snapshot().sessionId.ifBlank { null }, message)
        return if (ws.send(json.encodeToString(env))) requestId else null
    }

    fun cancel(requestId: String): Boolean {
        val ws = socketRef.get() ?: return false
        return ws.send(json.encodeToString(cancelEnvelope(requestId, settings.snapshot().sessionId.ifBlank { null })))
    }

    private fun open(isReconnect: Boolean) {
        val snap = settings.snapshot()
        val url = buildWsUrl(snap.wsUrl, snap.sessionId)
        val builder = Request.Builder().url(url)
        if (snap.apiKey.isNotBlank()) {
            builder.header("Authorization", "Bearer ${snap.apiKey}")
            builder.header("X-API-Key", snap.apiKey)
        }
        _state.value = if (isReconnect) SocketState.Reconnecting else SocketState.Connecting
        val previous = socketRef.get()
        val ws = client.newWebSocket(builder.build(), listener(isReconnect))
        socketRef.set(ws)
        if (previous != null && previous !== ws) previous.close(1000, "reconnect")
    }

    private fun listener(isReconnect: Boolean) = object : WebSocketListener() {
        override fun onOpen(webSocket: WebSocket, response: Response) {
            reconnectAttempt.set(0)
            _state.value = SocketState.Connected
            _lastError.value = null
            if (isReconnect) {
                val last = settings.snapshot().lastSeq
                webSocket.send(json.encodeToString(replayEnvelope(last, settings.snapshot().sessionId.ifBlank { null })))
            }
        }

        override fun onMessage(webSocket: WebSocket, text: String) {
            if (text.length > MAX_INBOUND_CHARS) {
                inboundSeq(text).takeIf { it > 0 }?.let { settings.saveLastSeq(it) }
                _lastError.value = "WebSocket message too large"
                _state.value = SocketState.Error
                return
            }
            val env = runCatching { json.decodeFromString(Envelope.serializer(), text) }.getOrNull()
            if (env == null) {
                _lastError.value = "invalid frame"
                return
            }
            env.seq?.let { settings.saveLastSeq(it) }
            env.helloSessionId()?.takeIf { it.isNotBlank() }?.let { settings.saveSession(it) }
            when (env.type) {
                "content" -> _state.value = SocketState.Running
                "done", "cancelled", "hello", "replay_complete" -> _state.value = SocketState.Connected
                "error" -> _state.value = SocketState.Error
            }
            _events.tryEmit(env)
        }

        override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
            _lastError.value = t.message
            scheduleReconnect()
        }

        override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
            if (!userClosed.get()) scheduleReconnect()
        }
    }

    private fun scheduleReconnect() {
        if (userClosed.get()) return
        val attempt = reconnectAttempt.incrementAndGet()
        if (attempt > MAX_RECONNECT_ATTEMPTS) {
            _state.value = SocketState.Error
            return
        }
        val delayMs = (RECONNECT_BASE_MS * attempt).coerceAtMost(RECONNECT_MAX_MS)
        _state.value = SocketState.Reconnecting
        reconnectJob?.cancel()
        reconnectJob = scope.launch {
            delay(delayMs)
            if (!userClosed.get()) open(isReconnect = true)
        }
    }
}

fun buildWsUrl(raw: String, sessionId: String): String {
    val trimmed = raw.trim().ifBlank { "ws://10.0.2.2:8787/v1/ws" }
    val withScheme = when {
        trimmed.startsWith("ws://") || trimmed.startsWith("wss://") -> trimmed
        trimmed.startsWith("http://") -> "ws://" + trimmed.removePrefix("http://")
        trimmed.startsWith("https://") -> "wss://" + trimmed.removePrefix("https://")
        else -> "ws://$trimmed"
    }
    val base = if (withScheme.contains("/v1/ws")) withScheme else withScheme.trimEnd('/') + "/v1/ws"
    if (sessionId.isBlank()) return base
    val sep = if (base.contains("?")) "&" else "?"
    return base + sep + "session=" + sessionId
}
