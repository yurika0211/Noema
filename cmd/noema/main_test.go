package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestStateDirMustStayOutsideHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), "lucky-home")
	cases := []struct {
		name    string
		state   string
		wantErr bool
	}{
		{name: "sibling", state: filepath.Join(home, "..", "noema-state"), wantErr: false},
		{name: "same", state: home, wantErr: true},
		{name: "nested", state: filepath.Join(home, "eventlog"), wantErr: true},
		{name: "empty", state: "  ", wantErr: true},
	}
	for _, tc := range cases {
		err := ensureStateDirOutsideHome(tc.state, home)
		if tc.wantErr && err == nil {
			t.Fatalf("%s: expected error for %q", tc.name, tc.state)
		}
		if !tc.wantErr && err != nil {
			t.Fatalf("%s: unexpected error: %v", tc.name, err)
		}
	}
}

func TestDefaultStateDirIsNotLuckyHome(t *testing.T) {
	t.Setenv("NOEMA_STATE_DIR", "")
	if got := defaultStateDir(); got == defaultHome() || strings.Contains(got, "lucky-home") {
		t.Fatalf("default state dir %q looks like a Lucky HomeDir", got)
	}
	if got := defaultStateDir(); !strings.HasSuffix(got, filepath.Join("data", "noema-state")) {
		t.Fatalf("default state dir=%q", got)
	}
}

func TestListenRequiresBindKey(t *testing.T) {
	t.Setenv("NOEMA_BIND_KEY", "")
	err := run([]string{"-listen", "127.0.0.1:0"})
	if err == nil {
		t.Fatal("expected error: -listen without NOEMA_BIND_KEY must be refused")
	}
	if !strings.Contains(err.Error(), "NOEMA_BIND_KEY") {
		t.Fatalf("error=%v", err)
	}
}
