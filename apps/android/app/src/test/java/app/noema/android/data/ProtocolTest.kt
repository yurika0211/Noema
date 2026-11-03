package app.noema.android.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

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
    fun capUiTextMarksTruncation() {
        val out = capUiText("a".repeat(40), 10)
        assertTrue(out.startsWith("aaaaaaaaaa"))
        assertTrue(out.contains("内容已截断"))
    }
}
