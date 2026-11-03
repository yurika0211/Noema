package eventlog

import (
	"errors"
	"testing"

	"github.com/yurika0211/Noema/internal/engine"
	"github.com/yurika0211/Noema/internal/protocol"
)

func TestAppendAndReplay(t *testing.T) {
	s := New()
	first, err := s.Append("sess", protocol.Envelope{Type: protocol.TypeContent, ClientRequestID: "req"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Append("sess", protocol.Envelope{Type: protocol.TypeDone, ClientRequestID: "req"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Seq != 1 || second.Seq != 2 {
		t.Fatalf("seq %d %d", first.Seq, second.Seq)
	}
	replay := s.After("sess", 1)
	if len(replay) != 1 || replay[0].Type != protocol.TypeDone {
		t.Fatalf("replay=%+v", replay)
	}
	if got := s.ForRequest("sess", "req"); len(got) != 2 {
		t.Fatalf("for request=%d", len(got))
	}
}

func TestBeginRunIdempotentAndConflict(t *testing.T) {
	s := New()
	id1, replay, err := s.BeginRun("owner", "sess", "req-1", "你好")
	if err != nil || replay || id1 == "" {
		t.Fatalf("first: id=%s replay=%v err=%v", id1, replay, err)
	}
	id2, replay, err := s.BeginRun("owner", "sess", "req-1", "你好")
	if err != nil || !replay || id2 != id1 {
		t.Fatalf("same input: id=%s replay=%v err=%v", id2, replay, err)
	}
	_, _, err = s.BeginRun("owner", "sess", "req-1", "另一句")
	if !errors.Is(err, engine.ErrInvalidRequest) {
		t.Fatalf("conflict: %v", err)
	}
}
