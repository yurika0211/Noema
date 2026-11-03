package eventlog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/yurika0211/Noema/internal/engine"
	"github.com/yurika0211/Noema/internal/protocol"
)

// Store 保存会话内可重放事件。样品用进程内存；重启后游标从 0 开始。
type Store struct {
	mu       sync.Mutex
	sessions map[string]*sessionLog
}

type sessionLog struct {
	seq    int64
	events []protocol.Envelope
	runs   map[string]*runRecord
}

type runRecord struct {
	InputHash string
	RunID     string
	Status    string
}

func New() *Store {
	return &Store{sessions: make(map[string]*sessionLog)}
}

func (s *Store) Append(sessionID string, env protocol.Envelope) (protocol.Envelope, error) {
	if sessionID == "" {
		return protocol.Envelope{}, fmt.Errorf("empty session")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	log := s.ensure(sessionID)
	log.seq++
	if env.EventID == "" {
		env.EventID = fmt.Sprintf("evt_%d", log.seq)
	}
	env.Seq = log.seq
	env.SessionID = sessionID
	if env.Protocol == "" {
		env.Protocol = protocol.Version
	}
	if env.CreatedAt.IsZero() {
		env.CreatedAt = time.Now().UTC()
	}
	log.events = append(log.events, env)
	return env, nil
}

func (s *Store) After(sessionID string, lastSeq int64) []protocol.Envelope {
	s.mu.Lock()
	defer s.mu.Unlock()
	log := s.sessions[sessionID]
	if log == nil {
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
	log := s.sessions[sessionID]
	if log == nil || clientRequestID == "" {
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
	log := s.sessions[sessionID]
	if log == nil {
		return 0
	}
	return log.seq
}

// BeginRun 按 owner+client_request_id 做回合幂等。相同输入复用，不同输入冲突。
func (s *Store) BeginRun(ownerID, sessionID, clientRequestID, message string) (runID string, replay bool, err error) {
	if clientRequestID == "" {
		return fmt.Sprintf("run_%s_%d", sessionID, time.Now().UnixNano()), false, nil
	}
	key := ownerID + "/" + clientRequestID
	sum := hashInput(message)
	s.mu.Lock()
	defer s.mu.Unlock()
	log := s.ensure(sessionID)
	if rec, ok := log.runs[key]; ok {
		if rec.InputHash != sum {
			return "", false, fmt.Errorf("%w: client_request_id reused with different input", engine.ErrInvalidRequest)
		}
		return rec.RunID, true, nil
	}
	runID = fmt.Sprintf("run_%s_%d", sessionID, log.seq+1)
	log.runs[key] = &runRecord{InputHash: sum, RunID: runID, Status: "running"}
	return runID, false, nil
}

func (s *Store) ensure(sessionID string) *sessionLog {
	log := s.sessions[sessionID]
	if log == nil {
		log = &sessionLog{runs: make(map[string]*runRecord)}
		s.sessions[sessionID] = log
	}
	return log
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
