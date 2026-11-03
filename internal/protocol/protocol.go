package protocol

import (
	"encoding/json"
	"time"
)

const Version = "noema.v1"

const (
	TypeChat           = "chat"
	TypeCancel         = "cancel"
	TypeReplay         = "replay"
	TypePing           = "ping"
	TypePong           = "pong"
	TypeContent        = "content"
	TypeDone           = "done"
	TypeError          = "error"
	TypeCancelled      = "cancelled"
	TypeCapabilities   = "capabilities"
	TypeReplayComplete = "replay_complete"
	TypeHello          = "hello"
)

// Envelope 是 Noema 客户端与接入层之间的产品协议，不是 Lucky Agent 的 WS 字段。
type Envelope struct {
	Protocol        string          `json:"protocol"`
	Type            string          `json:"type"`
	EventID         string          `json:"event_id,omitempty"`
	Seq             int64           `json:"seq,omitempty"`
	RunID           string          `json:"run_id,omitempty"`
	ClientRequestID string          `json:"client_request_id,omitempty"`
	SessionID       string          `json:"session_id,omitempty"`
	CharacterID     string          `json:"character_id,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	Payload         json.RawMessage `json:"payload,omitempty"`
}

type ChatPayload struct {
	Message string `json:"message"`
}

type ErrorPayload struct {
	Error string `json:"error"`
}

type ContentPayload struct {
	Text string `json:"text"`
}

type HelloPayload struct {
	SessionID    string          `json:"session_id"`
	CharacterID  string          `json:"character_id"`
	Capabilities json.RawMessage `json:"capabilities"`
}

type ReplayPayload struct {
	LastSeq int64 `json:"last_seq"`
}

func (e Envelope) Bytes() ([]byte, error) {
	if e.Protocol == "" {
		e.Protocol = Version
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	return json.Marshal(e)
}
