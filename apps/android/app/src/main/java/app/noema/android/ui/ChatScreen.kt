package app.noema.android.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import app.noema.android.data.SocketState

@Composable
fun ChatScreen(model: ChatViewModel) {
    val state by model.ui.collectAsStateWithLifecycle()
    val list = rememberLazyListState()
    var wsUrl by rememberSaveable { mutableStateOf(state.settings.wsUrl) }
    var apiKey by rememberSaveable { mutableStateOf(state.settings.apiKey) }
    LaunchedEffect(state.lines.size) {
        if (state.lines.isNotEmpty()) list.animateScrollToItem(state.lines.lastIndex)
    }
    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(NoemaColors.noema_bg)
            .padding(NoemaSpace.space_block),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text("灯織", style = MaterialTheme.typography.headlineSmall, color = NoemaColors.noema_ink)
        Text(
            "连接 ${state.socket.name.lowercase()} · session ${state.settings.sessionId.ifBlank { "未分配" }}",
            style = MaterialTheme.typography.bodySmall,
            color = NoemaColors.noema_mute,
        )
        var setupOpen by rememberSaveable { mutableStateOf(false) }
        OutlinedButton(onClick = { setupOpen = !setupOpen }) {
            Text(if (setupOpen) "收起连接" else "连接")
        }
        if (setupOpen) {
            OutlinedTextField(
                value = wsUrl,
                onValueChange = { wsUrl = it },
                label = { Text("Noema WebSocket") },
                modifier = Modifier.fillMaxWidth(),
                singleLine = true,
            )
            OutlinedTextField(
                value = apiKey,
                onValueChange = { apiKey = it },
                label = { Text("API key（可选）") },
                modifier = Modifier.fillMaxWidth(),
                singleLine = true,
            )
            OutlinedButton(onClick = { model.saveEndpoint(wsUrl, apiKey) }) { Text("连接") }
        }
        LazyColumn(
            state = list,
            modifier = Modifier
                .weight(1f)
                .fillMaxWidth(),
            verticalArrangement = Arrangement.spacedBy(NoemaSpace.space_tight),
        ) {
            items(state.lines, key = { it.id }) { line ->
                val mine = line.role == "user"
                Text(
                    text = if (mine) "你：${line.text}" else "灯織：${line.text}",
                    color = NoemaColors.noema_ink,
                    modifier = Modifier
                        .fillMaxWidth()
                        .background(if (mine) NoemaColors.noema_you else NoemaColors.noema_paper)
                        .padding(12.dp),
                )
            }
        }
        state.error?.let { Text(it, color = NoemaColors.noema_stop) }
        Row(horizontalArrangement = Arrangement.spacedBy(NoemaSpace.space_tight), modifier = Modifier.fillMaxWidth()) {
            OutlinedTextField(
                value = state.draft,
                onValueChange = model::updateDraft,
                modifier = Modifier.weight(1f),
                label = { Text("和灯織说话") },
                enabled = !state.running,
            )
            if (state.running) {
                OutlinedButton(
                    onClick = model::stop,
                    colors = ButtonDefaults.outlinedButtonColors(contentColor = NoemaColors.noema_stop),
                ) { Text("停止") }
            } else {
                Button(
                    onClick = model::send,
                    enabled = state.draft.isNotBlank() && state.socket != SocketState.Error,
                ) { Text("发送") }
            }
        }
    }
}
