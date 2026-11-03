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
import androidx.compose.ui.graphics.Color
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
            .background(Color(0xFFF4F1EA))
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text("灯織", style = MaterialTheme.typography.headlineSmall, color = Color(0xFF1C1A16))
        Text(
            "连接 ${state.socket.name.lowercase()} · session ${state.settings.sessionId.ifBlank { "未分配" }}",
            style = MaterialTheme.typography.bodySmall,
            color = Color(0xFF5C574E),
        )
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
        LazyColumn(
            state = list,
            modifier = Modifier
                .weight(1f)
                .fillMaxWidth(),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            items(state.lines, key = { it.id }) { line ->
                val mine = line.role == "user"
                Text(
                    text = if (mine) "你：${line.text}" else "灯織：${line.text}",
                    color = Color(0xFF1C1A16),
                    modifier = Modifier
                        .fillMaxWidth()
                        .background(if (mine) Color(0xFFE7E1D4) else Color(0xFFDCE6DF))
                        .padding(12.dp),
                )
            }
        }
        state.error?.let { Text(it, color = Color(0xFF8A3B2A)) }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.fillMaxWidth()) {
            OutlinedTextField(
                value = state.draft,
                onValueChange = model::updateDraft,
                modifier = Modifier.weight(1f),
                label = { Text("和灯織说话") },
                enabled = !state.running,
            )
            if (state.running) {
                OutlinedButton(onClick = model::stop) { Text("停止") }
            } else {
                Button(
                    onClick = model::send,
                    enabled = state.draft.isNotBlank() && state.socket != SocketState.Error,
                ) { Text("发送") }
            }
        }
    }
}
