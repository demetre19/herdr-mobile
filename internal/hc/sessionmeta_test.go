package hc

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionDirFromCwd(t *testing.T) {
	home := t.TempDir()
	sess := filepath.Join(home, ".omp", "agent", "sessions",
		"-Documents-DEV-WORK-Web Factory")
	if err := os.MkdirAll(sess, 0o755); err != nil {
		t.Fatal(err)
	}
	got := sessionDirFromCwd(home+"/Documents/DEV-WORK/Web Factory", home)
	if got != sess {
		t.Fatalf("sessionDirFromCwd = %q, want %q", got, sess)
	}
	if got := sessionDirFromCwd(home+"/no/such/dir", home); got != "" {
		t.Fatalf("missing dir = %q, want empty", got)
	}
	if got := sessionDirFromCwd("", home); got != "" {
		t.Fatalf("empty cwd = %q, want empty", got)
	}
}
