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
	"github.com/yurika0211/Noema/internal/estuary"
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

func TestBrowserTokenAndWebPage(t *testing.T) {
	srv := &Server{APIKey: "secret", Engine: &stubEngine{sessionID: "s1"}, Log: eventlog.New()}
	handler := srv.withAuth(srv.routes())

	page := httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/", nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "灯織") {
		t.Fatalf("page status=%d", page.Code)
	}
	if strings.Contains(page.Body.String(), "event_id") {
		t.Fatal("companion page lists event ids")
	}
	trace := httptest.NewRecorder()
	handler.ServeHTTP(trace, httptest.NewRequest(http.MethodGet, "/trace", nil))
	if trace.Code != http.StatusOK || !strings.Contains(trace.Body.String(), "诊断") {
		t.Fatalf("trace status=%d", trace.Code)
	}

	ok := httptest.NewRecorder()
	handler.ServeHTTP(ok, httptest.NewRequest(http.MethodGet, "/v1/capabilities?access_token=secret", nil))
	if ok.Code != http.StatusOK {
		t.Fatalf("access_token status=%d", ok.Code)
	}
	bad := httptest.NewRecorder()
	handler.ServeHTTP(bad, httptest.NewRequest(http.MethodGet, "/v1/capabilities?access_token=nope", nil))
	if bad.Code != http.StatusUnauthorized {
		t.Fatalf("bad token status=%d", bad.Code)
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
	srv := &Server{Engine: eng, Log: eventlog.New(), CharacterID: "hiori", Observe: obs}
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.handleWS(w, r)
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
	if !srv.WaitTurns(2 * time.Second) {
		t.Fatal("forward did not finish")
	}
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

func TestDoneAppendsDualSubjectLedgerWithoutReplayOrPing(t *testing.T) {
	dir := t.TempDir()
	led, err := estuary.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer led.Close()
	eng := &stubEngine{
		sessionID: "sess-led",
		events: []engine.RunEvent{
			{Type: engine.EventContent, Content: "灯"},
			{Type: engine.EventContent, Content: "織"},
			{Type: engine.EventDone, Content: ""},
		},
	}
	srv := &Server{Engine: eng, Log: eventlog.New(), Ledger: led, CharacterID: "hiori"}
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.handleWS(w, r)
	}))
	defer httpSrv.Close()

	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/v1/ws?session=sess-led"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = readEnv(t, conn)
	if err := conn.WriteJSON(protocol.Envelope{Type: protocol.TypePing}); err != nil {
		t.Fatal(err)
	}
	if pong := readEnv(t, conn); pong.Type != protocol.TypePong {
		t.Fatalf("pong type=%s", pong.Type)
	}
	payload, _ := json.Marshal(protocol.ChatPayload{Message: "你好"})
	if err := conn.WriteJSON(protocol.Envelope{
		Protocol: protocol.Version, Type: protocol.TypeChat, ClientRequestID: "req-led", Payload: payload,
	}); err != nil {
		t.Fatal(err)
	}
	var done protocol.Envelope
	for {
		ev := readEnv(t, conn)
		if ev.Type == protocol.TypeDone {
			done = ev
			break
		}
	}
	if !srv.WaitTurns(2 * time.Second) {
		t.Fatal("forward did not finish")
	}
	if err := conn.WriteJSON(protocol.Envelope{
		Type: protocol.TypeReplay, Payload: eventlog.MustPayload(protocol.ReplayPayload{LastSeq: 0}),
	}); err != nil {
		t.Fatal(err)
	}
	for {
		ev := readEnv(t, conn)
		if ev.Type == protocol.TypeReplayComplete {
			break
		}
	}

	views, err := led.Get(done.EventID)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 2 {
		t.Fatalf("views=%d %+v", len(views), views)
	}
	user := views[estuary.SubjectUser]
	character := views[estuary.SubjectCharacter]
	if user.Namespace != estuary.NamespaceEvent || user.Kind != estuary.KindChatTurn {
		t.Fatalf("user record=%+v", user)
	}
	if user.SessionID != "sess-led" || user.SubjectID == "sess-led" {
		t.Fatalf("subject collapsed into session: %+v", user)
	}
	if user.Seq == nil || *user.Seq != done.Seq || character.EventID != done.EventID {
		t.Fatalf("seq/event mismatch user=%+v done=%+v", user, done)
	}
	if text, _ := user.Payload["text"].(string); text != "你好" {
		t.Fatalf("user text=%v", user.Payload)
	}
	if text, _ := character.Payload["text"].(string); text != "灯織" {
		t.Fatalf("character text=%v", character.Payload)
	}
	if strings.Contains(fmtPayload(user), "灯織") || strings.Contains(fmtPayload(character), "你好") {
		t.Fatalf("payloads mixed user=%v character=%v", user.Payload, character.Payload)
	}
	all, err := led.List(estuary.Filter{SubjectID: estuary.SubjectUser, Namespace: estuary.NamespaceEvent})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("ping/replay wrote extra user records: %+v", all)
	}
}

func TestDoneWithFullTextDoesNotDoubleLedgerAssistant(t *testing.T) {
	led, err := estuary.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer led.Close()
	eng := &stubEngine{
		sessionID: "sess-full",
		events: []engine.RunEvent{
			{Type: engine.EventContent, Content: "灯"},
			{Type: engine.EventContent, Content: "織"},
			{Type: engine.EventDone, Content: "灯織"},
		},
	}
	srv := &Server{Engine: eng, Log: eventlog.New(), Ledger: led, CharacterID: "hiori"}
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.handleWS(w, r)
	}))
	defer httpSrv.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpSrv.URL, "http")+"/v1/ws?session=sess-full", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = readEnv(t, conn)
	payload, _ := json.Marshal(protocol.ChatPayload{Message: "你好"})
	if err := conn.WriteJSON(protocol.Envelope{
		Protocol: protocol.Version, Type: protocol.TypeChat, ClientRequestID: "req-full", Payload: payload,
	}); err != nil {
		t.Fatal(err)
	}
	var done protocol.Envelope
	for {
		ev := readEnv(t, conn)
		if ev.Type == protocol.TypeDone {
			done = ev
			break
		}
	}
	if !srv.WaitTurns(2 * time.Second) {
		t.Fatal("forward did not finish")
	}
	views, err := led.Get(done.EventID)
	if err != nil {
		t.Fatal(err)
	}
	text, _ := views[estuary.SubjectCharacter].Payload["text"].(string)
	if text != "灯織" {
		t.Fatalf("assistant text=%q", text)
	}
}

func TestLedgerAppendFailureIsVisible(t *testing.T) {
	var buf bytes.Buffer
	led, err := estuary.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer led.Close()
	// 用户行写入后角色行失败：不回滚，但错误必须出现在观测和后续 error 帧。
	led.ArmFailAfter(1)
	eng := &stubEngine{
		sessionID: "sess-fail",
		events: []engine.RunEvent{
			{Type: engine.EventContent, Content: "灯織"},
			{Type: engine.EventDone},
		},
	}
	srv := &Server{Engine: eng, Log: eventlog.New(), Ledger: led, CharacterID: "hiori", Observe: observe.New(&buf)}
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.handleWS(w, r)
	}))
	defer httpSrv.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpSrv.URL, "http")+"/v1/ws?session=sess-fail", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = readEnv(t, conn)
	payload, _ := json.Marshal(protocol.ChatPayload{Message: "你好"})
	if err := conn.WriteJSON(protocol.Envelope{
		Protocol: protocol.Version, Type: protocol.TypeChat, ClientRequestID: "req-fail", Payload: payload,
	}); err != nil {
		t.Fatal(err)
	}
	var done protocol.Envelope
	var sawError bool
	for {
		ev := readEnv(t, conn)
		switch ev.Type {
		case protocol.TypeDone:
			done = ev
		case protocol.TypeError:
			sawError = true
			if !strings.Contains(string(ev.Payload), "estuary ledger append failed") {
				t.Fatalf("error payload=%s", ev.Payload)
			}
		}
		if sawError && done.EventID != "" {
			break
		}
	}
	if !srv.WaitTurns(2 * time.Second) {
		t.Fatal("forward did not finish")
	}
	if !strings.Contains(buf.String(), "err=ledger_append") {
		t.Fatalf("observe missing ledger_append: %s", buf.String())
	}
	views, err := led.Get(done.EventID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := views[estuary.SubjectCharacter]; ok {
		t.Fatal("character row written after injected failure")
	}
	user, ok := views[estuary.SubjectUser]
	if !ok {
		t.Fatal("user row missing")
	}
	if text, _ := user.Payload["text"].(string); text != "你好" {
		t.Fatalf("user text=%v", user.Payload)
	}
	if strings.Contains(fmtPayload(user), "灯織") {
		t.Fatalf("partial user row mixed character text: %v", user.Payload)
	}
}

func TestMergeAssistant(t *testing.T) {
	if got := mergeAssistant("灯織", "灯織"); got != "灯織" {
		t.Fatalf("full duplicate: %q", got)
	}
	if got := mergeAssistant("灯", "灯織"); got != "灯織" {
		t.Fatalf("done contains buffer: %q", got)
	}
	if got := mergeAssistant("灯織", "。"); got != "灯織。" {
		t.Fatalf("delta: %q", got)
	}
	if got := mergeAssistant("灯織", ""); got != "灯織" {
		t.Fatalf("empty done: %q", got)
	}
}

func fmtPayload(rec estuary.Record) string {
	raw, _ := json.Marshal(rec.Payload)
	return string(raw)
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
