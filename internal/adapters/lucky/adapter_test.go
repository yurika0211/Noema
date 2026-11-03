package lucky

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yurika0211/Noema/internal/engine"
)

func TestOpenRequiresHomeDir(t *testing.T) {
	_, err := Open(Config{Provider: "openai", Model: "gpt-5.4-mini", APIKey: "test"})
	if err == nil || !strings.Contains(err.Error(), "HomeDir") {
		t.Fatalf("expected HomeDir error, got %v", err)
	}
}

func TestOpenIsolatesHomeAndReportsCapabilities(t *testing.T) {
	home := t.TempDir()
	a, err := Open(Config{
		HomeDir:      home,
		Provider:     "openai",
		Model:        "gpt-5.4-mini",
		APIKey:       "test-key-not-used",
		SystemPrompt: "You are 灯織.",
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer a.Close()

	abs, err := filepath.Abs(home)
	if err != nil {
		t.Fatal(err)
	}
	if got := a.HomeDir(); got != abs {
		t.Fatalf("HomeDir=%q want %q", got, abs)
	}
	if strings.Contains(a.HomeDir(), ".luckyagent") && !strings.HasPrefix(a.HomeDir(), abs) {
		t.Fatalf("home escaped into default CLI profile: %s", a.HomeDir())
	}

	caps := a.Capabilities()
	if !caps.TextStream || !caps.CancelTurn || !caps.IsolatedHomeDir {
		t.Fatalf("text path capabilities missing: %+v", caps)
	}
	if caps.HostContext || caps.UIDelta || caps.ApprovalResolution || caps.ContextualTools || caps.ControlledMemory {
		t.Fatalf("unsupported capabilities must stay false: %+v", caps)
	}

	soul := filepath.Join(a.HomeDir(), "memory", "prompts", "SOUL.md")
	raw, err := readFile(soul)
	if err != nil {
		t.Fatalf("soul: %v", err)
	}
	if !strings.Contains(raw, "灯織") {
		t.Fatalf("soul missing identity: %q", raw)
	}
}

func TestDeniedToolsStayDisabled(t *testing.T) {
	a, err := Open(Config{
		HomeDir:  t.TempDir(),
		Provider: "openai",
		Model:    "gpt-5.4-mini",
		APIKey:   "test-key-not-used",
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer a.Close()

	enabled := map[string]struct{}{}
	for _, name := range a.EnabledToolNames() {
		enabled[name] = struct{}{}
	}
	for _, name := range []string{"terminal", "file_write", "file_delete", "http_request", "computer_act", "remember", "autonomy"} {
		if _, ok := enabled[name]; ok {
			t.Fatalf("tool %s should be disabled", name)
		}
	}
}

func TestHostContextAndApprovalAreUnsupported(t *testing.T) {
	a, err := Open(Config{
		HomeDir:  t.TempDir(),
		Provider: "openai",
		Model:    "gpt-5.4-mini",
		APIKey:   "test-key-not-used",
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer a.Close()

	_, err = a.StartTurn(context.Background(), engine.RunRequest{
		Message:     "hello",
		HostContext: &engine.HostContext{},
	})
	if !errors.Is(err, engine.ErrUnsupported) {
		t.Fatalf("host context: %v", err)
	}

	err = a.ResolveApproval(context.Background(), engine.ApprovalDecision{RunID: "r1", RequestID: "a1"})
	if !errors.Is(err, engine.ErrUnsupported) {
		t.Fatalf("approval: %v", err)
	}
}

func TestNewSessionStaysInHome(t *testing.T) {
	a, err := Open(Config{
		HomeDir:  t.TempDir(),
		Provider: "openai",
		Model:    "gpt-5.4-mini",
		APIKey:   "test-key-not-used",
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer a.Close()

	sid, err := a.NewSession(context.Background())
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if sid == "" {
		t.Fatal("empty session id")
	}
}

func TestEmptyMessageRejected(t *testing.T) {
	a, err := Open(Config{
		HomeDir:  t.TempDir(),
		Provider: "openai",
		Model:    "gpt-5.4-mini",
		APIKey:   "test-key-not-used",
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer a.Close()

	_, err = a.StartTurn(context.Background(), engine.RunRequest{})
	if !errors.Is(err, engine.ErrInvalidRequest) {
		t.Fatalf("empty message: %v", err)
	}
}

func readFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
