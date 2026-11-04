package observe

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestFinishOmitsMessageBody(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf)
	start := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	log.now = func() time.Time { return start }
	turn := log.Start("", "sess", "run_1", "req_1")
	log.now = func() time.Time { return start.Add(40 * time.Millisecond) }
	log.FirstByte(turn, len("今晚想改剧本的开头"))
	log.now = func() time.Time { return start.Add(120 * time.Millisecond) }
	log.AddBytes(turn, 9)
	log.Finish(turn, "done", "")

	line := buf.String()
	for _, want := range []string{
		"protocol=noema.v1",
		"session_id=sess",
		"run_id=run_1",
		"client_request_id=req_1",
		"status=done",
		"err=-",
		"duration_ms=120",
		"first_byte_ms=40",
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("missing %s in %s", want, line)
		}
	}
	if strings.Contains(line, "今晚") || strings.Contains(line, "剧本") {
		t.Fatalf("message leaked into log: %s", line)
	}
	if !strings.Contains(line, "bytes=") || strings.Contains(line, "bytes=0") {
		t.Fatalf("bytes not counted: %s", line)
	}
}

func TestFirstByteOnceAndSecretLikeErrorCollapsed(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf)
	now := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	log.now = func() time.Time { return now }
	turn := log.Start("noema.v1", "s", "r", "c")
	log.now = func() time.Time { return now.Add(10 * time.Millisecond) }
	log.FirstByte(turn, 2)
	log.now = func() time.Time { return now.Add(50 * time.Millisecond) }
	log.FirstByte(turn, 3)
	log.Finish(turn, "error", "provider said the user wrote a private sentence")
	line := buf.String()
	if !strings.Contains(line, "first_byte_ms=10") {
		t.Fatalf("first byte overwritten: %s", line)
	}
	if !strings.Contains(line, "err=unclassified") {
		t.Fatalf("long error kept: %s", line)
	}
	if strings.Contains(line, "private") || strings.Contains(line, "sentence") {
		t.Fatalf("error text leaked: %s", line)
	}
	if !strings.Contains(line, "bytes=5") {
		t.Fatalf("bytes=%s", line)
	}
}

func TestFinishOnce(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf)
	turn := log.Start("noema.v1", "s", "r", "c")
	log.Finish(turn, "cancelled", "cancelled")
	log.Finish(turn, "done", "")
	if strings.Count(buf.String(), "component=turn") != 1 {
		t.Fatalf("double finish: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "status=cancelled") || !strings.Contains(buf.String(), "err=cancelled") {
		t.Fatalf("line: %s", buf.String())
	}
}
