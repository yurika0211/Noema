package transport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"bytes"

	"github.com/yurika0211/Noema/internal/engine"
	"github.com/yurika0211/Noema/internal/eventlog"
	"github.com/yurika0211/Noema/internal/observe"
	"github.com/yurika0211/Noema/internal/protocol"
)

type stubEngine struct {
	caps      engine.Capabilities
	sessionID string
	events    []engine.RunEvent
	cancelled chan string
	starts    atomic.Int32
}

func (s *stubEngine) NewSession(context.Context) (string, error) { return s.sessionID, nil }
func (s *stubEngine) Capabilities() engine.Capabilities          { return s.caps }
func (s *stubEngine) Close() error                               { return nil }
func (s *stubEngine) ResolveApproval(context.Context, engine.ApprovalDecision) error {
	return engine.Unsupported("approval_resolution")
}
func (s *stubEngine) CancelTurn(_ context.Context, runID string) error {
	if s.cancelled != nil {
		s.cancelled <- runID
	}
	return nil
}
func (s *stubEngine) StartTurn(ctx context.Context, req engine.RunRequest) (<-chan engine.RunEvent, error) {
	if req.Message == "" {
		return nil, engine.ErrInvalidRequest
	}
	s.starts.Add(1)
	ch := make(chan engine.RunEvent, len(s.events))
	go func() {
		defer close(ch)
		for _, ev := range s.events {
			select {
			case <-ctx.Done():
				ch <- engine.RunEvent{Type: engine.EventCancelled, Err: ctx.Err()}
				return
			case ch <- ev:
			}
		}
	}()
	return ch, nil
}

func TestAuthRejectsMissingKey(t *testing.T) {
	srv := &Server{APIKey: "secret", Engine: &stubEngine{sessionID: "s1"}, Log: eventlog.New()}
	req := httptest.NewRequest(http.MethodGet, "/v1/capabilities", nil)
	rec := httptest.NewRecorder()
	srv.withAuth(http.HandlerFunc(srv.handleCapabilities)).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestAuthRejectsWrongKey(t *testing.T) {
	srv := &Server{APIKey: "secret", Engine: &stubEngine{sessionID: "s1"}, Log: eventlog.New()}
	handler := srv.withAuth(http.HandlerFunc(srv.handleCapabilities))

	req := httptest.NewRequest(http.MethodGet, "/v1/capabilities", nil)
	req.Header.Set("Authorization", "Bearer nope")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong key status=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/capabilities", nil)
	req.Header.Set("X-API-Key", "secret")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("right key status=%d", rec.Code)
	}
}

// 没有配置 key 时 /v1/* 必须拒绝，不能依赖 Origin 或监听地址。
func TestUnconfiguredKeyRejectsV1(t *testing.T) {
	srv := &Server{Engine: &stubEngine{sessionID: "s1"}, Log: eventlog.New()}
	handler := srv.withAuth(http.HandlerFunc(srv.handleCapabilities))

	for _, path := range []string{"/v1/capabilities", "/v1/ws"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer anything")
		req.Header.Set("X-API-Key", "anything")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s status=%d want 401", path, rec.Code)
		}
	}

	// healthz 保留给探活。
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.withAuth(http.HandlerFunc(srv.handleHealth)).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz status=%d", rec.Code)
	}
}

// 走完整 mux：默认 Server 没设 key，/v1/ws 也不能升级。
func TestUnconfiguredKeyRejectsWebSocketUpgrade(t *testing.T) {
	srv := &Server{Addr: "127.0.0.1:0", Engine: &stubEngine{sessionID: "s1"}, Log: eventlog.New()}
	go func() { _ = srv.ListenAndServe() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.WaitReady(ctx); err != nil {
		t.Fatalf("wait ready: %v", err)
	}
	defer srv.Close()

	wsURL := "ws://" + srv.ListenAddr() + "/v1/ws?session=s1"
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("expected dial to fail without key")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("resp=%v err=%v", resp, err)
	}
	if resp != nil {
		_ = resp.Body.Close()
	}
}

func TestChatReplayAndCancel(t *testing.T) {
	eng := &stubEngine{
		sessionID: "sess-fixed",
		caps:      engine.Capabilities{TextStream: true, CancelTurn: true, IsolatedHomeDir: true},
		events: []engine.RunEvent{
			{Type: engine.EventContent, Content: "灯"},
			{Type: engine.EventContent, Content: "織"},
			{Type: engine.EventDone},
		},
		cancelled: make(chan string, 1),
	}
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		(&Server{Engine: eng, Log: eventlog.New(), CharacterID: "hiori"}).handleWS(w, r)
	}))
	defer httpSrv.Close()

	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/v1/ws?session=sess-fixed"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	hello := readEnv(t, conn)
	if hello.Type != protocol.TypeHello {
		t.Fatalf("hello type=%s", hello.Type)
	}

	chatPayload, _ := json.Marshal(protocol.ChatPayload{Message: "你好"})
	if err := conn.WriteJSON(protocol.Envelope{
		Protocol:        protocol.Version,
		Type:            protocol.TypeChat,
		ClientRequestID: "req-1",
		Payload:         chatPayload,
	}); err != nil {
		t.Fatal(err)
	}

	var texts []string
	for {
		ev := readEnv(t, conn)
		switch ev.Type {
		case protocol.TypeContent:
			var body protocol.ContentPayload
			_ = json.Unmarshal(ev.Payload, &body)
			texts = append(texts, body.Text)
			if ev.Seq == 0 {
				t.Fatal("content missing seq")
			}
		case protocol.TypeDone:
			if strings.Join(texts, "") != "灯織" {
				t.Fatalf("texts=%v", texts)
			}
			last := ev.Seq
			if err := conn.WriteJSON(protocol.Envelope{
				Type:    protocol.TypeReplay,
				Payload: eventlog.MustPayload(protocol.ReplayPayload{LastSeq: 1}),
			}); err != nil {
				t.Fatal(err)
			}
			replayed := 0
			for {
				rep := readEnv(t, conn)
				if rep.Type == protocol.TypeReplayComplete {
					break
				}
				replayed++
				if rep.Seq <= 1 {
					t.Fatalf("replayed seq=%d", rep.Seq)
				}
			}
			if replayed == 0 {
				t.Fatal("expected persisted replay")
			}
			_ = last
			if err := conn.WriteJSON(protocol.Envelope{Type: protocol.TypeCancel, ClientRequestID: "req-1"}); err != nil {
				t.Fatal(err)
			}
			cancelled := readEnv(t, conn)
			if cancelled.Type != protocol.TypeCancelled {
				t.Fatalf("cancel type=%s", cancelled.Type)
			}
			select {
			case <-eng.cancelled:
			case <-time.After(time.Second):
				t.Fatal("engine cancel not called")
			}
			return
		default:
			t.Fatalf("unexpected %s", ev.Type)
		}
	}
}

func TestTurnLogRecordsTimingWithoutBody(t *testing.T) {
	var buf bytes.Buffer
	eng := &stubEngine{
		sessionID: "sess-obs",
		events: []engine.RunEvent{
			{Type: engine.EventContent, Content: "私密句子不该出现在日志"},
			{Type: engine.EventDone},
		},
	}
	obs := observe.New(&buf)
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		(&Server{Engine: eng, Log: eventlog.New(), CharacterID: "hiori", Observe: obs}).handleWS(w, r)
	}))
	defer httpSrv.Close()

	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/v1/ws?session=sess-obs"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = readEnv(t, conn)
	payload, _ := json.Marshal(protocol.ChatPayload{Message: "用户原文也不该出现"})
	if err := conn.WriteJSON(protocol.Envelope{
		Protocol: protocol.Version, Type: protocol.TypeChat, ClientRequestID: "req-obs", Payload: payload,
	}); err != nil {
		t.Fatal(err)
	}
	for {
		ev := readEnv(t, conn)
		if ev.Type == protocol.TypeDone {
			break
		}
	}
	time.Sleep(30 * time.Millisecond)
	line := buf.String()
	for _, want := range []string{"component=turn", "protocol=noema.v1", "session_id=sess-obs", "status=done", "client_request_id=req-obs", "first_byte_ms="} {
		if !strings.Contains(line, want) {
			t.Fatalf("missing %s in %s", want, line)
		}
	}
	if strings.Contains(line, "私密") || strings.Contains(line, "用户原文") {
		t.Fatalf("body leaked: %s", line)
	}
}

func readEnv(t *testing.T, conn *websocket.Conn) protocol.Envelope {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var env protocol.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
	return env
}
