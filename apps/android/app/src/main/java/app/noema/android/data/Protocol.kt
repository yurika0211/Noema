package app.noema.android.data

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonPrimitive

const val PROTOCOL = "noema.v1"
const val MAX_INBOUND_CHARS = 8_000_000
const val MAX_UI_MESSAGE_CHARS = 32_000
const val RECONNECT_BASE_MS = 50L
const val RECONNECT_MAX_MS = 250L
const val MAX_RECONNECT_ATTEMPTS = 8

@Serializable
data class Envelope(
    val protocol: String = PROTOCOL,
    val type: String,
    @SerialName("event_id") val eventId: String? = null,
    val seq: Long? = null,
    @SerialName("run_id") val runId: String? = null,
    @SerialName("client_request_id") val clientRequestId: String? = null,
    @SerialName("session_id") val sessionId: String? = null,
    @SerialName("character_id") val characterId: String? = null,
    @SerialName("created_at") val createdAt: String? = null,
    val payload: JsonElement? = null,
)

fun Envelope.payloadText(): String {
    val obj = payload as? JsonObject ?: return ""
    return obj["text"]?.jsonPrimitive?.contentOrNull
        ?: obj["error"]?.jsonPrimitive?.contentOrNull
        ?: ""
}

fun Envelope.helloSessionId(): String? {
    val obj = payload as? JsonObject ?: return sessionId
    return obj["session_id"]?.jsonPrimitive?.contentOrNull ?: sessionId
}

fun chatEnvelope(requestId: String, sessionId: String?, message: String): Envelope = Envelope(
    type = "chat",
    clientRequestId = requestId,
    sessionId = sessionId,
    payload = JsonObject(mapOf("message" to JsonPrimitive(message))),
)

fun cancelEnvelope(requestId: String, sessionId: String?): Envelope = Envelope(
    type = "cancel",
    clientRequestId = requestId,
    sessionId = sessionId,
)

fun replayEnvelope(lastSeq: Long, sessionId: String?): Envelope = Envelope(
    type = "replay",
    sessionId = sessionId,
    payload = JsonObject(mapOf("last_seq" to JsonPrimitive(lastSeq))),
)

fun inboundEventCursor(text: String): String = quotedField(text, "event_id")

fun inboundSeq(text: String): Long {
    val key = "\"seq\""
    var from = 0
    while (from < text.length) {
        val at = text.indexOf(key, from)
        if (at < 0) return 0
        var i = at + key.length
        while (i < text.length && text[i].isWhitespace()) i++
        if (i >= text.length || text[i] != ':') {
            from = at + key.length
            continue
        }
        i++
        while (i < text.length && text[i].isWhitespace()) i++
        val start = i
        while (i < text.length && text[i].isDigit()) i++
        if (i > start) return text.substring(start, i).toLongOrNull() ?: 0
        from = at + key.length
    }
    return 0
}

private fun quotedField(text: String, field: String): String {
    val key = "\"$field\""
    var from = 0
    while (from < text.length) {
        val at = text.indexOf(key, from)
        if (at < 0) return ""
        var i = at + key.length
        while (i < text.length && text[i].isWhitespace()) i++
        if (i >= text.length || text[i] != ':') {
            from = at + key.length
            continue
        }
        i++
        while (i < text.length && text[i].isWhitespace()) i++
        if (i >= text.length || text[i] != '"') {
            from = at + key.length
            continue
        }
        i++
        val start = i
        while (i < text.length && text[i] != '"') {
            if (text[i] == '\\') i++
            i++
        }
        return text.substring(start, i.coerceAtMost(text.length))
    }
    return ""
}

fun capUiText(value: String, maxChars: Int = MAX_UI_MESSAGE_CHARS): String {
    if (value.length <= maxChars) return value
    return value.take(maxChars) + "\n…内容已截断…"
}
