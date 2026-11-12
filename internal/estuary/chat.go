package estuary

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/yurika0211/Noema/internal/protocol"
)

// ChatTurnInput 是一轮已完成对话写进账本所需的信封与正文。
// UserMessage 是用户视角，AssistantText 是角色视角；两者可以共享 event_id。
type ChatTurnInput struct {
	Envelope      protocol.Envelope
	UserMessage   string
	AssistantText string
	// Fictional 为 true 时两条记录都标 fictional，不得升格成现实。
	Fictional bool
	Now       time.Time
}

// AppendChatTurn 为一轮完成的对话追加 namespace=event、kind=chat_turn 的双主体记录。
// user 记录只含用户消息，character 记录只含助手正文（没有助手正文则不写 character 行）。
// session_id、seq、event_id 来自信封，不用 session_id 充当 subject_id。
func AppendChatTurn(led *Ledger, in ChatTurnInput) ([]Record, error) {
	if led == nil {
		return nil, errNil()
	}
	env := in.Envelope
	eventID := strings.TrimSpace(env.EventID)
	if eventID == "" {
		return nil, errEventID()
	}
	created := in.Now
	if created.IsZero() {
		created = env.CreatedAt
	}
	if created.IsZero() {
		created = time.Now().UTC()
	}
	characterID := strings.TrimSpace(env.CharacterID)
	if characterID == "" {
		characterID = CharacterID
	}
	var seq *int64
	if env.Seq > 0 {
		s := env.Seq
		seq = &s
	}
	user := Record{
		SubjectID: SubjectUser,
		Namespace: NamespaceEvent,
		EventID:   eventID,
		SessionID: env.SessionID,
		Seq:       seq,
		Kind:      KindChatTurn,
		CreatedAt: created.UTC(),
		Provenance: &Provenance{
			Source:      SourceUser,
			Fictional:   in.Fictional,
			CharacterID: characterID,
		},
		Payload: map[string]any{
			"role": "user",
			"text": in.UserMessage,
		},
	}
	if env.RunID != "" {
		user.Payload["run_id"] = env.RunID
	}
	written := make([]Record, 0, 2)
	stored, err := led.Append(user)
	if err != nil {
		return nil, err
	}
	written = append(written, stored)
	if err := led.project(stored); err != nil {
		return written, err
	}

	assistant := strings.TrimSpace(in.AssistantText)
	if assistant == "" {
		return written, nil
	}
	character := Record{
		SubjectID: SubjectCharacter,
		Namespace: NamespaceEvent,
		EventID:   eventID,
		SessionID: env.SessionID,
		Seq:       seq,
		Kind:      KindChatTurn,
		CreatedAt: created.UTC(),
		Provenance: &Provenance{
			Source:      SourceCharacter,
			Fictional:   in.Fictional,
			CharacterID: characterID,
		},
		Payload: map[string]any{
			"role": "assistant",
			"text": assistant,
		},
	}
	if env.RunID != "" {
		character.Payload["run_id"] = env.RunID
	}
	stored, err = led.Append(character)
	if err != nil {
		return written, err
	}
	written = append(written, stored)
	if err := led.project(stored); err != nil {
		return written, err
	}
	return written, nil
}

func (l *Ledger) project(rec Record) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	idx := l.index
	l.mu.Unlock()
	if idx == nil {
		return nil
	}
	return idx.Project(context.Background(), rec)
}

// UserText 从 chat 请求 payload 取出用户消息。解析失败返回空串。
func UserText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var body protocol.ChatPayload
	if err := json.Unmarshal(raw, &body); err != nil {
		return ""
	}
	return body.Message
}

// AssistantText 从 done/content payload 取出助手正文。
func AssistantText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var body protocol.ContentPayload
	if err := json.Unmarshal(raw, &body); err != nil {
		return ""
	}
	return body.Text
}

func errNil() error { return errString("estuary: nil ledger") }

func errEventID() error { return errString("estuary: event_id is required") }

type errString string

func (e errString) Error() string { return string(e) }
