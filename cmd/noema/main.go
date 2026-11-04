package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/yurika0211/Noema/internal/adapters/lucky"
	"github.com/yurika0211/Noema/internal/engine"
	"github.com/yurika0211/Noema/internal/eventlog"
	"github.com/yurika0211/Noema/internal/observe"
	"github.com/yurika0211/Noema/internal/transport"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "noema: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("noema", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	home := fs.String("home", defaultHome(), "Noema-owned Lucky Agent HomeDir; never ~/.luckyagent")
	message := fs.String("message", "", "one-shot user message; empty starts a stdin loop")
	sessionID := fs.String("session", "", "existing session id; empty creates one")
	listen := fs.String("listen", "", "serve noema.v1 WebSocket on this address, e.g. 127.0.0.1:8787")
	apiKey := fs.String("api-key", os.Getenv("NOEMA_BIND_KEY"), "required API key for /v1/*; empty refuses every /v1 request (healthz stays open)")
	stateDir := fs.String("state-dir", defaultStateDir(), "Noema-owned event log dir; never inside the Lucky HomeDir")
	capsOnly := fs.Bool("capabilities", false, "print adapter capabilities as JSON and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}

	listenAddr := strings.TrimSpace(*listen)
	bindKey := strings.TrimSpace(*apiKey)
	if listenAddr != "" && bindKey == "" {
		return fmt.Errorf("-listen requires NOEMA_BIND_KEY / -api-key; /v1/* is rejected without a key, including loopback")
	}

	prompt, err := loadSoul()
	if err != nil {
		return err
	}
	adapter, err := lucky.Open(lucky.Config{
		HomeDir:      *home,
		Provider:     envOr("NOEMA_PROVIDER", "openai"),
		Model:        envOr("NOEMA_MODEL", "gpt-5.4-mini"),
		APIKey:       os.Getenv("NOEMA_API_KEY"),
		APIBase:      os.Getenv("NOEMA_API_BASE"),
		SystemPrompt: prompt,
	})
	if err != nil {
		return err
	}
	defer adapter.Close()

	if *capsOnly {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(adapter.Capabilities())
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if listenAddr != "" {
		if err := ensureStateDirOutsideHome(*stateDir, adapter.HomeDir()); err != nil {
			return err
		}
		logStore, err := eventlog.Open(*stateDir)
		if err != nil {
			return err
		}
		defer logStore.Close()
		srv := &transport.Server{
			Addr:        listenAddr,
			APIKey:      bindKey,
			CharacterID: "hiori",
			Engine:      adapter,
			Log:         logStore,
			Observe:     observe.New(os.Stderr),
		}
		errCh := make(chan error, 1)
		go func() { errCh <- srv.ListenAndServe() }()
		if err := srv.WaitReady(ctx); err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "listen=%s home=%s state=%s character=hiori\n", srv.ListenAddr(), adapter.HomeDir(), logStore.Dir())
		select {
		case <-ctx.Done():
			return srv.Close()
		case err := <-errCh:
			if transport.ListenError(err) {
				return err
			}
			return nil
		}
	}

	sid := strings.TrimSpace(*sessionID)
	if sid == "" {
		id, err := adapter.NewSession(ctx)
		if err != nil {
			return err
		}
		sid = id
	}
	fmt.Fprintf(os.Stdout, "home=%s session=%s\n", adapter.HomeDir(), sid)

	if strings.TrimSpace(*message) != "" {
		return oneTurn(ctx, adapter, sid, strings.TrimSpace(*message), os.Stdout)
	}
	return repl(ctx, adapter, sid, os.Stdin, os.Stdout)
}

func oneTurn(ctx context.Context, eng engine.Engine, sessionID, message string, w io.Writer) error {
	events, err := eng.StartTurn(ctx, engine.RunRequest{
		SessionID: sessionID,
		Message:   message,
	})
	if err != nil {
		return err
	}
	return printEvents(ctx, eng, events, w)
}

func repl(ctx context.Context, eng engine.Engine, sessionID string, in io.Reader, w io.Writer) error {
	fmt.Fprintln(w, "noema text sample. /quit exits. Ctrl-C cancels the current turn.")
	caps := eng.Capabilities()
	fmt.Fprintf(w, "capabilities: text_stream=%v cancel=%v host_context=%v ui_delta=%v approval=%v\n",
		caps.TextStream, caps.CancelTurn, caps.HostContext, caps.UIDelta, caps.ApprovalResolution)

	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for {
		fmt.Fprint(w, "> ")
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return err
			}
			return nil
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if line == "/quit" || line == "/exit" {
			return nil
		}
		if err := oneTurn(ctx, eng, sessionID, line, w); err != nil {
			fmt.Fprintf(w, "error: %v\n", err)
		}
	}
}

func printEvents(ctx context.Context, eng engine.Engine, events <-chan engine.RunEvent, w io.Writer) error {
	var runID string
	for {
		select {
		case <-ctx.Done():
			if runID != "" {
				_ = eng.CancelTurn(context.Background(), runID)
			}
			return ctx.Err()
		case ev, ok := <-events:
			if !ok {
				fmt.Fprintln(w)
				return nil
			}
			if ev.RunID != "" {
				runID = ev.RunID
			}
			switch ev.Type {
			case engine.EventContent:
				fmt.Fprint(w, ev.Content)
			case engine.EventDone:
				if ev.Content != "" {
					fmt.Fprint(w, ev.Content)
				}
				fmt.Fprintln(w)
				return nil
			case engine.EventCancelled:
				fmt.Fprintln(w, "\n[cancelled]")
				return ev.Err
			case engine.EventError:
				return ev.Err
			case engine.EventApproval:
				fmt.Fprintln(w, "\n[approval_required: unsupported on this SDK pin; write tools stay disabled]")
			}
		}
	}
}

func loadSoul() (string, error) {
	candidates := []string{
		filepath.Join("characters", "hiori", "SOUL.md"),
		filepath.Join("..", "..", "characters", "hiori", "SOUL.md"),
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "..", "characters", "hiori", "SOUL.md"))
	}
	for _, p := range candidates {
		raw, err := os.ReadFile(p)
		if err == nil {
			return string(raw), nil
		}
	}
	return "", fmt.Errorf("characters/hiori/SOUL.md not found")
}

func defaultHome() string {
	if v := strings.TrimSpace(os.Getenv("NOEMA_HOME")); v != "" {
		return v
	}
	return filepath.Join("data", "lucky-home")
}

// defaultStateDir 是 Noema 自己的事件日志目录，与 Lucky HomeDir 分开。
func defaultStateDir() string {
	if v := strings.TrimSpace(os.Getenv("NOEMA_STATE_DIR")); v != "" {
		return v
	}
	return filepath.Join("data", "noema-state")
}

// ensureStateDirOutsideHome 防止把产品事件日志写进 Lucky HomeDir 冒充权威库。
func ensureStateDirOutsideHome(stateDir, homeDir string) error {
	state := strings.TrimSpace(stateDir)
	if state == "" {
		return fmt.Errorf("state dir is required")
	}
	absState, err := filepath.Abs(state)
	if err != nil {
		return fmt.Errorf("resolve state dir: %w", err)
	}
	absHome, err := filepath.Abs(strings.TrimSpace(homeDir))
	if err != nil {
		return fmt.Errorf("resolve home dir: %w", err)
	}
	if absState == absHome || strings.HasPrefix(absState, absHome+string(os.PathSeparator)) {
		return fmt.Errorf("state dir %s is inside the Lucky HomeDir %s; use a Noema-owned directory such as data/noema-state", absState, absHome)
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
