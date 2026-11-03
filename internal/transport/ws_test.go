package transport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/yurika0211/Noema/internal/engine"
	"github.com/yurika0211/Noema/internal/eventlog"
	"github.com/yurika0211/Noema/internal/protocol"
)

type stubEngine struct {
	caps      engine.Capabilities
	sessionID string
	events    []engine.RunEvent
	cancelled chan string
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
