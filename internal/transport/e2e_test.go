package transport

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/yurika0211/Noema/internal/engine"
	"github.com/yurika0211/Noema/internal/eventlog"
	"github.com/yurika0211/Noema/internal/protocol"
)

// 这一条不打 provider：用 stub engine 走真实 TCP + 真实落盘，
// 覆盖 #8 要求的「发一条 → Stop → 断线 → 重连只重放已持久事件」。
func TestRestartReplayAndIdempotentReconnect(t *testing.T) {
	stateDir := t.TempDir()
	const bindKey = "test-bind-key"
	const sessionID = "sess-e2e"

	eng := &stubEngine{
		sessionID: sessionID,
		caps:      engine.Capabilities{TextStream: true, CancelTurn: true, IsolatedHomeDir: true},
		events: []engine.RunEvent{
			{Type: engine.EventContent, Content: "你"},
			{Type: engine.EventContent, Content: "好"},
			{Type: engine.EventDone},
		},
		cancelled: make(chan string, 4),
	}

	// --- 第一段：起服务、发一条、收到流式正文与 done ---
	store1, err := eventlog.Open(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	srv1 := &Server{Addr: "127.0.0.1:0", APIKey: bindKey, CharacterID: "hiori", Engine: eng, Log: store1}
	go func() { _ = srv1.ListenAndServe() }()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv1.WaitReady(ctx); err != nil {
		t.Fatalf("wait ready: %v", err)
	}

	conn1 := dialWS(t, srv1.ListenAddr(), bindKey, sessionID)
	if hello := readEnv(t, conn1); hello.Type != protocol.TypeHello {
		t.Fatalf("hello type=%s", hello.Type)
	}
	if err := conn1.WriteJSON(protocol.Envelope{
		Protocol:        protocol.Version,
		Type:            protocol.TypeChat,
		ClientRequestID: "req-1",
		Payload:         eventlog.MustPayload(protocol.ChatPayload{Message: "你好"}),
	}); err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	var doneSeq int64
	var runID string
stream:
	for {
		ev := readEnv(t, conn1)
		if ev.RunID != "" {
			runID = ev.RunID
		}
		switch ev.Type {
		case protocol.TypeContent:
			var body protocol.ContentPayload
			_ = json.Unmarshal(ev.Payload, &body)
			text.WriteString(body.Text)
		case protocol.TypeDone:
			doneSeq = ev.Seq
			if text.String() != "你好" {
				t.Fatalf("streamed text=%q", text.String())
			}
			break stream
		case protocol.TypeError:
			t.Fatalf("turn error: %s", string(ev.Payload))
		default:
			t.Fatalf("unexpected %s", ev.Type)
		}
	}
	if doneSeq == 0 || runID == "" {
		t.Fatalf("done seq=%d run id=%q", doneSeq, runID)
	}

	// Stop：回合已结束，cancel 只产生并持久化一条 cancelled。
	if err := conn1.WriteJSON(protocol.Envelope{
		Type:            protocol.TypeCancel,
		ClientRequestID: "req-1",
		SessionID:       sessionID,
	}); err != nil {
		t.Fatal(err)
	}
	cancelled := readEnv(t, conn1)
	if cancelled.Type != protocol.TypeCancelled {
		t.Fatalf("cancel type=%s", cancelled.Type)
	}
	if cancelled.Seq != doneSeq+1 {
		t.Fatalf("cancel seq=%d want %d", cancelled.Seq, doneSeq+1)
	}

	// 模拟断线：关客户端连接，再关服务与日志文件。
	_ = conn1.Close()
	shutdown(t, srv1)
	if err := store1.Close(); err != nil {
		t.Fatal(err)
	}

	// --- 第二段：模拟重启，同一 session 重连 ---
	store2, err := eventlog.Open(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store2.Close()
	if got := store2.LastSeq(sessionID); got != cancelled.Seq {
		t.Fatalf("persisted last seq=%d want %d", got, cancelled.Seq)
	}

	srv2 := &Server{Addr: "127.0.0.1:0", APIKey: bindKey, CharacterID: "hiori", Engine: eng, Log: store2}
	go func() { _ = srv2.ListenAndServe() }()
	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()
	if err := srv2.WaitReady(ctx2); err != nil {
		t.Fatalf("wait ready 2: %v", err)
	}
	defer srv2.Close()

	conn2 := dialWS(t, srv2.ListenAddr(), bindKey, sessionID)
	if hello := readEnv(t, conn2); hello.Type != protocol.TypeHello {
		t.Fatalf("hello2 type=%s", hello.Type)
	}

	// 从游标 2 重连：只重放已持久且 seq>2 的事件，不重跑回合。
	if err := conn2.WriteJSON(protocol.Envelope{
		Type:    protocol.TypeReplay,
		Payload: eventlog.MustPayload(protocol.ReplayPayload{LastSeq: 2}),
	}); err != nil {
		t.Fatal(err)
	}
	var replayed []protocol.Envelope
	var complete protocol.Envelope
	for {
		ev := readEnv(t, conn2)
		if ev.Type == protocol.TypeReplayComplete {
			complete = ev
			break
		}
		replayed = append(replayed, ev)
	}
	if want := int(cancelled.Seq - 2); len(replayed) != want {
		t.Fatalf("replayed %d events want %d: %+v", len(replayed), want, replayed)
	}
	for _, ev := range replayed {
		if ev.Seq <= 2 {
			t.Fatalf("replayed already-seen seq=%d", ev.Seq)
		}
	}
	var replayBody protocol.ReplayPayload
	_ = json.Unmarshal(complete.Payload, &replayBody)
	if replayBody.LastSeq != cancelled.Seq {
		t.Fatalf("replay_complete last_seq=%d want %d", replayBody.LastSeq, cancelled.Seq)
	}

	// 重发同一个 client_request_id：幂等回放，不产生新回合。
	if err := conn2.WriteJSON(protocol.Envelope{
		Protocol:        protocol.Version,
		Type:            protocol.TypeChat,
		ClientRequestID: "req-1",
		SessionID:       sessionID,
		Payload:         eventlog.MustPayload(protocol.ChatPayload{Message: "你好"}),
	}); err != nil {
		t.Fatal(err)
	}
	// 幂等回放把这一轮已经发生过的事件按 seq 全部吐出（含 Stop 产生的 cancelled），
	// 但不重跑模型：StartTurn 调用数保持为 1。
	var replayTypes []string
	for {
		ev := readEnv(t, conn2)
		replayTypes = append(replayTypes, ev.Type)
		if ev.Type == protocol.TypeError {
			t.Fatalf("idempotent replay error: %s", string(ev.Payload))
		}
		if ev.Type == protocol.TypeCancelled {
			break
		}
	}
	if got := strings.Join(replayTypes, ","); got != "content,content,done,cancelled" {
		t.Fatalf("idempotent replay=%s", got)
	}
	if got := store2.LastSeq(sessionID); got != cancelled.Seq {
		t.Fatalf("idempotent replay appended events: last seq=%d want %d", got, cancelled.Seq)
	}
	if got := eng.starts.Load(); got != 1 {
		t.Fatalf("engine StartTurn calls=%d want 1 (no new turn on reconnect)", got)
	}

	// 同一输入不同内容必须冲突，而不是悄悄开新回合。
	if err := conn2.WriteJSON(protocol.Envelope{
		Protocol:        protocol.Version,
		Type:            protocol.TypeChat,
		ClientRequestID: "req-1",
		SessionID:       sessionID,
		Payload:         eventlog.MustPayload(protocol.ChatPayload{Message: "另一句"}),
	}); err != nil {
		t.Fatal(err)
	}
	if ev := readEnv(t, conn2); ev.Type != protocol.TypeError {
		t.Fatalf("conflict type=%s seq=%d payload=%s want error", ev.Type, ev.Seq, string(ev.Payload))
	}

	// 无 key 不能连。
	_, resp, err := websocket.DefaultDialer.Dial("ws://"+srv2.ListenAddr()+"/v1/ws?session="+sessionID, nil)
	if err == nil {
		t.Fatal("expected unauthorized dial")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized resp=%v err=%v", resp, err)
	}
	_ = resp.Body.Close()
}

func dialWS(t *testing.T, addr, key, sessionID string) *websocket.Conn {
	t.Helper()
	header := http.Header{}
	header.Set("Authorization", "Bearer "+key)
	conn, resp, err := websocket.DefaultDialer.Dial("ws://"+addr+"/v1/ws?session="+sessionID, header)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
			_ = resp.Body.Close()
		}
		t.Fatalf("dial: %v status=%d", err, status)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// shutdown 关服务并留一点时间让 WS 处理协程退出，避免与下一次打开同一个日志文件竞争。
func shutdown(t *testing.T, srv *Server) {
	t.Helper()
	if err := srv.Close(); err != nil {
		t.Fatalf("close server: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
}
