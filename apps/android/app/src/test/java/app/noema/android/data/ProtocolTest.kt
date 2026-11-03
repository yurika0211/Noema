package app.noema.android.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive

class ProtocolTest {
    @Test
    fun oversizedFrameStillFindsEventId() {
        val prefix = """{"protocol":"noema.v1","type":"content","event_id":"evt_18","seq":18,"payload":{"text":""""
        val body = "x".repeat(1000)
        val json = prefix + body + """"}}"""
        assertEquals("evt_18", inboundEventCursor(json))
        assertEquals(18L, inboundSeq(json))
    }

    @Test
    fun buildWsUrlAddsSession() {
        assertEquals(
            "ws://10.0.2.2:8787/v1/ws?session=abc",
            buildWsUrl("http://10.0.2.2:8787", "abc"),
        )
        assertEquals(
            "wss://example.test/v1/ws",
            buildWsUrl("https://example.test", ""),
        )
    }

    @Test
    fun reconnectEnvelopesCarryCursorAndRequestId() {
        val replay = replayEnvelope(42, "sess")
        assertEquals("replay", replay.type)
        assertEquals("sess", replay.sessionId)
        assertEquals("42", replay.payload?.jsonObject?.get("last_seq")?.jsonPrimitive?.content)

        // 同一个 client_request_id 重发时服务端幂等，不会新开回合。
        val chat = chatEnvelope("req-1", "sess", "你好")
        assertEquals("req-1", chat.clientRequestId)
        assertEquals("你好", chat.payload?.jsonObject?.get("message")?.jsonPrimitive?.content)

        val cancel = cancelEnvelope("req-1", "sess")
        assertEquals("cancel", cancel.type)
        assertEquals("req-1", cancel.clientRequestId)
    }

    @Test
    fun capUiTextMarksTruncation() {
        val out = capUiText("a".repeat(40), 10)
        assertTrue(out.startsWith("aaaaaaaaaa"))
        assertTrue(out.contains("内容已截断"))
    }
}
