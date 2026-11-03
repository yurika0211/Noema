package eventlog

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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

func TestDiskStoreSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Append("sess-1", protocol.Envelope{Type: protocol.TypeContent, ClientRequestID: "req-1"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Append("sess-1", protocol.Envelope{Type: protocol.TypeDone, ClientRequestID: "req-1"})
	if err != nil {
		t.Fatal(err)
	}
	runID, replay, err := store.BeginRun("owner", "sess-1", "req-1", "你好")
	if err != nil || replay {
		t.Fatalf("begin: id=%s replay=%v err=%v", runID, replay, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	if got := reopened.LastSeq("sess-1"); got != second.Seq {
		t.Fatalf("last seq after restart=%d want %d", got, second.Seq)
	}
	replayAfter := reopened.After("sess-1", first.Seq)
	if len(replayAfter) != 1 || replayAfter[0].Type != protocol.TypeDone || replayAfter[0].EventID != second.EventID {
		t.Fatalf("replay after restart=%+v", replayAfter)
	}
	if got := reopened.ForRequest("sess-1", "req-1"); len(got) != 2 {
		t.Fatalf("for request after restart=%d", len(got))
	}

	// 单调 seq 继续，不与重启前冲突。
	third, err := reopened.Append("sess-1", protocol.Envelope{Type: protocol.TypeContent})
	if err != nil {
		t.Fatal(err)
	}
	if third.Seq != second.Seq+1 {
		t.Fatalf("seq after restart=%d want %d", third.Seq, second.Seq+1)
	}

	// 幂等记录也落盘：相同输入复用，不同输入冲突。
	againID, replay, err := reopened.BeginRun("owner", "sess-1", "req-1", "你好")
	if err != nil || !replay || againID != runID {
		t.Fatalf("idempotent after restart: id=%s replay=%v err=%v", againID, replay, err)
	}
	if _, _, err := reopened.BeginRun("owner", "sess-1", "req-1", "另一句"); !errors.Is(err, engine.ErrInvalidRequest) {
		t.Fatalf("conflict after restart: %v", err)
	}

	// owner 不同则不是同一个幂等键。
	if _, _, err := reopened.BeginRun("other", "sess-1", "req-1", "你好"); err != nil {
		t.Fatalf("different owner should start a new run: %v", err)
	}
}

func TestDiskStoreSeparatesSessionsAndSanitizesName(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if _, err := store.Append("a/b", protocol.Envelope{Type: protocol.TypeContent}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append("other", protocol.Envelope{Type: protocol.TypeContent}); err != nil {
		t.Fatal(err)
	}
	if got := store.LastSeq("a/b"); got != 1 {
		t.Fatalf("a/b seq=%d", got)
	}
	if got := store.LastSeq("other"); got != 1 {
		t.Fatalf("other seq=%d", got)
	}
	if got := store.LastSeq("missing"); got != 0 {
		t.Fatalf("missing seq=%d", got)
	}

	entries, err := os.ReadDir(filepath.Join(dir, "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("files=%d want 2", len(entries))
	}
	for _, e := range entries {
		if filepath.Base(e.Name()) != e.Name() {
			t.Fatalf("session file escaped dir: %s", e.Name())
		}
	}
}

func sessionFile(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("sessions files=%d want 1", len(entries))
	}
	return filepath.Join(dir, "sessions", entries[0].Name())
}

func appendRaw(t *testing.T, path, raw string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(raw); err != nil {
		t.Fatal(err)
	}
	if err := file.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDiskStoreSkipsTornLine(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append("sess", protocol.Envelope{Type: protocol.TypeContent}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "sessions"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	path := filepath.Join(dir, "sessions", entries[0].Name())
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"kind":"event","event":{"type":"done"`); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()

	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got := reopened.LastSeq("sess"); got != 1 {
		t.Fatalf("last seq=%d want 1 (torn line ignored)", got)
	}
	if evs := reopened.After("sess", 0); len(evs) != 1 || evs[0].Type != protocol.TypeContent {
		t.Fatalf("events=%+v", evs)
	}
	if _, err := reopened.Append("sess", protocol.Envelope{Type: protocol.TypeDone}); err != nil {
		t.Fatalf("append after torn line: %v", err)
	}
}

// 末尾是无换行残字节时，Open 必须截掉它：否则下一次 Append 会粘在残行上，
// 下次启动整行解析失败，新事件被丢掉。
func TestDiskStoreAppendAfterTornTailWithoutNewline(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Append("sess", protocol.Envelope{Type: protocol.TypeContent, ClientRequestID: "req-1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	path := sessionFile(t, dir)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// 模拟写入中途崩溃：没有换行的半条记录。
	torn := `{"kind":"event","event":{"type":"done","seq":2`
	appendRaw(t, path, torn)

	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.LastSeq("sess"); got != first.Seq {
		t.Fatalf("last seq after torn tail=%d want %d", got, first.Seq)
	}
	second, err := reopened.Append("sess", protocol.Envelope{Type: protocol.TypeDone})
	if err != nil {
		t.Fatalf("append after torn tail: %v", err)
	}
	if second.Seq != first.Seq+1 {
		t.Fatalf("seq=%d want %d", second.Seq, first.Seq+1)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}

	// 残尾必须已经被截掉，文件只长大到一条完整新记录。
	grown, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if grown.Size() <= before.Size() {
		t.Fatalf("file did not grow: before=%d after=%d", before.Size(), grown.Size())
	}

	// 再开一次：新事件在，残行没有污染。
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	evs := again.After("sess", 0)
	if len(evs) != 2 {
		t.Fatalf("events=%+v want 2", evs)
	}
	if evs[0].Seq != first.Seq || evs[0].Type != protocol.TypeContent {
		t.Fatalf("first event=%+v", evs[0])
	}
	if evs[1].Seq != second.Seq || evs[1].Type != protocol.TypeDone {
		t.Fatalf("second event=%+v", evs[1])
	}
	if got := again.LastSeq("sess"); got != second.Seq {
		t.Fatalf("last seq=%d want %d", got, second.Seq)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(raw, []byte("\n")) {
		t.Fatalf("log does not end with newline: %q", raw)
	}
	for i, line := range bytes.Split(bytes.TrimSuffix(raw, []byte("\n")), []byte("\n")) {
		var rec record
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatalf("line %d is not a complete record: %q: %v", i, line, err)
		}
	}
}

// 只读路径（LastSeq/After）也会发现残尾并截掉，不需要等 Append。
func TestDiskStoreTruncatesTornTailOnRead(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append("sess", protocol.Envelope{Type: protocol.TypeContent}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	path := sessionFile(t, dir)
	complete, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	appendRaw(t, path, `{"kind":"event","event":{"type":"done"`)

	reader, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if got := reader.LastSeq("sess"); got != 1 {
		t.Fatalf("last seq=%d want 1", got)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.Size() != complete.Size() {
		t.Fatalf("torn tail not truncated: size=%d want %d", after.Size(), complete.Size())
	}
}

// 写失败不能推进 seq，也不能留下半行让下次 Append 粘上去。
func TestDiskStoreWriteFailureKeepsSeqAndFile(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Append("sess", protocol.Envelope{Type: protocol.TypeContent})
	if err != nil {
		t.Fatal(err)
	}

	// 关掉底层文件，制造写失败。
	store.mu.Lock()
	log := store.sessions["sess"]
	store.mu.Unlock()
	if log == nil || log.file == nil {
		t.Fatal("session file not open")
	}
	sizeBefore := log.size
	// 保留 file 指针但关掉底层描述符：写入会失败，不会悄悄重开。
	if err := log.file.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Append("sess", protocol.Envelope{Type: protocol.TypeDone}); err == nil {
		t.Fatal("append on a closed session file must fail instead of degrading to memory")
	}
	if got := store.LastSeq("sess"); got != first.Seq {
		t.Fatalf("last seq=%d want %d", got, first.Seq)
	}
	if log.size != sizeBefore {
		t.Fatalf("size=%d want %d", log.size, sizeBefore)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if evs := reopened.After("sess", 0); len(evs) != 1 || evs[0].Type != protocol.TypeContent {
		t.Fatalf("events=%+v", evs)
	}
}

func TestOpenRequiresDir(t *testing.T) {
	if _, err := Open("  "); err == nil {
		t.Fatal("expected error for empty state dir")
	}
}
