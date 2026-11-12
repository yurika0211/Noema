package estuary

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yurika0211/Noema/internal/protocol"
)

func TestSubjectIndexIsolationRestartAndFictional(t *testing.T) {
	dir := t.TempDir()
	led, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	idx, err := OpenIndex(dir, HashEmbedder{Dim: 32})
	if err != nil {
		t.Fatal(err)
	}
	led.AttachIndex(idx)

	env := protocol.Envelope{
		Type:        protocol.TypeDone,
		EventID:     "evt_shared_index",
		Seq:         4,
		SessionID:   "sess-idx",
		CharacterID: CharacterID,
		CreatedAt:   time.Date(2026, 11, 13, 2, 0, 0, 0, time.UTC),
	}
	if _, err := AppendChatTurn(led, ChatTurnInput{
		Envelope:      env,
		UserMessage:   "用户私语 青柠汽水",
		AssistantText: "灯織只对自己说 晚霞",
	}); err != nil {
		t.Fatal(err)
	}
	fiction := env
	fiction.EventID = "evt_fiction_index"
	if _, err := AppendChatTurn(led, ChatTurnInput{
		Envelope:      fiction,
		UserMessage:   "虚构经历 龙城",
		AssistantText: "虚构旁白 龙城",
		Fictional:     true,
	}); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	userHits, err := led.Search(ctx, SubjectUser, "青柠汽水")
	if err != nil {
		t.Fatal(err)
	}
	if len(userHits) == 0 || !strings.Contains(userHits[0].Content, "青柠汽水") {
		t.Fatalf("user hits=%+v", userHits)
	}
	for _, hit := range userHits {
		if hit.SubjectID != SubjectUser {
			t.Fatalf("user search returned %s", hit.SubjectID)
		}
		if strings.Contains(hit.Content, "灯織只对自己说") || strings.Contains(hit.Content, "龙城") {
			t.Fatalf("user index leaked: %+v", hit)
		}
	}
	charHits, err := led.Search(ctx, SubjectCharacter, "晚霞")
	if err != nil {
		t.Fatal(err)
	}
	if len(charHits) == 0 || !strings.Contains(charHits[0].Content, "晚霞") {
		t.Fatalf("character hits=%+v", charHits)
	}
	for _, hit := range charHits {
		if strings.Contains(hit.Content, "青柠汽水") || strings.Contains(hit.Content, "龙城") {
			t.Fatalf("character index leaked: %+v", hit)
		}
	}
	fictionHits, err := led.Search(ctx, SubjectUser, "龙城")
	if err != nil {
		t.Fatal(err)
	}
	for _, hit := range fictionHits {
		if strings.Contains(hit.Content, "龙城") || hit.EventID == "evt_fiction_index" {
			t.Fatalf("fictional entered real index: %+v", hit)
		}
	}

	if err := idx.Close(); err != nil {
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
	again, err := OpenIndex(dir, HashEmbedder{Dim: 32})
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	reopened.AttachIndex(again)
	userHits, err = reopened.Search(ctx, SubjectUser, "青柠汽水")
	if err != nil {
		t.Fatal(err)
	}
	if len(userHits) == 0 || !strings.Contains(userHits[0].Content, "青柠汽水") {
		t.Fatalf("restart user hits=%+v", userHits)
	}
	for _, hit := range userHits {
		if strings.Contains(hit.Content, "晚霞") {
			t.Fatalf("restart leaked character text: %+v", hit)
		}
	}
}
