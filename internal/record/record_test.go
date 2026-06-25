package record

import (
	"errors"
	"testing"
	"time"
)

type stubStore struct {
	inserted []Record
	err      error
}

func (s *stubStore) Insert(r Record) error {
	if s.err != nil {
		return s.err
	}
	s.inserted = append(s.inserted, r)
	return nil
}

func TestRunValidInput(t *testing.T) {
	store := &stubStore{}
	args := []string{"--cmd", "ls -la", "--dir", "/tmp", "--session", "test-123"}

	before := time.Now().Unix()
	if err := Run(args, store); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	after := time.Now().Unix()

	if len(store.inserted) != 1 {
		t.Fatalf("expected 1 inserted record, got %d", len(store.inserted))
	}
	r := store.inserted[0]
	if r.Cmd != "ls -la" {
		t.Errorf("cmd: got %q, want %q", r.Cmd, "ls -la")
	}
	if r.Dir != "/tmp" {
		t.Errorf("dir: got %q, want %q", r.Dir, "/tmp")
	}
	if r.Session != "test-123" {
		t.Errorf("session: got %q, want %q", r.Session, "test-123")
	}
	if r.Ts < before || r.Ts > after {
		t.Errorf("ts %d not in range [%d, %d]", r.Ts, before, after)
	}
}

func TestRunWithExplicitTs(t *testing.T) {
	store := &stubStore{}
	args := []string{"--cmd", "echo hi", "--dir", "/home/user", "--session", "sess-1", "--ts", "1700000000"}

	if err := Run(args, store); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if store.inserted[0].Ts != 1700000000 {
		t.Errorf("ts: got %d, want 1700000000", store.inserted[0].Ts)
	}
}

func TestRunMissingCmd(t *testing.T) {
	store := &stubStore{}
	args := []string{"--dir", "/tmp", "--session", "sess-1"}

	if err := Run(args, store); err == nil {
		t.Fatal("expected error for missing --cmd, got nil")
	}
	if len(store.inserted) != 0 {
		t.Error("should not insert when validation fails")
	}
}

func TestRunMissingDir(t *testing.T) {
	store := &stubStore{}
	args := []string{"--cmd", "ls", "--session", "sess-1"}

	if err := Run(args, store); err == nil {
		t.Fatal("expected error for missing --dir, got nil")
	}
}

func TestRunMissingSession(t *testing.T) {
	store := &stubStore{}
	args := []string{"--cmd", "ls", "--dir", "/tmp"}

	if err := Run(args, store); err == nil {
		t.Fatal("expected error for missing --session, got nil")
	}
}

func TestRunInvalidTs(t *testing.T) {
	store := &stubStore{}
	args := []string{"--cmd", "ls", "--dir", "/tmp", "--session", "sess-1", "--ts", "not-a-number"}

	if err := Run(args, store); err == nil {
		t.Fatal("expected error for invalid --ts, got nil")
	}
}

func TestRunStorageError(t *testing.T) {
	store := &stubStore{err: errors.New("disk full")}
	args := []string{"--cmd", "ls", "--dir", "/tmp", "--session", "sess-1"}

	if err := Run(args, store); err == nil {
		t.Fatal("expected error when storage fails, got nil")
	}
}

func TestRunEmptyCmd(t *testing.T) {
	store := &stubStore{}
	args := []string{"--cmd", "", "--dir", "/tmp", "--session", "sess-1"}

	if err := Run(args, store); err == nil {
		t.Fatal("expected error for empty --cmd, got nil")
	}
}

func TestRunEmptyDir(t *testing.T) {
	store := &stubStore{}
	args := []string{"--cmd", "ls", "--dir", "", "--session", "sess-1"}

	if err := Run(args, store); err == nil {
		t.Fatal("expected error for empty --dir, got nil")
	}
}

func TestRunEmptySession(t *testing.T) {
	store := &stubStore{}
	args := []string{"--cmd", "ls", "--dir", "/tmp", "--session", ""}

	if err := Run(args, store); err == nil {
		t.Fatal("expected error for empty --session, got nil")
	}
}
