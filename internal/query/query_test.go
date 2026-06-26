package query

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"enhanced-history/internal/record"
)

type stubQuerier struct {
	rows []record.Record
	err  error
	got  Filter
}

func (s *stubQuerier) Query(f Filter) ([]record.Record, error) {
	s.got = f
	if s.err != nil {
		return nil, s.err
	}
	return s.rows, nil
}

var sampleRows = []record.Record{
	{ID: 42, Ts: 1000, Dir: "/home/user", Session: "s1", Cmd: "ls -la"},
	{ID: 43, Ts: 2000, Dir: "/home/user", Session: "s1", Cmd: "git status"},
}

func TestRunNoFlags(t *testing.T) {
	store := &stubQuerier{rows: sampleRows}
	var buf bytes.Buffer

	if err := Run([]string{}, store, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "ls -la") || !strings.Contains(out, "git status") {
		t.Errorf("expected both commands in output, got:\n%s", out)
	}
	if store.got != (Filter{}) {
		t.Errorf("expected empty filter, got %+v", store.got)
	}
}

func TestRunOutputFormat(t *testing.T) {
	store := &stubQuerier{rows: []record.Record{{ID: 42, Ts: 1000, Dir: "/", Session: "s", Cmd: "ls -la"}}}
	var buf bytes.Buffer

	if err := Run([]string{}, store, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "   42  ls -la\n"
	if got := buf.String(); got != want {
		t.Errorf("output format: got %q, want %q", got, want)
	}
}

func TestRunSinceFlag(t *testing.T) {
	store := &stubQuerier{rows: sampleRows}
	var buf bytes.Buffer

	if err := Run([]string{"--since", "2026-06-01"}, store, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 2026-06-01 00:00:00 UTC
	wantTs := int64(1780272000)
	if store.got.TsFrom != wantTs {
		t.Errorf("TsFrom: got %d, want %d", store.got.TsFrom, wantTs)
	}
}

func TestRunSinceFlagInvalidDate(t *testing.T) {
	store := &stubQuerier{}
	var buf bytes.Buffer

	if err := Run([]string{"--since", "not-a-date"}, store, &buf); err == nil {
		t.Fatal("expected error for invalid --since date, got nil")
	}
}

func TestRunDirFlag(t *testing.T) {
	store := &stubQuerier{rows: sampleRows}
	var buf bytes.Buffer

	if err := Run([]string{"--dir", "/home/user"}, store, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if store.got.Dir != "/home/user" {
		t.Errorf("Dir: got %q, want %q", store.got.Dir, "/home/user")
	}
}

func TestRunSessionFlag(t *testing.T) {
	store := &stubQuerier{rows: sampleRows}
	var buf bytes.Buffer

	if err := Run([]string{"--session", "abc-123"}, store, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if store.got.Session != "abc-123" {
		t.Errorf("Session: got %q, want %q", store.got.Session, "abc-123")
	}
}

func TestRunSearchFlag(t *testing.T) {
	store := &stubQuerier{rows: sampleRows}
	var buf bytes.Buffer

	if err := Run([]string{"--search", "git"}, store, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if store.got.CmdSubstr != "git" {
		t.Errorf("CmdSubstr: got %q, want %q", store.got.CmdSubstr, "git")
	}
}

func TestRunCombinedFlags(t *testing.T) {
	store := &stubQuerier{rows: sampleRows}
	var buf bytes.Buffer

	args := []string{"--since", "2026-06-01", "--dir", "/home", "--session", "s1", "--search", "ls"}
	if err := Run(args, store, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if store.got.TsFrom == 0 {
		t.Error("TsFrom should be set")
	}
	if store.got.Dir != "/home" {
		t.Errorf("Dir: got %q, want /home", store.got.Dir)
	}
	if store.got.Session != "s1" {
		t.Errorf("Session: got %q, want s1", store.got.Session)
	}
	if store.got.CmdSubstr != "ls" {
		t.Errorf("CmdSubstr: got %q, want ls", store.got.CmdSubstr)
	}
}

func TestRunStoreError(t *testing.T) {
	store := &stubQuerier{err: errors.New("db failure")}
	var buf bytes.Buffer

	if err := Run([]string{}, store, &buf); err == nil {
		t.Fatal("expected error when store fails, got nil")
	}
}

func TestRunEmptyResults(t *testing.T) {
	store := &stubQuerier{rows: []record.Record{}}
	var buf bytes.Buffer

	if err := Run([]string{}, store, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if buf.Len() != 0 {
		t.Errorf("expected empty output for no results, got %q", buf.String())
	}
}

func TestRunFullFlag(t *testing.T) {
	// Ts=0 => 1970-01-01 00:00 UTC
	store := &stubQuerier{rows: []record.Record{
		{ID: 42, Ts: 0, Dir: "/home/user", Session: "abc-123", Cmd: "ls -la"},
	}}
	var buf bytes.Buffer

	if err := Run([]string{"--full"}, store, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := buf.String()
	want := "   42  ls -la   /home/user   1970-01-01 00:00   abc-123\n"
	if got != want {
		t.Errorf("full output format:\n got  %q\n want %q", got, want)
	}
}

func TestRunFullFlagComposesWithFilters(t *testing.T) {
	store := &stubQuerier{rows: sampleRows}
	var buf bytes.Buffer

	args := []string{"--full", "--since", "2026-06-01", "--dir", "/home", "--session", "s1", "--search", "ls"}
	if err := Run(args, store, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All filter fields must still be passed to the store
	if store.got.TsFrom == 0 {
		t.Error("TsFrom should be set")
	}
	if store.got.Dir != "/home" {
		t.Errorf("Dir: got %q, want /home", store.got.Dir)
	}
	if store.got.Session != "s1" {
		t.Errorf("Session: got %q, want s1", store.got.Session)
	}
	if store.got.CmdSubstr != "ls" {
		t.Errorf("CmdSubstr: got %q, want ls", store.got.CmdSubstr)
	}

	// Output must contain all five fields for each row
	out := buf.String()
	for _, r := range sampleRows {
		if !strings.Contains(out, r.Cmd) {
			t.Errorf("expected cmd %q in full output", r.Cmd)
		}
		if !strings.Contains(out, r.Dir) {
			t.Errorf("expected dir %q in full output", r.Dir)
		}
		if !strings.Contains(out, r.Session) {
			t.Errorf("expected session %q in full output", r.Session)
		}
	}
}
