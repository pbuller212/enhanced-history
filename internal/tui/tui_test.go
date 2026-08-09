package tui

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"enhanced-history/internal/query"
	"enhanced-history/internal/record"
)

// fakeStore is a test double for Querier that records the filters it was
// called with and returns a canned result.
type fakeStore struct {
	rows       []record.Record
	err        error
	lastFilter query.Filter
	calls      int
}

func (f *fakeStore) Query(filt query.Filter) ([]record.Record, error) {
	f.calls++
	f.lastFilter = filt
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

func sampleRows() []record.Record {
	return []record.Record{
		{ID: 1, Ts: 1700000000, Dir: "/home/alice", Session: "s1", Cmd: "ls -la"},
		{ID: 2, Ts: 1700000100, Dir: "/home/alice", Session: "s1", Cmd: "git status"},
		{ID: 3, Ts: 1700000200, Dir: "/tmp", Session: "s2", Cmd: "echo hi"},
	}
}

func TestNewInitializesFocusAndFields(t *testing.T) {
	store := &fakeStore{rows: sampleRows()}
	m := New(store)

	if m.focus != focusSearch {
		t.Fatalf("expected initial focus focusSearch, got %v", m.focus)
	}
	if !m.search.Focused() {
		t.Fatal("expected search input to be focused initially")
	}
	if m.dir.Focused() || m.session.Focused() || m.since.Focused() {
		t.Fatal("expected only search input focused initially")
	}
}

func TestInitTriggersInitialLoad(t *testing.T) {
	store := &fakeStore{rows: sampleRows()}
	m := New(store)

	cmd := m.Init()
	if cmd == nil {
		t.Fatal("expected Init to return a load command")
	}
	msg := cmd()
	fm, ok := msg.(filterMsg)
	if !ok {
		t.Fatalf("expected filterMsg, got %T", msg)
	}
	if len(fm.rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(fm.rows))
	}
}

func TestUpdateFilterMsgPopulatesRowsAndTable(t *testing.T) {
	store := &fakeStore{rows: sampleRows()}
	m := New(store)

	updated, _ := m.Update(filterMsg{rows: sampleRows()})
	mm := updated.(Model)

	if len(mm.rows) != 3 {
		t.Fatalf("expected 3 rows stored, got %d", len(mm.rows))
	}
	if len(mm.table.Rows()) != 3 {
		t.Fatalf("expected 3 table rows, got %d", len(mm.table.Rows()))
	}
	if mm.errMsg != "" {
		t.Fatalf("expected no error, got %q", mm.errMsg)
	}
}

func TestUpdateFilterMsgSurfacesError(t *testing.T) {
	store := &fakeStore{}
	m := New(store)

	updated, _ := m.Update(filterMsg{err: errors.New("boom")})
	mm := updated.(Model)

	if mm.errMsg != "boom" {
		t.Fatalf("expected errMsg %q, got %q", "boom", mm.errMsg)
	}
}

func TestTypingInSearchTriggersLiveReload(t *testing.T) {
	store := &fakeStore{rows: sampleRows()}
	m := New(store)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("git")})
	mm := updated.(Model)

	if mm.search.Value() != "git" {
		t.Fatalf("expected search value %q, got %q", "git", mm.search.Value())
	}
	if cmd == nil {
		t.Fatal("expected a reload command after typing")
	}

	msg := cmd()
	// tea.Batch wraps multiple commands into a BatchMsg; unwrap to find filterMsg.
	found := false
	switch v := msg.(type) {
	case filterMsg:
		found = true
		if store.lastFilter.CmdSubstr != "git" {
			t.Fatalf("expected CmdSubstr %q, got %q", "git", store.lastFilter.CmdSubstr)
		}
	case tea.BatchMsg:
		for _, c := range v {
			if fm, ok := c().(filterMsg); ok {
				found = true
				_ = fm
			}
		}
	}
	if !found {
		t.Fatalf("expected reload command to eventually produce filterMsg, got %T", msg)
	}
}

func TestTabCyclesFocusThroughAllZones(t *testing.T) {
	store := &fakeStore{}
	m := New(store)

	want := []focusZone{focusDir, focusSession, focusSince, focusTable, focusSearch}
	for i, w := range want {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
		m = updated.(Model)
		if m.focus != w {
			t.Fatalf("step %d: expected focus %v, got %v", i, w, m.focus)
		}
	}
}

func TestShiftTabCyclesBackward(t *testing.T) {
	store := &fakeStore{}
	m := New(store)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = updated.(Model)
	if m.focus != focusTable {
		t.Fatalf("expected focus focusTable after shift+tab from focusSearch, got %v", m.focus)
	}
}

func TestQOnlyQuitsWhenTableFocused(t *testing.T) {
	store := &fakeStore{}
	m := New(store)

	// focusSearch: "q" should be typed into the field, not quit.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	mm := updated.(Model)
	if mm.quitting {
		t.Fatal("expected q while search is focused to type, not quit")
	}
	if mm.search.Value() != "q" {
		t.Fatalf("expected search value %q, got %q", "q", mm.search.Value())
	}

	// Move focus to the table, then "q" should quit.
	mm.focus = focusTable
	updated2, cmd := mm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	mm2 := updated2.(Model)
	if !mm2.quitting {
		t.Fatal("expected q while table is focused to quit")
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit command")
	}
}

func TestEscAlwaysQuits(t *testing.T) {
	store := &fakeStore{}
	m := New(store)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	mm := updated.(Model)
	if !mm.quitting {
		t.Fatal("expected esc to quit regardless of focus")
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit command")
	}
}

func TestCopySelectedWritesCommandToClipboard(t *testing.T) {
	store := &fakeStore{rows: sampleRows()}
	m := New(store)
	m.rows = sampleRows()
	m.table.SetRows(rowsToTable(m.rows))
	m.focus = focusTable

	var written string
	m.clipboardWrite = func(s string) error {
		written = s
		return nil
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := updated.(Model)
	if cmd == nil {
		t.Fatal("expected a copy command")
	}
	msg := cmd()
	sm, ok := msg.(statusMsg)
	if !ok {
		t.Fatalf("expected statusMsg, got %T", msg)
	}
	if written != "ls -la" {
		t.Fatalf("expected clipboard to receive %q, got %q", "ls -la", written)
	}
	_ = mm
	_ = sm
}

func TestCopySelectedSurfacesClipboardError(t *testing.T) {
	store := &fakeStore{}
	m := New(store)
	m.rows = sampleRows()
	m.table.SetRows(rowsToTable(m.rows))
	m.focus = focusTable
	m.clipboardWrite = func(s string) error {
		return errors.New("no clipboard available")
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg := cmd()
	fm, ok := msg.(filterMsg)
	if !ok {
		t.Fatalf("expected filterMsg carrying error, got %T", msg)
	}
	if fm.err == nil {
		t.Fatal("expected non-nil error from failed clipboard write")
	}
}

func TestStatusMsgUpdatesStatusAndClearsError(t *testing.T) {
	store := &fakeStore{}
	m := New(store)
	m.errMsg = "stale error"

	updated, _ := m.Update(statusMsg("copied to clipboard"))
	mm := updated.(Model)
	if mm.status != "copied to clipboard" {
		t.Fatalf("expected status set, got %q", mm.status)
	}
	if mm.errMsg != "" {
		t.Fatalf("expected errMsg cleared, got %q", mm.errMsg)
	}
}

func TestBuildFilterCombinesAllFields(t *testing.T) {
	store := &fakeStore{}
	m := New(store)
	m.search.SetValue(" git ")
	m.dir.SetValue(" /home ")
	m.session.SetValue(" s1 ")
	m.since.SetValue("2024-01-01")

	f := m.buildFilter()
	if f.CmdSubstr != "git" {
		t.Errorf("CmdSubstr = %q, want %q", f.CmdSubstr, "git")
	}
	if f.Dir != "/home" {
		t.Errorf("Dir = %q, want %q", f.Dir, "/home")
	}
	if f.Session != "s1" {
		t.Errorf("Session = %q, want %q", f.Session, "s1")
	}
	if f.TsFrom == 0 {
		t.Error("expected TsFrom to be set from --since")
	}
}

func TestBuildFilterIgnoresMalformedSince(t *testing.T) {
	store := &fakeStore{}
	m := New(store)
	m.since.SetValue("not-a-date")

	f := m.buildFilter()
	if f.TsFrom != 0 {
		t.Errorf("expected TsFrom to stay zero for malformed date, got %d", f.TsFrom)
	}
}

func TestParseDate(t *testing.T) {
	tests := []struct {
		in     string
		wantOK bool
	}{
		{"", false},
		{"2024-01-15", true},
		{"not-a-date", false},
		{"2024-13-01", false},
	}
	for _, tt := range tests {
		_, ok := parseDate(tt.in)
		if ok != tt.wantOK {
			t.Errorf("parseDate(%q) ok = %v, want %v", tt.in, ok, tt.wantOK)
		}
	}
}

func TestFormatTs(t *testing.T) {
	got := formatTs(1700000000)
	want := "2023-11-14 22:13"
	if got != want {
		t.Errorf("formatTs = %q, want %q", got, want)
	}
}

func TestTopNOrdersByCountThenKey(t *testing.T) {
	counts := map[string]int{
		"a": 2,
		"b": 5,
		"c": 5,
		"d": 1,
	}
	got := topN(counts, 2)
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}
	if got[0].key != "b" || got[0].count != 5 {
		t.Errorf("entry 0 = %+v, want {b 5}", got[0])
	}
	if got[1].key != "c" || got[1].count != 5 {
		t.Errorf("entry 1 = %+v, want {c 5}", got[1])
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 20); got != "short" {
		t.Errorf("truncate short string changed it: %q", got)
	}
	got := truncate("this is a long string", 10)
	if len([]rune(got)) != 10 {
		t.Errorf("truncate length = %d, want 10", len([]rune(got)))
	}
}

func TestRowsToTable(t *testing.T) {
	rows := sampleRows()
	tr := rowsToTable(rows)
	if len(tr) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(tr))
	}
	if tr[0][0] != "1" || tr[0][3] != "ls -la" {
		t.Errorf("row 0 = %v, want ID=1 Cmd=ls -la", tr[0])
	}
}

func TestViewDoesNotPanic(t *testing.T) {
	store := &fakeStore{rows: sampleRows()}
	m := New(store)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	mm := updated.(Model)
	updated2, _ := mm.Update(filterMsg{rows: sampleRows()})
	mm2 := updated2.(Model)

	out := mm2.View()
	if out == "" {
		t.Fatal("expected non-empty view output")
	}
}

func TestViewEmptyWhenQuitting(t *testing.T) {
	store := &fakeStore{}
	m := New(store)
	m.quitting = true
	if out := m.View(); out != "" {
		t.Errorf("expected empty view when quitting, got %q", out)
	}
}
