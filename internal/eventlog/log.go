package eventlog

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/yurika0211/Noema/internal/engine"
	"github.com/yurika0211/Noema/internal/protocol"
)

// Store 保存会话内可重放事件。
//
// 两种模式：
//   - New()：进程内存，重启后游标从 0 开始，只用于测试。
//   - Open(dir)：追加写 Noema 自己的状态目录，重启后按单调 seq 继续。
//
// 状态目录属于 Noema，不是 Lucky Agent 的 HomeDir；不要用 Lucky 会话 JSON 冒充产品事件日志。
type Store struct {
	mu       sync.Mutex
	dir      string
	sessions map[string]*sessionLog
	closed   bool
}

type sessionLog struct {
	seq     int64
	events  []protocol.Envelope
	runs    map[string]*runRecord
	file    *os.File
	loaded  bool
	loadErr error
}

type runRecord struct {
	Key       string `json:"key"`
	InputHash string `json:"input_hash"`
	RunID     string `json:"run_id"`
	Status    string `json:"status"`
}

// record 是磁盘上的一行。kind 区分事件与回合幂等记录。
type record struct {
	Kind  string             `json:"kind"`
	Event *protocol.Envelope `json:"event,omitempty"`
	Run   *runRecord         `json:"run,omitempty"`
}

const (
	recordEvent = "event"
	recordRun   = "run"
)

// New 返回内存 Store。
func New() *Store {
	return &Store{sessions: make(map[string]*sessionLog)}
}

// Open 返回落盘 Store，事件写到 dir/sessions/*.jsonl。
func Open(dir string) (*Store, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, fmt.Errorf("eventlog: state dir is required")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("eventlog: resolve state dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(abs, "sessions"), 0o700); err != nil {
		return nil, fmt.Errorf("eventlog: create state dir: %w", err)
	}
	return &Store{dir: abs, sessions: make(map[string]*sessionLog)}, nil
}

// Dir 返回状态目录；内存 Store 返回空串。
func (s *Store) Dir() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dir
}

func (s *Store) Append(sessionID string, env protocol.Envelope) (protocol.Envelope, error) {
	if sessionID == "" {
		return protocol.Envelope{}, fmt.Errorf("empty session")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	log, err := s.writableLocked(sessionID)
	if err != nil {
		return protocol.Envelope{}, err
	}
	env.Seq = log.seq + 1
	if env.EventID == "" {
		env.EventID = fmt.Sprintf("evt_%d", env.Seq)
	}
	env.SessionID = sessionID
	if env.Protocol == "" {
		env.Protocol = protocol.Version
	}
	if env.CreatedAt.IsZero() {
		env.CreatedAt = time.Now().UTC()
	}
	if err := writeRecord(log, record{Kind: recordEvent, Event: &env}); err != nil {
		return protocol.Envelope{}, err
	}
	log.seq = env.Seq
	log.events = append(log.events, env)
	return env, nil
}

func (s *Store) After(sessionID string, lastSeq int64) []protocol.Envelope {
	s.mu.Lock()
	defer s.mu.Unlock()
	log, err := s.stateLocked(sessionID)
	if err != nil || log == nil {
		return nil
	}
	out := make([]protocol.Envelope, 0)
	for _, ev := range log.events {
		if ev.Seq > lastSeq {
			out = append(out, ev)
		}
	}
	return out
}

func (s *Store) ForRequest(sessionID, clientRequestID string) []protocol.Envelope {
	s.mu.Lock()
	defer s.mu.Unlock()
	if clientRequestID == "" {
		return nil
	}
	log, err := s.stateLocked(sessionID)
	if err != nil || log == nil {
		return nil
	}
	out := make([]protocol.Envelope, 0)
	for _, ev := range log.events {
		if ev.ClientRequestID == clientRequestID {
			out = append(out, ev)
		}
	}
	return out
}

func (s *Store) LastSeq(sessionID string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	log, err := s.stateLocked(sessionID)
	if err != nil || log == nil {
		return 0
	}
	return log.seq
}

// BeginRun 按 owner+client_request_id 做回合幂等。相同输入复用，不同输入冲突。
// 记录落盘，重启后同一 client_request_id 仍复用而不是重开回合。
func (s *Store) BeginRun(ownerID, sessionID, clientRequestID, message string) (runID string, replay bool, err error) {
	if clientRequestID == "" {
		return fmt.Sprintf("run_%s_%d", sessionID, time.Now().UnixNano()), false, nil
	}
	key := ownerID + "/" + clientRequestID
	sum := hashInput(message)
	s.mu.Lock()
	defer s.mu.Unlock()
	log, err := s.writableLocked(sessionID)
	if err != nil {
		return "", false, err
	}
	if rec, ok := log.runs[key]; ok {
		if rec.InputHash != sum {
			return "", false, fmt.Errorf("%w: client_request_id reused with different input", engine.ErrInvalidRequest)
		}
		return rec.RunID, true, nil
	}
	runID = fmt.Sprintf("run_%s_%d", sessionID, log.seq+1)
	rec := &runRecord{Key: key, InputHash: sum, RunID: runID, Status: "running"}
	if err := writeRecord(log, record{Kind: recordRun, Run: rec}); err != nil {
		return "", false, err
	}
	log.runs[key] = rec
	return runID, false, nil
}

// Close 关闭所有已打开的会话文件。内存 Store 无操作。
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	var firstErr error
	for _, log := range s.sessions {
		if log.file != nil {
			if err := log.file.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
			log.file = nil
		}
	}
	return firstErr
}

// stateLocked 返回已装载的会话日志；未知会话返回 (nil, nil)，不会创建文件。
// 调用方必须持有 s.mu。
func (s *Store) stateLocked(sessionID string) (*sessionLog, error) {
	if log, ok := s.sessions[sessionID]; ok {
		if log.loaded {
			return log, log.loadErr
		}
		if err := s.loadLocked(sessionID, log); err != nil {
			return log, err
		}
		return log, nil
	}
	log := &sessionLog{runs: make(map[string]*runRecord)}
	if err := s.loadLocked(sessionID, log); err != nil {
		return nil, err
	}
	if !log.loaded {
		return nil, nil
	}
	s.sessions[sessionID] = log
	return log, nil
}

// writableLocked 返回可追加的会话日志，必要时创建状态文件。调用方必须持有 s.mu。
func (s *Store) writableLocked(sessionID string) (*sessionLog, error) {
	log, err := s.stateLocked(sessionID)
	if err != nil {
		return nil, err
	}
	if log == nil {
		log = &sessionLog{loaded: true, runs: make(map[string]*runRecord)}
		s.sessions[sessionID] = log
	}
	if s.dir == "" {
		return log, nil
	}
	if s.closed {
		return nil, fmt.Errorf("eventlog: store closed")
	}
	if log.file != nil {
		return log, nil
	}
	file, err := os.OpenFile(s.sessionPath(sessionID), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("eventlog: open session log: %w", err)
	}
	log.file = file
	return log, nil
}

// loadLocked 从磁盘装载会话。文件不存在时保持未装载（只读访问不会建文件）。
func (s *Store) loadLocked(sessionID string, log *sessionLog) error {
	if s.dir == "" {
		log.loaded = true
		return nil
	}
	if s.closed {
		return fmt.Errorf("eventlog: store closed")
	}
	path := s.sessionPath(sessionID)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		log.loaded = true
		log.loadErr = fmt.Errorf("eventlog: stat session log: %w", err)
		return log.loadErr
	}
	if err := loadFile(log, path); err != nil {
		log.loaded = true
		log.loadErr = err
		return err
	}
	log.loaded = true
	s.sessions[sessionID] = log
	return nil
}

func (s *Store) sessionPath(sessionID string) string {
	return filepath.Join(s.dir, "sessions", fileName(sessionID))
}

func writeRecord(log *sessionLog, rec record) error {
	if log.file == nil {
		return nil
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("eventlog: encode record: %w", err)
	}
	raw = append(raw, '\n')
	if _, err := log.file.Write(raw); err != nil {
		return fmt.Errorf("eventlog: append record: %w", err)
	}
	if err := log.file.Sync(); err != nil {
		return fmt.Errorf("eventlog: sync record: %w", err)
	}
	return nil
}

func loadFile(log *sessionLog, path string) error {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("eventlog: read session log: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 8_000_000)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var rec record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			// 崩溃可能留下半行：跳过而不是让整个会话不可读。
			continue
		}
		switch rec.Kind {
		case recordEvent:
			if rec.Event == nil {
				continue
			}
			log.events = append(log.events, *rec.Event)
			if rec.Event.Seq > log.seq {
				log.seq = rec.Event.Seq
			}
		case recordRun:
			if rec.Run == nil {
				continue
			}
			log.runs[rec.Run.Key] = rec.Run
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("eventlog: scan session log: %w", err)
	}
	return nil
}

// fileName 把 session id 映射成安全文件名。被改写时附加哈希，避免不同 id 撞同名文件。
func fileName(sessionID string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '-', r == '_', r == '.':
			return r
		default:
			return '_'
		}
	}, sessionID)
	if safe == "" || safe == "." || safe == ".." || safe != sessionID {
		sum := sha256.Sum256([]byte(sessionID))
		prefix := strings.Trim(safe, ".")
		if prefix == "" {
			prefix = "s"
		}
		safe = prefix + "-" + hex.EncodeToString(sum[:8])
	}
	return safe + ".jsonl"
}

func hashInput(message string) string {
	sum := sha256.Sum256([]byte(message))
	return hex.EncodeToString(sum[:])
}

func MustPayload(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}
