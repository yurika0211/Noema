package app.noema.android.ui

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color

// 与 res/values/colors.xml 同名同值，再与 apps/web/index.html :root 手对齐。
object NoemaColors {
    val noema_bg = Color(0xFFF4F1EA)
    val noema_ink = Color(0xFF1C1A16)
    val noema_accent = Color(0xFF355E4B)
    val noema_paper = Color(0xFFFBF8F1)
    val noema_mute = Color(0xFF7A756C)
    val noema_you = Color(0xFFEBE4D6)
    val noema_stop = Color(0xFF7A3A32)
}

private val NoemaScheme = lightColorScheme(
    primary = NoemaColors.noema_accent,
    onPrimary = NoemaColors.noema_paper,
    background = NoemaColors.noema_bg,
    onBackground = NoemaColors.noema_ink,
    surface = NoemaColors.noema_paper,
    onSurface = NoemaColors.noema_ink,
    error = NoemaColors.noema_stop,
    onError = NoemaColors.noema_paper,
)

@Composable
fun NoemaTheme(content: @Composable () -> Unit) {
    MaterialTheme(colorScheme = NoemaScheme, content = content)
}
