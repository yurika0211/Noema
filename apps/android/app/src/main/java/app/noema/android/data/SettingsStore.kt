package app.noema.android.data

import android.content.Context
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey
import app.noema.android.BuildConfig
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

data class ClientSettings(
    val wsUrl: String = BuildConfig.DEFAULT_WS_URL,
    val apiKey: String = "",
    val sessionId: String = "",
    val lastSeq: Long = 0,
)

class SettingsStore(context: Context) {
    private val masterKey = MasterKey.Builder(context)
        .setKeyScheme(MasterKey.KeyScheme.AES256_GCM)
        .build()

    private val prefs = EncryptedSharedPreferences.create(
        context,
        "noema_secure_settings",
        masterKey,
        EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
        EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM,
    )

    private val _settings = MutableStateFlow(read())
    val settings: StateFlow<ClientSettings> = _settings.asStateFlow()

    fun snapshot(): ClientSettings = _settings.value

    fun save(wsUrl: String, apiKey: String) {
        prefs.edit()
            .putString(KEY_WS, wsUrl.trim())
            .putString(KEY_API, apiKey)
            .apply()
        _settings.value = read()
    }

    fun saveSession(sessionId: String) {
        prefs.edit().putString(KEY_SESSION, sessionId).apply()
        _settings.value = read()
    }

    fun saveLastSeq(seq: Long) {
        if (seq <= snapshot().lastSeq) return
        prefs.edit().putLong(KEY_SEQ, seq).apply()
        _settings.value = read()
    }

    private fun read(): ClientSettings = ClientSettings(
        wsUrl = prefs.getString(KEY_WS, BuildConfig.DEFAULT_WS_URL) ?: BuildConfig.DEFAULT_WS_URL,
        apiKey = prefs.getString(KEY_API, "") ?: "",
        sessionId = prefs.getString(KEY_SESSION, "") ?: "",
        lastSeq = prefs.getLong(KEY_SEQ, 0),
    )

    private companion object {
        const val KEY_WS = "ws_url"
        const val KEY_API = "api_key"
        const val KEY_SESSION = "session_id"
        const val KEY_SEQ = "last_seq"
    }
}
