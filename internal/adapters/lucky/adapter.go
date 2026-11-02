package lucky

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/yurika0211/luckyagent-app/sdk"

	"github.com/yurika0211/Noema/internal/engine"
)

// Config 是适配层启动参数。HomeDir 必须显式设置，不能落到 ~/.luckyagent。
type Config struct {
	HomeDir      string
	Provider     string
	Model        string
	APIKey       string
	APIBase      string
	SystemPrompt string
}

type Adapter struct {
	agent *sdk.Agent
	home  string

	mu      sync.Mutex
	cancels map[string]context.CancelFunc
	seq     atomic.Uint64
}

func Open(cfg Config) (*Adapter, error) {
	home := strings.TrimSpace(cfg.HomeDir)
	if home == "" {
		return nil, fmt.Errorf("lucky adapter: HomeDir is required")
	}
	abs, err := filepath.Abs(home)
	if err != nil {
		return nil, fmt.Errorf("lucky adapter: resolve HomeDir: %w", err)
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, fmt.Errorf("lucky adapter: create HomeDir: %w", err)
	}

	autoApprove := false
	agent, err := sdk.New(sdk.Config{
		HomeDir:      abs,
		Provider:     cfg.Provider,
		Model:        cfg.Model,
		APIKey:       cfg.APIKey,
		APIBase:      cfg.APIBase,
		SystemPrompt: cfg.SystemPrompt,
		AutoApprove:  &autoApprove,
		DisableTools: deniedTools,
	})
	if err != nil {
		return nil, fmt.Errorf("lucky adapter: sdk.New: %w", err)
	}

	a := &Adapter{
		agent:   agent,
		home:    agent.HomeDir(),
		cancels: make(map[string]context.CancelFunc),
	}
	if err := a.lockDownTools(); err != nil {
		_ = a.Close()
		return nil, err
	}
	return a, nil
}

func (a *Adapter) lockDownTools() error {
	if a.agent == nil {
		return fmt.Errorf("lucky adapter: closed")
	}
	for _, info := range a.agent.ListTools() {
		name := strings.TrimSpace(info.Name)
		if name == "" {
			continue
		}
		if _, ok := allowedReadTools[name]; ok {
			continue
		}
		if err := a.agent.DisableTool(name); err != nil {
			// 未知工具在某些版本上 Disable 会报 not found；默认拒绝仍算成功。
			if !strings.Contains(err.Error(), "not found") {
				return fmt.Errorf("lucky adapter: disable %s: %w", name, err)
			}
		}
	}
	return nil
}

func (a *Adapter) Capabilities() engine.Capabilities {
	return engine.Capabilities{
		TextStream:         true,
		CancelTurn:         true,
		IsolatedHomeDir:    true,
		HostContext:        false,
		UIDelta:            false,
		ApprovalResolution: false,
		ContextualTools:    false,
		ControlledMemory:   false,
	}
}

func (a *Adapter) HomeDir() string {
	if a == nil {
		return ""
	}
	return a.home
}

func (a *Adapter) NewSession(ctx context.Context) (string, error) {
	_ = ctx
	if a == nil || a.agent == nil {
		return "", fmt.Errorf("lucky adapter: closed")
	}
	id, err := a.agent.NewSession()
	if err != nil {
		return "", fmt.Errorf("lucky adapter: new session: %w", err)
	}
	return id, nil
}

func (a *Adapter) StartTurn(ctx context.Context, request engine.RunRequest) (<-chan engine.RunEvent, error) {
	if a == nil || a.agent == nil {
		return nil, fmt.Errorf("lucky adapter: closed")
	}
	if request.HostContext != nil {
		return nil, engine.Unsupported("host_context")
	}
	msg := strings.TrimSpace(request.Message)
	if msg == "" {
		return nil, fmt.Errorf("%w: empty message", engine.ErrInvalidRequest)
	}

	sessionID := strings.TrimSpace(request.SessionID)
	if sessionID == "" {
		id, err := a.agent.NewSession()
		if err != nil {
			return nil, fmt.Errorf("lucky adapter: new session: %w", err)
		}
		sessionID = id
	}

	runID := fmt.Sprintf("run_%d", a.seq.Add(1))

	turnCtx, cancel := context.WithCancel(ctx)
	a.mu.Lock()
	a.cancels[runID] = cancel
	a.mu.Unlock()

	sdkEvents, err := a.agent.ChatSessionStream(turnCtx, sessionID, msg)
	if err != nil {
		cancel()
		a.forget(runID)
		return nil, fmt.Errorf("lucky adapter: chat: %w", err)
	}

	out := make(chan engine.RunEvent, 64)
	go a.forward(turnCtx, cancel, runID, sessionID, request.ClientRequestID, sdkEvents, out)
	return out, nil
}

func (a *Adapter) forward(
	ctx context.Context,
	cancel context.CancelFunc,
	runID, sessionID, clientRequestID string,
	in <-chan sdk.Event,
	out chan<- engine.RunEvent,
) {
	defer close(out)
	defer cancel()
	defer a.forget(runID)

	emit := func(ev engine.RunEvent) {
		ev.RunID = runID
		ev.SessionID = sessionID
		ev.ClientRequestID = clientRequestID
		select {
		case out <- ev:
		case <-ctx.Done():
		}
	}

	cancelled := false
	for {
		select {
		case <-ctx.Done():
			cancelled = true
			emit(engine.RunEvent{Type: engine.EventCancelled, Err: ctx.Err()})
			return
		case ev, ok := <-in:
			if !ok {
				if !cancelled {
					emit(engine.RunEvent{Type: engine.EventDone})
				}
				return
			}
			switch ev.Type {
			case sdk.EventContent:
				emit(engine.RunEvent{Type: engine.EventContent, Content: ev.Content})
			case sdk.EventDone:
				emit(engine.RunEvent{Type: engine.EventDone, Content: ev.Content})
				return
			case sdk.EventError:
				emit(engine.RunEvent{Type: engine.EventError, Err: ev.Err, Content: ev.Content})
				return
			case sdk.EventApprovalRequired:
				emit(engine.RunEvent{
					Type:    engine.EventApproval,
					Content: "approval_required but ResolveApproval is unsupported on this SDK pin",
					Err:     engine.Unsupported("approval_resolution"),
				})
			default:
				// thinking / tool / reasoning 不进入陪伴界面。
			}
		}
	}
}

func (a *Adapter) ResolveApproval(ctx context.Context, decision engine.ApprovalDecision) error {
	_ = ctx
	_ = decision
	return engine.Unsupported("approval_resolution")
}

func (a *Adapter) CancelTurn(ctx context.Context, runID string) error {
	_ = ctx
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return fmt.Errorf("%w: empty run id", engine.ErrInvalidRequest)
	}
	a.mu.Lock()
	cancel, ok := a.cancels[runID]
	a.mu.Unlock()
	if !ok {
		return fmt.Errorf("%w: run %s", engine.ErrNotFound, runID)
	}
	cancel()
	return nil
}

func (a *Adapter) Close() error {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	for id, cancel := range a.cancels {
		cancel()
		delete(a.cancels, id)
	}
	agent := a.agent
	a.agent = nil
	a.mu.Unlock()
	if agent == nil {
		return nil
	}
	return agent.Close()
}

func (a *Adapter) forget(runID string) {
	a.mu.Lock()
	delete(a.cancels, runID)
	a.mu.Unlock()
}

func (a *Adapter) DisabledToolNames() []string {
	if a == nil || a.agent == nil {
		return nil
	}
	var out []string
	for _, info := range a.agent.ListTools() {
		if !info.Enabled {
			out = append(out, info.Name)
		}
	}
	return out
}

func (a *Adapter) EnabledToolNames() []string {
	if a == nil || a.agent == nil {
		return nil
	}
	var out []string
	for _, info := range a.agent.ListTools() {
		if info.Enabled {
			out = append(out, info.Name)
		}
	}
	return out
}

func IsUnsupported(err error) bool {
	return errors.Is(err, engine.ErrUnsupported)
}
