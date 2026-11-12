package estuary

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// 稳定主体。session_id 是连续对话，不是主体。
const (
	SubjectUser      = "user"
	SubjectCharacter = "character"
	CharacterID      = "hiori"

	NamespaceMemory   = "memory"
	NamespaceEvent    = "event"
	NamespaceActivity = "activity"
	NamespaceIntent   = "intent"

	KindChatTurn = "chat_turn"

	SourceUser      = "user"
	SourceCharacter = "character"
	SourceEngine    = "engine"
	SourceSystem    = "system"
)

// Provenance 与 docs/estuary-event.schema.json 对齐。
// fictional=true 必须原样保存，不得升格成现实事实。
type Provenance struct {
	Source      string `json:"source,omitempty"`
	Fictional   bool   `json:"fictional,omitempty"`
	CharacterID string `json:"character_id,omitempty"`
}

// Record 是账本一行。字段名与 schema 一致，不额外发明生命周期字段。
type Record struct {
	SubjectID  string         `json:"subject_id"`
	Namespace  string         `json:"namespace"`
	EventID    string         `json:"event_id"`
	SessionID  string         `json:"session_id,omitempty"`
	Seq        *int64         `json:"seq,omitempty"`
	ActivityID string         `json:"activity_id,omitempty"`
	IntentID   string         `json:"intent_id,omitempty"`
	Kind       string         `json:"kind,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	Provenance *Provenance    `json:"provenance,omitempty"`
	Payload    map[string]any `json:"payload,omitempty"`
}

// Filter 约束列表。SubjectID 与 Namespace 必填，避免跨主体扫描。
type Filter struct {
	SubjectID string
	Namespace string
	AfterSeq  *int64
	// Fictional 非空时只返回匹配的虚构标记。nil provenance 视为 fictional=false。
	Fictional *bool
}

// Ledger 是 Noema 自己的双主体事件账本。
// 文件在 NOEMA_STATE_DIR/estuary/events.jsonl，不写 Lucky HomeDir。
// 同一 event_id 可以有两条记录（user / character），payload 按 subject_id 隔离。
type Ledger struct {
	mu       sync.Mutex
	dir      string
	path     string
	file     *os.File
	size     int64
	records  []Record
	closed   bool
	writeErr error
	// failAfter 让接下来第 N 次成功写入之后的下一次 Append 失败（N=0 表示下一次就失败）。
	// 只给测试用，用来露出半截 chat_turn，不回滚已写入的行。
	failAfter int
	failArmed bool
}

// Open 打开或创建 stateDir 下的账本，并截掉无换行的崩溃残尾。
func Open(stateDir string) (*Ledger, error) {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return nil, fmt.Errorf("estuary: state dir is required")
	}
	abs, err := filepath.Abs(stateDir)
	if err != nil {
		return nil, fmt.Errorf("estuary: resolve state dir: %w", err)
	}
	dir := filepath.Join(abs, "estuary")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("estuary: create ledger dir: %w", err)
	}
	path := filepath.Join(dir, "events.jsonl")
	led := &Ledger{dir: abs, path: path}
	if err := led.load(); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("estuary: open ledger: %w", err)
	}
	led.file = file
	return led, nil
}

// Dir 返回 Noema 状态目录（含 estuary/ 的父目录）。
func (l *Ledger) Dir() string {
	if l == nil {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.dir
}

// Path 返回 jsonl 路径。
func (l *Ledger) Path() string {
	if l == nil {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.path
}

// ArmFailAfter 让接下来成功写入 n 条之后的下一次 Append 失败。n=0 时下一次就失败。
func (l *Ledger) ArmFailAfter(n int) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if n < 0 {
		n = 0
	}
	l.failAfter = n
	l.failArmed = true
}

// Append 追加一条记录。本切片只追加，没有删除或过期。
func (l *Ledger) Append(rec Record) (Record, error) {
	if l == nil {
		return Record{}, fmt.Errorf("estuary: nil ledger")
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	} else {
		rec.CreatedAt = rec.CreatedAt.UTC()
	}
	if err := validate(rec); err != nil {
		return Record{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return Record{}, fmt.Errorf("estuary: ledger closed")
	}
	if l.writeErr != nil {
		return Record{}, l.writeErr
	}
	if l.file == nil {
		return Record{}, fmt.Errorf("estuary: ledger file is not open")
	}
	if l.failArmed {
		if l.failAfter == 0 {
			l.failArmed = false
			return Record{}, fmt.Errorf("estuary: append record: injected failure")
		}
		l.failAfter--
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return Record{}, fmt.Errorf("estuary: encode record: %w", err)
	}
	raw = append(raw, '\n')
	if _, err := l.file.Write(raw); err != nil {
		l.writeErr = fmt.Errorf("estuary: append record: %w", err)
		if terr := l.file.Truncate(l.size); terr != nil {
			l.writeErr = fmt.Errorf("%w; rollback truncate failed: %v", l.writeErr, terr)
		} else if serr := l.file.Sync(); serr != nil {
			l.writeErr = fmt.Errorf("%w; rollback sync failed: %v", l.writeErr, serr)
		}
		return Record{}, l.writeErr
	}
	if err := l.file.Sync(); err != nil {
		l.writeErr = fmt.Errorf("estuary: sync record: %w", err)
		return Record{}, l.writeErr
	}
	l.size += int64(len(raw))
	l.records = append(l.records, cloneRecord(rec))
	return cloneRecord(rec), nil
}

// List 按 subject_id + namespace 返回该主体自己的记录，不包含其他主体的 payload。
func (l *Ledger) List(f Filter) ([]Record, error) {
	if l == nil {
		return nil, fmt.Errorf("estuary: nil ledger")
	}
	subject := strings.TrimSpace(f.SubjectID)
	ns := strings.TrimSpace(f.Namespace)
	if subject == "" || ns == "" {
		return nil, fmt.Errorf("estuary: subject_id and namespace are required")
	}
	if !validNamespace(ns) {
		return nil, fmt.Errorf("estuary: invalid namespace %q", ns)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Record, 0)
	for _, rec := range l.records {
		if rec.SubjectID != subject || rec.Namespace != ns {
			continue
		}
		if f.AfterSeq != nil {
			if rec.Seq == nil || *rec.Seq <= *f.AfterSeq {
				continue
			}
		}
		if f.Fictional != nil && fictionalOf(rec) != *f.Fictional {
			continue
		}
		out = append(out, cloneRecord(rec))
	}
	return out, nil
}

// Get 返回同一 event_id 下每个主体自己的视角。键是 subject_id，值不混入其他主体 payload。
func (l *Ledger) Get(eventID string) (map[string]Record, error) {
	if l == nil {
		return nil, fmt.Errorf("estuary: nil ledger")
	}
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return nil, fmt.Errorf("estuary: event_id is required")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make(map[string]Record)
	for _, rec := range l.records {
		if rec.EventID != eventID {
			continue
		}
		out[rec.SubjectID] = cloneRecord(rec)
	}
	return out, nil
}

// Close 关闭账本文件。
func (l *Ledger) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	if errors.Is(err, os.ErrClosed) {
		return nil
	}
	return err
}

func (l *Ledger) load() error {
	info, err := os.Stat(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("estuary: stat ledger: %w", err)
	}
	file, err := os.Open(l.path)
	if err != nil {
		return fmt.Errorf("estuary: read ledger: %w", err)
	}
	defer file.Close()

	reader := bufio.NewReaderSize(file, 64*1024)
	var valid int64
	records := make([]Record, 0)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("estuary: scan ledger: %w", err)
		}
		valid += int64(len(line))
		text := strings.TrimSpace(string(line))
		if text == "" {
			continue
		}
		var rec Record
		if err := json.Unmarshal([]byte(text), &rec); err != nil {
			continue
		}
		if validate(rec) != nil {
			continue
		}
		records = append(records, rec)
	}
	if valid < info.Size() {
		if err := os.Truncate(l.path, valid); err != nil {
			return fmt.Errorf("estuary: truncate torn tail: %w", err)
		}
	}
	l.size = valid
	l.records = records
	return nil
}

func validate(rec Record) error {
	if strings.TrimSpace(rec.SubjectID) == "" {
		return fmt.Errorf("estuary: subject_id is required")
	}
	if !validNamespace(rec.Namespace) {
		return fmt.Errorf("estuary: invalid namespace %q", rec.Namespace)
	}
	if strings.TrimSpace(rec.EventID) == "" {
		return fmt.Errorf("estuary: event_id is required")
	}
	if rec.CreatedAt.IsZero() {
		return fmt.Errorf("estuary: created_at is required")
	}
	if rec.Seq != nil && *rec.Seq < 0 {
		return fmt.Errorf("estuary: seq must be >= 0")
	}
	if rec.Provenance != nil && rec.Provenance.Source != "" && !validSource(rec.Provenance.Source) {
		return fmt.Errorf("estuary: invalid provenance.source %q", rec.Provenance.Source)
	}
	return nil
}

func validNamespace(ns string) bool {
	switch ns {
	case NamespaceMemory, NamespaceEvent, NamespaceActivity, NamespaceIntent:
		return true
	default:
		return false
	}
}

func validSource(source string) bool {
	switch source {
	case SourceUser, SourceCharacter, SourceEngine, SourceSystem:
		return true
	default:
		return false
	}
}

func fictionalOf(rec Record) bool {
	return rec.Provenance != nil && rec.Provenance.Fictional
}

func cloneRecord(rec Record) Record {
	out := rec
	if rec.Seq != nil {
		seq := *rec.Seq
		out.Seq = &seq
	}
	if rec.Provenance != nil {
		prov := *rec.Provenance
		out.Provenance = &prov
	}
	if rec.Payload != nil {
		out.Payload = make(map[string]any, len(rec.Payload))
		for k, v := range rec.Payload {
			out.Payload[k] = v
		}
	}
	return out
}
