package query

import (
	"bytes"
	"errors"
	"os"
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

	got := buf.String()
	// Default (terse) output is just the command, with one leading space — no record id.
	if got != " ls -la\n" {
		t.Errorf("output format: got %q, want %q", got, " ls -la\n")
	}
}

func TestRunLeadingSpaceBeforeCommand(t *testing.T) {
	rows := []record.Record{{ID: 42, Ts: 0, Dir: "/d", Session: "s", Cmd: "ls"}}

	tests := []struct {
		name string
		args []string
		want string
	}{
		{"default", []string{}, " ls\n"},
		{"ids", []string{"--ids"}, "42    ls\n"},
		{"full", []string{"--full"}, " ls   /d   1970-01-01 00:00   s\n"},
		{"full+ids", []string{"--full", "--ids"}, "42    ls   /d   1970-01-01 00:00   s\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := Run(tt.args, &stubQuerier{rows: rows}, &buf); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := buf.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRunIdsFlag(t *testing.T) {
	store := &stubQuerier{rows: []record.Record{{ID: 42, Ts: 1000, Dir: "/", Session: "s", Cmd: "ls -la"}}}
	var buf bytes.Buffer

	if err := Run([]string{"--ids"}, store, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := buf.String()
	fields := strings.Fields(strings.TrimRight(got, "\n"))
	if len(fields) < 2 {
		t.Fatalf("--ids output: expected at least 2 fields, got %d in %q", len(fields), got)
	}
	if fields[0] != "42" {
		t.Errorf("--ids output: id field got %q, want %q", fields[0], "42")
	}
	if !strings.Contains(got, "ls -la") {
		t.Errorf("--ids output: cmd not found in %q", got)
	}
}

func TestRunFullWithIdsFlag(t *testing.T) {
	store := &stubQuerier{rows: []record.Record{
		{ID: 42, Ts: 0, Dir: "/home/user", Session: "abc-123", Cmd: "ls -la"},
	}}
	var buf bytes.Buffer

	if err := Run([]string{"--full", "--ids"}, store, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := buf.String()
	fields := strings.Fields(strings.TrimRight(got, "\n"))
	if len(fields) < 5 {
		t.Fatalf("--full --ids output: expected at least 5 fields, got %d in %q", len(fields), got)
	}
	if fields[0] != "42" {
		t.Errorf("--full --ids output: id field got %q, want %q", fields[0], "42")
	}
	for _, want := range []string{"ls -la", "/home/user", "1970-01-01 00:00", "abc-123"} {
		if !strings.Contains(got, want) {
			t.Errorf("--full --ids output: %q not found in %q", want, got)
		}
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
	// tabwriter pads columns; split on runs of whitespace to check fields
	fields := strings.Fields(strings.TrimRight(got, "\n"))
	// expected fields: ls, -la, /home/user, 1970-01-01, 00:00, abc-123
	// "ls -la" becomes two fields after Fields(); check key values
	if len(fields) < 4 {
		t.Fatalf("full output: expected at least 4 fields, got %d in %q", len(fields), got)
	}
	// No record id by default, even with --full.
	if strings.HasPrefix(strings.TrimSpace(got), "42") {
		t.Errorf("full output: unexpected leading id in %q", got)
	}
	if !strings.Contains(got, "ls -la") {
		t.Errorf("full output: cmd not found in %q", got)
	}
	if !strings.Contains(got, "/home/user") {
		t.Errorf("full output: dir not found in %q", got)
	}
	if !strings.Contains(got, "1970-01-01 00:00") {
		t.Errorf("full output: timestamp not found in %q", got)
	}
	if !strings.Contains(got, "abc-123") {
		t.Errorf("full output: session not found in %q", got)
	}
	// Verify column alignment: use two rows so tabwriter must pad the shorter one.
	// The padded output must have at least 3 spaces between columns (minwidth=3).
	store2 := &stubQuerier{rows: []record.Record{
		{ID: 1, Ts: 0, Dir: "/a", Session: "s1", Cmd: "x"},
		{ID: 9999, Ts: 0, Dir: "/longer/path", Session: "s2", Cmd: "longer command here"},
	}}
	var buf2 bytes.Buffer
	if err := Run([]string{"--full", "--ids"}, store2, &buf2); err != nil {
		t.Fatalf("alignment check: unexpected error: %v", err)
	}
	lines := strings.Split(strings.TrimRight(buf2.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("alignment check: expected 2 lines, got %d", len(lines))
	}
	// Find position of "x" cmd and "longer command here" cmd columns — they must start at the same offset
	col0 := strings.Index(lines[0], "x")
	col1 := strings.Index(lines[1], "longer")
	if col0 != col1 {
		t.Errorf("columns not aligned: cmd starts at col %d in line 0 but col %d in line 1\nline0: %q\nline1: %q",
			col0, col1, lines[0], lines[1])
	}
}

func TestRunMeFlag(t *testing.T) {
	t.Setenv("EH_SESSION", "my-session-id")
	store := &stubQuerier{rows: sampleRows}
	var buf bytes.Buffer

	if err := Run([]string{"--me"}, store, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if store.got.Session != "my-session-id" {
		t.Errorf("Session: got %q, want %q", store.got.Session, "my-session-id")
	}
}

func TestRunMeFlagComposesWithFull(t *testing.T) {
	t.Setenv("EH_SESSION", "my-session-id")
	store := &stubQuerier{rows: sampleRows}
	var buf bytes.Buffer

	if err := Run([]string{"--me", "--full"}, store, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if store.got.Session != "my-session-id" {
		t.Errorf("Session: got %q, want %q", store.got.Session, "my-session-id")
	}
	if !strings.Contains(buf.String(), "ls -la") {
		t.Errorf("expected full output to contain command, got: %s", buf.String())
	}
}

func TestRunMeFlagWithSessionConflict(t *testing.T) {
	t.Setenv("EH_SESSION", "my-session-id")
	store := &stubQuerier{}
	var buf bytes.Buffer

	err := Run([]string{"--me", "--session", "other-session"}, store, &buf)
	if err == nil {
		t.Fatal("expected error when --me and --session are both set, got nil")
	}
	if !strings.Contains(err.Error(), "--me") || !strings.Contains(err.Error(), "--session") {
		t.Errorf("error message should mention both flags, got: %v", err)
	}
}

func TestRunMeFlagNoEnvVar(t *testing.T) {
	t.Setenv("EH_SESSION", "")
	store := &stubQuerier{}
	var buf bytes.Buffer

	err := Run([]string{"--me"}, store, &buf)
	if err == nil {
		t.Fatal("expected error when EH_SESSION is unset, got nil")
	}
	if !strings.Contains(err.Error(), "EH_SESSION") {
		t.Errorf("error message should mention EH_SESSION, got: %v", err)
	}
}

func TestRunHereFlag(t *testing.T) {
	store := &stubQuerier{rows: sampleRows}
	var buf bytes.Buffer

	if err := Run([]string{"--here"}, store, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if store.got.Dir != wd {
		t.Errorf("Dir: got %q, want %q", store.got.Dir, wd)
	}
	if !store.got.DirExact {
		t.Error("expected DirExact to be true")
	}
}

func TestRunHereFlagWithDirConflict(t *testing.T) {
	store := &stubQuerier{}
	var buf bytes.Buffer

	err := Run([]string{"--here", "--dir", "/some/path"}, store, &buf)
	if err == nil {
		t.Fatal("expected error when --here and --dir are both set, got nil")
	}
	if !strings.Contains(err.Error(), "--here") || !strings.Contains(err.Error(), "--dir") {
		t.Errorf("error message should mention both flags, got: %v", err)
	}
}

func TestRunHereFlagComposesWithMe(t *testing.T) {
	t.Setenv("EH_SESSION", "my-session-id")
	store := &stubQuerier{rows: sampleRows}
	var buf bytes.Buffer

	if err := Run([]string{"--here", "--me"}, store, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if store.got.Dir != wd {
		t.Errorf("Dir: got %q, want %q", store.got.Dir, wd)
	}
	if !store.got.DirExact {
		t.Error("expected DirExact to be true")
	}
	if store.got.Session != "my-session-id" {
		t.Errorf("Session: got %q, want %q", store.got.Session, "my-session-id")
	}
}

func TestRunHelpFlag(t *testing.T) {
	store := &stubQuerier{}
	var buf bytes.Buffer

	err := Run([]string{"--help"}, store, &buf)
	if err != ErrHelp {
		t.Fatalf("expected ErrHelp, got %v", err)
	}

	out := buf.String()
	for _, want := range []string{"Usage:", "hist", "Flags:", "--full", "--here", "--ids", "--since", "--dir", "--session", "--search", "--version"} {
		if !strings.Contains(out, want) {
			t.Errorf("help output missing %q:\n%s", want, out)
		}
	}
	// Store must not have been queried
	if store.got != (Filter{}) {
		t.Errorf("expected no query on --help, got filter %+v", store.got)
	}
}

func TestRunHelpShortFlag(t *testing.T) {
	store := &stubQuerier{}
	var buf bytes.Buffer

	err := Run([]string{"-h"}, store, &buf)
	if err != ErrHelp {
		t.Fatalf("expected ErrHelp for -h, got %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Usage:") {
		t.Errorf("help output missing 'Usage:':\n%s", out)
	}
}

func TestRunHelpOutputGoesToOut(t *testing.T) {
	store := &stubQuerier{}
	var out bytes.Buffer
	var discarded bytes.Buffer // simulate stderr being separate
	_ = discarded

	err := Run([]string{"--help"}, store, &out)
	if err != ErrHelp {
		t.Fatalf("expected ErrHelp, got %v", err)
	}
	if out.Len() == 0 {
		t.Error("help output should be written to out, not discarded")
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
