package app.noema.android.ui

import org.junit.Assert.assertFalse
import org.junit.Test
import java.io.File

class ChatScreenSourceTest {
    @Test
    fun chatScreenHasNoInlineColorLiterals() {
        val root = File("src/main/java/app/noema/android/ui/ChatScreen.kt")
        val source = root.readText()
        assertFalse(source.contains("Color(0xFF"))
        assertFalse(source.contains("Color(0xff"))
    }
}
