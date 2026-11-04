package transport

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/yurika0211/Noema/internal/engine"
	"github.com/yurika0211/Noema/internal/eventlog"
	"github.com/yurika0211/Noema/internal/observe"
	"github.com/yurika0211/Noema/internal/protocol"
)

const maxFrameBytes = 8_000_000

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin == "" {
			return true
		}
		return strings.HasPrefix(origin, "http://127.0.0.1") ||
			strings.HasPrefix(origin, "http://localhost") ||
			strings.HasPrefix(origin, "http://10.0.2.2")
	},
}

type Server struct {
	Addr        string
	APIKey      string
	CharacterID string
	Engine      engine.Engine
	Log         *eventlog.Store
	Observe     *observe.Logger

	mu      sync.Mutex
	httpSrv *http.Server
	ready   chan struct{}

	// turns 是还没结束的 forward。测试用来等日志写完，不靠 sleep。
	turns sync.WaitGroup
}

func (s *Server) ListenAndServe() error {
	if s.Log == nil {
		s.Log = eventlog.New()
	}

	if s.CharacterID == "" {
		s.CharacterID = "hiori"
	}
	ready := s.readyCh()
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/v1/capabilities", s.handleCapabilities)
	mux.HandleFunc("/v1/ws", s.handleWS)

	srv := &http.Server{
		Addr:              s.Addr,
		Handler:           s.withAuth(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}
	s.mu.Lock()
	s.httpSrv = srv
	s.mu.Unlock()

	ln, err := net.Listen("tcp", s.Addr)
	if err != nil {
		select {
		case <-ready:
		default:
			close(ready)
		}
		return err
	}
	s.mu.Lock()
	s.Addr = ln.Addr().String()
	s.mu.Unlock()
	select {
	case <-ready:
	default:
		close(ready)
	}
	return srv.Serve(ln)
}

func (s *Server) readyCh() chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ready == nil {
		s.ready = make(chan struct{})
	}
	return s.ready
}

func (s *Server) WaitReady(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.readyCh():
		return nil
	}
}

func (s *Server) Close() error {
	s.mu.Lock()
	srv := s.httpSrv
	s.mu.Unlock()
	if srv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}

func (s *Server) ListenAddr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Addr
}

// withAuth 保护 /v1/*。没有配置 NOEMA_BIND_KEY 时一律 401，而不是放行：
// loopback 开发也必须设 key，否则真机走 LAN 时同一进程就是裸接口。
// /healthz 不鉴权，只用来探活。
func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		want := strings.TrimSpace(s.APIKey)
		if want == "" {
			http.Error(w, "unauthorized: NOEMA_BIND_KEY is not configured", http.StatusUnauthorized)
			return
		}
		got := strings.TrimSpace(r.Header.Get("Authorization"))
		if strings.HasPrefix(got, "Bearer ") {
			got = strings.TrimSpace(strings.TrimPrefix(got, "Bearer "))
		}
		if got == "" {
			got = strings.TrimSpace(r.Header.Get("X-API-Key"))
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) handleCapabilities(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.Engine.Capabilities())
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	raw, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	conn := newConn(raw)
	defer conn.Close()
	conn.inner.SetReadLimit(maxFrameBytes)
	_ = conn.inner.SetReadDeadline(time.Now().Add(90 * time.Second))
	conn.inner.SetPongHandler(func(string) error {
		return conn.inner.SetReadDeadline(time.Now().Add(90 * time.Second))
	})

	sessionID := strings.TrimSpace(r.URL.Query().Get("session"))
	if sessionID == "" {
		id, err := s.Engine.NewSession(r.Context())
		if err != nil {
			_ = writeJSON(conn, protocol.Envelope{Type: protocol.TypeError, Payload: eventlog.MustPayload(protocol.ErrorPayload{Error: err.Error()})})
			return
		}
		sessionID = id
	}

	helloPayload, _ := json.Marshal(protocol.HelloPayload{
		SessionID:    sessionID,
		CharacterID:  s.CharacterID,
		Capabilities: eventlog.MustPayload(s.Engine.Capabilities()),
	})
	_ = writeJSON(conn, protocol.Envelope{
		Type:        protocol.TypeHello,
		CharacterID: s.CharacterID,
		SessionID:   sessionID,
		Payload:     helloPayload,
	})

	var (
		mu     sync.Mutex
		runID  string
		cancel context.CancelFunc
	)
	stopTurn := func() {
		mu.Lock()
		id := runID
		c := cancel
		runID = ""
		cancel = nil
		mu.Unlock()
		if c != nil {
			c()
		}
		if id != "" {
			_ = s.Engine.CancelTurn(context.Background(), id)
		}
	}
	defer stopTurn()

	for {
		_, data, err := conn.inner.ReadMessage()
		if err != nil {
			return
		}
		_ = conn.inner.SetReadDeadline(time.Now().Add(90 * time.Second))
		if len(data) > maxFrameBytes {
			continue
		}
		var env protocol.Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			_ = writeJSON(conn, protocol.Envelope{Type: protocol.TypeError, Payload: eventlog.MustPayload(protocol.ErrorPayload{Error: "invalid json"})})
			continue
		}
		switch env.Type {
		case protocol.TypePing:
			_ = writeJSON(conn, protocol.Envelope{Type: protocol.TypePong, SessionID: sessionID})
		case protocol.TypeReplay:
			s.replay(conn, sessionID, env)
		case protocol.TypeCancel:
			stopTurn()
			out, _ := s.Log.Append(sessionID, protocol.Envelope{
				Type:            protocol.TypeCancelled,
				SessionID:       sessionID,
				ClientRequestID: env.ClientRequestID,
				CharacterID:     s.CharacterID,
			})
			_ = writeJSON(conn, out)
		case protocol.TypeChat:
			stopTurn()
			turnCtx, turnCancel := context.WithCancel(r.Context())
			mu.Lock()
			cancel = turnCancel
			mu.Unlock()
			id, err := s.startChat(turnCtx, conn, sessionID, env)
			if err != nil {
				turnCancel()
				out, _ := s.Log.Append(sessionID, protocol.Envelope{
					Type:            protocol.TypeError,
					SessionID:       sessionID,
					ClientRequestID: env.ClientRequestID,
					CharacterID:     s.CharacterID,
					Payload:         eventlog.MustPayload(protocol.ErrorPayload{Error: err.Error()}),
				})
				_ = writeJSON(conn, out)
				continue
			}
			mu.Lock()
			runID = id
			mu.Unlock()
		default:
			_ = writeJSON(conn, protocol.Envelope{Type: protocol.TypeError, Payload: eventlog.MustPayload(protocol.ErrorPayload{Error: "unknown type"})})
		}
	}
}

// replay 只读地把 last_seq 之后的事件推给客户端。
// replay_complete 是控制帧，不落盘也不占 seq：重连不该改写事件日志。
func (s *Server) replay(conn *wsConn, sessionID string, env protocol.Envelope) {
	var body protocol.ReplayPayload
	_ = json.Unmarshal(env.Payload, &body)
	for _, ev := range s.Log.After(sessionID, body.LastSeq) {
		_ = writeJSON(conn, ev)
	}
	_ = writeJSON(conn, protocol.Envelope{
		Protocol:    protocol.Version,
		Type:        protocol.TypeReplayComplete,
		SessionID:   sessionID,
		CharacterID: s.CharacterID,
		CreatedAt:   time.Now().UTC(),
		Payload:     eventlog.MustPayload(map[string]int64{"last_seq": s.Log.LastSeq(sessionID)}),
	})
}

func (s *Server) startChat(ctx context.Context, conn *wsConn, sessionID string, env protocol.Envelope) (string, error) {
	var body protocol.ChatPayload
	if err := json.Unmarshal(env.Payload, &body); err != nil {
		return "", fmt.Errorf("%w: chat payload", engine.ErrInvalidRequest)
	}
	message := strings.TrimSpace(body.Message)
	if message == "" {
		return "", fmt.Errorf("%w: empty message", engine.ErrInvalidRequest)
	}
	runID, replay, err := s.Log.BeginRun("local", sessionID, env.ClientRequestID, message)
	if err != nil {
		return "", err
	}
	if replay {
		turn := s.Observe.Start(protocol.Version, sessionID, runID, env.ClientRequestID)
		n := 0
		for _, ev := range s.Log.ForRequest(sessionID, env.ClientRequestID) {
			raw, err := ev.Bytes()
			if err == nil {
				n += len(raw)
			}
			_ = writeJSON(conn, ev)
		}
		s.Observe.AddBytes(turn, n)
		s.Observe.Finish(turn, "replay", "")
		return runID, nil
	}
	events, err := s.Engine.StartTurn(ctx, engine.RunRequest{
		OwnerID:         "local",
		CharacterID:     s.CharacterID,
		SessionID:       sessionID,
		Message:         message,
		ClientRequestID: env.ClientRequestID,
	})
	if err != nil {
		turn := s.Observe.Start(protocol.Version, sessionID, runID, env.ClientRequestID)
		s.Observe.Finish(turn, "error", errClass(err))
		return "", err
	}
	s.turns.Add(1)
	go func() {
		defer s.turns.Done()
		s.forward(ctx, conn, sessionID, runID, env.ClientRequestID, events)
	}()
	return runID, nil
}

// WaitTurns 等到已经启动的 forward 都返回。超时返回 false。
func (s *Server) WaitTurns(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		s.turns.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func errClass(err error) string {
	switch {
	case err == nil:
		return "-"
	case errors.Is(err, engine.ErrInvalidRequest):
		return "invalid_request"
	case errors.Is(err, engine.ErrUnsupported):
		return "unsupported"
	case errors.Is(err, engine.ErrNotFound):
		return "not_found"
	default:
		return "turn_failed"
	}
}

func (s *Server) forward(ctx context.Context, conn *wsConn, sessionID, runID, clientRequestID string, events <-chan engine.RunEvent) {
	turn := s.Observe.Start(protocol.Version, sessionID, runID, clientRequestID)
	status := "cancelled"
	class := "cancelled"
	for {
		select {
		case <-ctx.Done():
			s.Observe.Finish(turn, status, class)
			return
		case ev, ok := <-events:
			if !ok {
				s.Observe.Finish(turn, status, class)
				return
			}
			out := protocol.Envelope{
				Type:            string(ev.Type),
				RunID:           runID,
				ClientRequestID: clientRequestID,
				SessionID:       sessionID,
				CharacterID:     s.CharacterID,
			}
			visible := 0
			switch ev.Type {
			case engine.EventContent:
				visible = len(ev.Content)
				out.Payload = eventlog.MustPayload(protocol.ContentPayload{Text: ev.Content})
			case engine.EventDone:
				visible = len(ev.Content)
				out.Payload = eventlog.MustPayload(protocol.ContentPayload{Text: ev.Content})
				status, class = "done", "-"
			case engine.EventError:
				msg := "turn failed"
				if ev.Err != nil {
					msg = ev.Err.Error()
				}
				out.Payload = eventlog.MustPayload(protocol.ErrorPayload{Error: msg})
				status, class = "error", errClass(ev.Err)
			case engine.EventCancelled:
				out.Type = protocol.TypeCancelled
				status, class = "cancelled", "cancelled"
			case engine.EventApproval:
				out.Type = protocol.TypeError
				out.Payload = eventlog.MustPayload(protocol.ErrorPayload{Error: engine.Unsupported("approval_resolution").Error()})
				status, class = "error", "unsupported"
			}
			if visible > 0 {
				s.Observe.FirstByte(turn, visible)
			}
			stored, err := s.Log.Append(sessionID, out)
			if err != nil {
				s.Observe.Finish(turn, "error", "log_append")
				return
			}
			if err := writeJSON(conn, stored); err != nil {
				s.Observe.Finish(turn, status, class)
				return
			}
			if ev.Type == engine.EventDone || ev.Type == engine.EventError || ev.Type == engine.EventCancelled {
				s.Observe.Finish(turn, status, class)
				return
			}
		}
	}
}

type wsConn struct {
	inner *websocket.Conn
	mu    sync.Mutex
}

func newConn(inner *websocket.Conn) *wsConn { return &wsConn{inner: inner} }

func (c *wsConn) Close() error { return c.inner.Close() }

func writeJSON(conn *wsConn, env protocol.Envelope) error {
	raw, err := env.Bytes()
	if err != nil {
		return err
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()
	_ = conn.inner.SetWriteDeadline(time.Now().Add(15 * time.Second))
	return conn.inner.WriteMessage(websocket.TextMessage, raw)
}

func ListenError(err error) bool {
	return err != nil && !errors.Is(err, http.ErrServerClosed)
}
