package app.noema.android

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.viewModels
import app.noema.android.ui.ChatScreen
import app.noema.android.ui.ChatViewModel
import app.noema.android.ui.NoemaTheme

class MainActivity : ComponentActivity() {
    private val model: ChatViewModel by viewModels()

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent {
            NoemaTheme {
                ChatScreen(model)
            }
        }
    }
}
