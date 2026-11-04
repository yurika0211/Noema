package observe

import (
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/yurika0211/Noema/internal/protocol"
)

// Logger 记回合元数据。字段只有协议版本、ID、状态、耗时和字节数。
// 不接受正文：调用方不要把 message / content 传进来。
type Logger struct {
	mu  sync.Mutex
	out *log.Logger
	now func() time.Time
}

func New(w io.Writer) *Logger {
	if w == nil {
		w = os.Stderr
	}
	return &Logger{
		out: log.New(w, "", log.LstdFlags|log.LUTC),
		now: time.Now,
	}
}

type Turn struct {
	Protocol        string
	SessionID       string
	RunID           string
	ClientRequestID string
	started         time.Time
	first           time.Time
	bytes           int
	status          string
	errClass        string
	done            bool
}

func (l *Logger) Start(protocolName, sessionID, runID, clientRequestID string) *Turn {
	if l == nil {
		return nil
	}
	if strings.TrimSpace(protocolName) == "" {
		protocolName = protocol.Version
	}
	return &Turn{
		Protocol:        protocolName,
		SessionID:       sessionID,
		RunID:           runID,
		ClientRequestID: clientRequestID,
		started:         l.now(),
	}
}

// FirstByte 记录首段可见输出。重复调用只保留第一次。
func (l *Logger) FirstByte(t *Turn, n int) {
	if l == nil {
		return
	}
	if t == nil || t.done || !t.first.IsZero() {
		if t != nil && n > 0 {
			t.bytes += n
		}
		return
	}
	t.first = l.now()
	if n > 0 {
		t.bytes += n
	}
}

func (l *Logger) AddBytes(t *Turn, n int) {
	if l == nil {
		return
	}
	if t == nil || n <= 0 || t.done {
		return
	}
	t.bytes += n
}

// Finish 打一行。status 用 done / cancelled / error / replay。
// errClass 是短分类，不是模型原文，也不是用户消息。
func (l *Logger) Finish(t *Turn, status, errClass string) {
	if l == nil || t == nil || t.done {
		return
	}
	t.done = true
	t.status = status
	t.errClass = classify(errClass)
	end := l.now()
	total := end.Sub(t.started).Milliseconds()
	first := int64(-1)
	if !t.first.IsZero() {
		first = t.first.Sub(t.started).Milliseconds()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.out.Printf("component=turn protocol=%s session_id=%s run_id=%s client_request_id=%s status=%s err=%s duration_ms=%d first_byte_ms=%d bytes=%d",
		t.Protocol, t.SessionID, t.RunID, t.ClientRequestID, t.status, t.errClass, total, first, t.bytes)
}

func classify(errClass string) string {
	s := strings.TrimSpace(errClass)
	if s == "" {
		return "-"
	}
	// 只留短分类。带空格的句子多半是正文或供应商错误原文。
	if strings.ContainsAny(s, " \t\n") || len(s) > 48 {
		return "unclassified"
	}
	return s
}
