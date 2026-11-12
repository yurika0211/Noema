package estuary

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yurika0211/Noema/internal/protocol"
)

func TestAppendRestartIsolationAndFictional(t *testing.T) {
	dir := t.TempDir()
	led, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if led.Dir() == "" || strings.Contains(led.Dir(), "lucky-home") || strings.Contains(led.Path(), ".luckyagent") {
		t.Fatalf("ledger escaped into Lucky home: dir=%s path=%s", led.Dir(), led.Path())
	}
	if !strings.HasSuffix(led.Path(), filepath.Join("estuary", "events.jsonl")) {
		t.Fatalf("path=%s", led.Path())
	}

	now := time.Date(2026, 11, 12, 1, 2, 3, 0, time.UTC)
	seq := int64(7)
	shared := "evt_shared"
	userRec, err := led.Append(Record{
		SubjectID:  SubjectUser,
		Namespace:  NamespaceEvent,
		EventID:    shared,
		SessionID:  "sess-1",
		Seq:        &seq,
		Kind:       KindChatTurn,
		CreatedAt:  now,
		Provenance: &Provenance{Source: SourceUser, Fictional: false, CharacterID: CharacterID},
		Payload:    map[string]any{"role": "user", "text": "用户私语"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if userRec.EventID != shared || userRec.SubjectID != SubjectUser {
		t.Fatalf("user=%+v", userRec)
	}
	_, err = led.Append(Record{
		SubjectID:  SubjectCharacter,
		Namespace:  NamespaceEvent,
		EventID:    shared,
		SessionID:  "sess-1",
		Seq:        &seq,
		Kind:       KindChatTurn,
		CreatedAt:  now,
		Provenance: &Provenance{Source: SourceCharacter, Fictional: false, CharacterID: CharacterID},
		Payload:    map[string]any{"role": "assistant", "text": "灯織只对自己说"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = led.Append(Record{
		SubjectID:  SubjectCharacter,
		Namespace:  NamespaceEvent,
		EventID:    "evt_fiction",
		SessionID:  "sess-1",
		Kind:       "memory_write",
		CreatedAt:  now,
		Provenance: &Provenance{Source: SourceCharacter, Fictional: true, CharacterID: CharacterID},
		Payload:    map[string]any{"text": "虚构经历"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := led.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	userView, err := reopened.List(Filter{SubjectID: SubjectUser, Namespace: NamespaceEvent})
	if err != nil {
		t.Fatal(err)
	}
	if len(userView) != 1 {
		t.Fatalf("user view=%+v", userView)
	}
	if textOf(userView[0]) != "用户私语" {
		t.Fatalf("user payload leaked or lost: %+v", userView[0].Payload)
	}
	if strings.Contains(mustJSON(userView[0]), "灯織只对自己说") || strings.Contains(mustJSON(userView[0]), "虚构经历") {
		t.Fatalf("user view contains other subject payload: %s", mustJSON(userView[0]))
	}

	charView, err := reopened.List(Filter{SubjectID: SubjectCharacter, Namespace: NamespaceEvent})
	if err != nil {
		t.Fatal(err)
	}
	if len(charView) != 2 {
		t.Fatalf("character view=%+v", charView)
	}
	for _, rec := range charView {
		if strings.Contains(mustJSON(rec), "用户私语") {
			t.Fatalf("character view leaked user payload: %s", mustJSON(rec))
		}
	}

	views, err := reopened.Get(shared)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 2 {
		t.Fatalf("views=%+v", views)
	}
	if textOf(views[SubjectUser]) != "用户私语" || textOf(views[SubjectCharacter]) != "灯織只对自己说" {
		t.Fatalf("mixed payloads: user=%v character=%v", views[SubjectUser].Payload, views[SubjectCharacter].Payload)
	}
	if views[SubjectUser].SubjectID == views[SubjectCharacter].SubjectID {
		t.Fatal("subjects collapsed")
	}

	fictional := true
	onlyFiction, err := reopened.List(Filter{SubjectID: SubjectCharacter, Namespace: NamespaceEvent, Fictional: &fictional})
	if err != nil {
		t.Fatal(err)
	}
	if len(onlyFiction) != 1 || !onlyFiction[0].Provenance.Fictional || onlyFiction[0].EventID != "evt_fiction" {
		t.Fatalf("fictional filter=%+v", onlyFiction)
	}
	real := false
	onlyReal, err := reopened.List(Filter{SubjectID: SubjectCharacter, Namespace: NamespaceEvent, Fictional: &real})
	if err != nil {
		t.Fatal(err)
	}
	if len(onlyReal) != 1 || fictionalOf(onlyReal[0]) {
		t.Fatalf("real filter promoted fiction: %+v", onlyReal)
	}

	after := int64(6)
	later, err := reopened.List(Filter{SubjectID: SubjectUser, Namespace: NamespaceEvent, AfterSeq: &after})
	if err != nil {
		t.Fatal(err)
	}
	if len(later) != 1 || later[0].EventID != shared {
		t.Fatalf("after=%+v", later)
	}
}

func TestTornTailTruncatedBeforeNextAppend(t *testing.T) {
	dir := t.TempDir()
	led, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 12, 4, 0, 0, 0, time.UTC)
	if _, err := led.Append(Record{
		SubjectID: SubjectUser,
		Namespace: NamespaceEvent,
		EventID:   "evt_1",
		CreatedAt: now,
		Payload:   map[string]any{"text": "完整行"},
	}); err != nil {
		t.Fatal(err)
	}
	path := led.Path()
	if err := led.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	torn := `{"subject_id":"character","namespace":"event","event_id":"evt_torn","created_at":"2026-11-12T04:00:01Z","payload":{"text":"半行`
	if err := os.WriteFile(path, append(mustRead(t, path), []byte(torn)...), 0o600); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != before.Size() {
		t.Fatalf("torn tail not truncated: size=%d want %d", info.Size(), before.Size())
	}
	if _, err := reopened.Append(Record{
		SubjectID: SubjectCharacter,
		Namespace: NamespaceEvent,
		EventID:   "evt_2",
		CreatedAt: now.Add(time.Second),
		Payload:   map[string]any{"text": "截尾后的新行"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}

	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	user, err := again.List(Filter{SubjectID: SubjectUser, Namespace: NamespaceEvent})
	if err != nil {
		t.Fatal(err)
	}
	character, err := again.List(Filter{SubjectID: SubjectCharacter, Namespace: NamespaceEvent})
	if err != nil {
		t.Fatal(err)
	}
	if len(user) != 1 || user[0].EventID != "evt_1" {
		t.Fatalf("user=%+v", user)
	}
	if len(character) != 1 || character[0].EventID != "evt_2" {
		t.Fatalf("character=%+v want only evt_2", character)
	}
	raw := mustRead(t, path)
	if !bytes.HasSuffix(raw, []byte("\n")) {
		t.Fatalf("file does not end with newline: %q", raw)
	}
	if bytes.Contains(raw, []byte("evt_torn")) {
		t.Fatalf("torn id survived: %s", raw)
	}
	for i, line := range bytes.Split(bytes.TrimSuffix(raw, []byte("\n")), []byte("\n")) {
		var rec Record
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatalf("line %d not a complete record: %q: %v", i, line, err)
		}
	}
}

func TestAppendChatTurnSharesEventIDWithoutMixingPayload(t *testing.T) {
	led, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer led.Close()
	seq := int64(3)
	env := protocol.Envelope{
		Protocol:    protocol.Version,
		Type:        protocol.TypeDone,
		EventID:     "evt_3",
		Seq:         seq,
		SessionID:   "sess-9",
		CharacterID: "hiori",
		RunID:       "run_1",
		CreatedAt:   time.Date(2026, 11, 12, 8, 0, 0, 0, time.UTC),
	}
	written, err := AppendChatTurn(led, ChatTurnInput{
		Envelope:      env,
		UserMessage:   "你好",
		AssistantText: "灯織在。",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 2 {
		t.Fatalf("written=%d", len(written))
	}
	views, err := led.Get("evt_3")
	if err != nil {
		t.Fatal(err)
	}
	if views[SubjectUser].SessionID != "sess-9" || views[SubjectUser].SubjectID == "sess-9" {
		t.Fatalf("session used as subject: %+v", views[SubjectUser])
	}
	if views[SubjectUser].Seq == nil || *views[SubjectUser].Seq != 3 {
		t.Fatalf("seq=%v", views[SubjectUser].Seq)
	}
	if textOf(views[SubjectUser]) != "你好" || textOf(views[SubjectCharacter]) != "灯織在。" {
		t.Fatalf("payloads user=%v character=%v", views[SubjectUser].Payload, views[SubjectCharacter].Payload)
	}
	if views[SubjectUser].Provenance.Fictional || views[SubjectCharacter].Provenance.Source != SourceCharacter {
		t.Fatalf("provenance user=%+v character=%+v", views[SubjectUser].Provenance, views[SubjectCharacter].Provenance)
	}
}

func TestRejectsSchemaViolationsAndEmptyStateDir(t *testing.T) {
	if _, err := Open("  "); err == nil {
		t.Fatal("empty state dir must fail")
	}
	led, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer led.Close()
	_, err = led.Append(Record{Namespace: NamespaceEvent, EventID: "e", CreatedAt: time.Now().UTC()})
	if err == nil {
		t.Fatal("missing subject_id")
	}
	_, err = led.Append(Record{SubjectID: SubjectUser, Namespace: "session", EventID: "e", CreatedAt: time.Now().UTC()})
	if err == nil {
		t.Fatal("invalid namespace")
	}
	if _, err := led.List(Filter{SubjectID: SubjectUser}); err == nil {
		t.Fatal("list without namespace")
	}
}

func textOf(rec Record) string {
	if rec.Payload == nil {
		return ""
	}
	text, _ := rec.Payload["text"].(string)
	return text
}

func mustJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(raw)
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
