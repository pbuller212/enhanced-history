package tui

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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
	m := New(store, false)

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
	m := New(store, false)

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
	m := New(store, false)

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
	m := New(store, false)

	updated, _ := m.Update(filterMsg{err: errors.New("boom")})
	mm := updated.(Model)

	if mm.errMsg != "boom" {
		t.Fatalf("expected errMsg %q, got %q", "boom", mm.errMsg)
	}
}

// findDebounceMsg unwraps a (possibly tea.Batch-wrapped) command looking for
// a debounceMsg, running any nested commands to get there.
func findDebounceMsg(t *testing.T, cmd tea.Cmd) debounceMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a non-nil command")
	}
	msg := cmd()
	switch v := msg.(type) {
	case debounceMsg:
		return v
	case tea.BatchMsg:
		for _, c := range v {
			if dm, ok := c().(debounceMsg); ok {
				return dm
			}
		}
	}
	t.Fatalf("expected debounceMsg, got %T", msg)
	return debounceMsg{}
}

func TestTypingInSearchSchedulesDebounceNotImmediateReload(t *testing.T) {
	store := &fakeStore{rows: sampleRows()}
	m := New(store, false)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("git")})
	mm := updated.(Model)

	if mm.search.Value() != "git" {
		t.Fatalf("expected search value %q, got %q", "git", mm.search.Value())
	}
	// The keystroke itself must not have queried the store yet — only the
	// debounce timer fires the query, after searchDebounce elapses.
	if store.calls != 0 {
		t.Fatalf("expected no query on keystroke, got %d calls", store.calls)
	}

	dm := findDebounceMsg(t, cmd)
	if dm.gen != mm.filterGen {
		t.Fatalf("expected debounceMsg.gen %d, got %d", mm.filterGen, dm.gen)
	}
}

func TestDebounceFiresReloadWhenGenerationStillCurrent(t *testing.T) {
	store := &fakeStore{rows: sampleRows()}
	m := New(store, false)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("git")})
	mm := updated.(Model)

	updated2, cmd := mm.Update(debounceMsg{gen: mm.filterGen})
	mm2 := updated2.(Model)
	if cmd == nil {
		t.Fatal("expected debounceMsg to trigger a reload command")
	}
	msg := cmd()
	fm, ok := msg.(filterMsg)
	if !ok {
		t.Fatalf("expected filterMsg, got %T", msg)
	}
	if store.lastFilter.CmdSubstr != "git" {
		t.Fatalf("expected CmdSubstr %q, got %q", "git", store.lastFilter.CmdSubstr)
	}
	_ = mm2
	_ = fm
}

func TestStaleDebounceIsDiscarded(t *testing.T) {
	store := &fakeStore{rows: sampleRows()}
	m := New(store, false)

	// Simulate an edit from an earlier generation arriving after a newer
	// edit already bumped filterGen (e.g. the user kept typing).
	m.filterGen = 5
	updated, cmd := m.Update(debounceMsg{gen: 4})
	mm := updated.(Model)
	if cmd != nil {
		t.Fatalf("expected stale debounceMsg to be discarded, got a command")
	}
	if store.calls != 0 {
		t.Fatalf("expected no query from stale debounceMsg, got %d calls", store.calls)
	}
	_ = mm
}

func TestStaleFilterMsgIsDiscarded(t *testing.T) {
	store := &fakeStore{}
	m := New(store, false)
	m.filterGen = 2
	m.rows = sampleRows()
	m.table.SetRows(rowsToTable(m.rows, false))

	// A filterMsg tagged with an older generation must not overwrite the
	// current (newer) rows, since it was superseded before it arrived.
	staleRows := []record.Record{{ID: 99, Ts: 1, Dir: "/stale", Session: "x", Cmd: "stale"}}
	updated, _ := m.Update(filterMsg{gen: 1, rows: staleRows})
	mm := updated.(Model)

	if len(mm.rows) != 3 {
		t.Fatalf("expected stale filterMsg to be ignored, rows changed to len %d", len(mm.rows))
	}
}

func TestDebounceUsesConfiguredDelay(t *testing.T) {
	if searchDebounce != 300*time.Millisecond {
		t.Fatalf("expected 300ms debounce, got %v", searchDebounce)
	}
}

func TestTabCyclesFocusThroughAllZones(t *testing.T) {
	store := &fakeStore{}
	m := New(store, false)

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
	m := New(store, false)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = updated.(Model)
	if m.focus != focusTable {
		t.Fatalf("expected focus focusTable after shift+tab from focusSearch, got %v", m.focus)
	}
}

func TestCtrlJJumpsFromFieldToTable(t *testing.T) {
	store := &fakeStore{}
	m := New(store, false)

	// Move focus onto a field other than the first.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if m.focus != focusDir {
		t.Fatalf("setup: expected focusDir, got %v", m.focus)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	m = updated.(Model)
	if m.focus != focusTable {
		t.Fatalf("expected ctrl+j to jump to focusTable, got %v", m.focus)
	}
	if !m.table.Focused() {
		t.Fatal("expected table widget to be focused after ctrl+j")
	}
	if m.dir.Focused() {
		t.Fatal("expected dir input to be blurred after ctrl+j")
	}
}

func TestCtrlKRestoresLastFocusedField(t *testing.T) {
	store := &fakeStore{}
	m := New(store, false)

	// Tab to focusSession.
	for range 2 {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
		m = updated.(Model)
	}
	if m.focus != focusSession {
		t.Fatalf("setup: expected focusSession, got %v", m.focus)
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	m = updated.(Model)
	if m.focus != focusTable {
		t.Fatalf("expected focusTable after ctrl+j, got %v", m.focus)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	m = updated.(Model)
	if m.focus != focusSession {
		t.Fatalf("expected ctrl+k to restore focusSession, got %v", m.focus)
	}
	if !m.session.Focused() {
		t.Fatal("expected session input to be focused after ctrl+k")
	}
}

func TestCtrlKDefaultsToSearchWhenNoFieldTouched(t *testing.T) {
	store := &fakeStore{}
	m := New(store, false)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	m = updated.(Model)

	if m.focus != focusSearch {
		t.Fatalf("expected ctrl+k to default to focusSearch, got %v", m.focus)
	}
}

func TestCtrlJNoOpOnTable(t *testing.T) {
	store := &fakeStore{}
	m := New(store, false)
	m.focus = focusTable
	m.table.Focus()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	m = updated.(Model)
	if m.focus != focusTable {
		t.Fatalf("expected ctrl+j on table to be a no-op, got %v", m.focus)
	}
}

func TestCtrlKNoOpOnField(t *testing.T) {
	store := &fakeStore{}
	m := New(store, false)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	m = updated.(Model)
	if m.focus != focusSearch {
		t.Fatalf("expected ctrl+k on a field to be a no-op, got %v", m.focus)
	}
}

func TestCtrlJDoesNotBreakTabCycling(t *testing.T) {
	store := &fakeStore{}
	m := New(store, false)

	// Jump to the table and back, then verify Tab still cycles normally.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	m = updated.(Model)

	want := []focusZone{focusDir, focusSession, focusSince, focusTable, focusSearch}
	for i, w := range want {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
		m = updated.(Model)
		if m.focus != w {
			t.Fatalf("step %d: expected focus %v, got %v", i, w, m.focus)
		}
	}
}

func TestQOnlyQuitsWhenTableFocused(t *testing.T) {
	store := &fakeStore{}
	m := New(store, false)

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
	m := New(store, false)

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
	m := New(store, false)
	m.rows = sampleRows()
	m.table.SetRows(rowsToTable(m.rows, false))
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
	m := New(store, false)
	m.rows = sampleRows()
	m.table.SetRows(rowsToTable(m.rows, false))
	m.focus = focusTable
	// Bump filterGen past its zero value, as a prior filter-field edit would.
	// A clipboard error must surface regardless of filterGen, since it is no
	// longer carried by filterMsg (which the gen check can discard as stale).
	m.filterGen = 1
	m.clipboardWrite = func(s string) error {
		return errors.New("no clipboard available")
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg := cmd()
	em, ok := msg.(clipboardErrMsg)
	if !ok {
		t.Fatalf("expected clipboardErrMsg carrying error, got %T", msg)
	}
	if em.err == nil {
		t.Fatal("expected non-nil error from failed clipboard write")
	}

	updated, _ := m.Update(em)
	mm := updated.(Model)
	if mm.errMsg == "" {
		t.Fatal("expected Update to surface the clipboard error in errMsg")
	}
}

func TestStatusMsgUpdatesStatusAndClearsError(t *testing.T) {
	store := &fakeStore{}
	m := New(store, false)
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
	m := New(store, false)
	m.search.SetValue(" git ")
	m.dir.SetValue(" /home ")
	m.session.SetValue(" s1 ")
	m.since.SetValue("2024-01-01")

	f := m.buildFilter()
	if f.CmdSubstr != "git" {
		t.Errorf("CmdSubstr = %q, want %q", f.CmdSubstr, "git")
	}
	if f.DirSubstr != "/home" {
		t.Errorf("DirSubstr = %q, want %q", f.DirSubstr, "/home")
	}
	if f.Dir != "" {
		t.Errorf("Dir = %q, want empty (TUI uses substring match)", f.Dir)
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
	m := New(store, false)
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

func TestRowsToTable(t *testing.T) {
	rows := sampleRows()

	// Default: no id column — cells are time, dir, cmd.
	tr := rowsToTable(rows, false)
	if len(tr) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(tr))
	}
	if len(tr[0]) != 3 {
		t.Fatalf("expected 3 cells without id, got %d: %v", len(tr[0]), tr[0])
	}
	if tr[0][2] != "ls -la" {
		t.Errorf("row 0 = %v, want Cmd=ls -la in last cell", tr[0])
	}
	if tr[0][0] == "1" {
		t.Errorf("row 0 unexpectedly leads with id: %v", tr[0])
	}

	// With showID: leading cell is the record id.
	trID := rowsToTable(rows, true)
	if len(trID[0]) != 4 {
		t.Fatalf("expected 4 cells with id, got %d: %v", len(trID[0]), trID[0])
	}
	if trID[0][0] != "1" || trID[0][3] != "ls -la" {
		t.Errorf("row 0 = %v, want ID=1 Cmd=ls -la", trID[0])
	}
}

func TestTableColumnsRespectShowID(t *testing.T) {
	if got := tableColumns(false); len(got) != 3 || got[0].Title != "Time (UTC)" {
		t.Errorf("tableColumns(false) = %v, want 3 columns starting with Time (UTC)", got)
	}
	if got := tableColumns(true); len(got) != 4 || got[0].Title != "ID" {
		t.Errorf("tableColumns(true) = %v, want 4 columns starting with ID", got)
	}
}

func TestNewOmitsIDColumnByDefault(t *testing.T) {
	store := &fakeStore{rows: sampleRows()}

	m := New(store, false)
	for _, c := range m.table.Columns() {
		if c.Title == "ID" {
			t.Fatalf("expected no ID column by default, got %v", m.table.Columns())
		}
	}

	mID := New(store, true)
	if mID.table.Columns()[0].Title != "ID" {
		t.Fatalf("expected leading ID column with showID, got %v", mID.table.Columns())
	}
}

func TestViewDoesNotPanic(t *testing.T) {
	store := &fakeStore{rows: sampleRows()}
	m := New(store, false)
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
	m := New(store, false)
	m.quitting = true
	if out := m.View(); out != "" {
		t.Errorf("expected empty view when quitting, got %q", out)
	}
}

func sizedModel(t *testing.T, width, height int) Model {
	t.Helper()
	m := New(&fakeStore{rows: sampleRows()}, false)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	updated2, _ := updated.(Model).Update(filterMsg{rows: sampleRows()})
	return updated2.(Model)
}

func TestLayoutTableFillsTerminalWidth(t *testing.T) {
	for _, width := range []int{120, 200} {
		m := sizedModel(t, width, 30)

		// The table panel adds a border (2) and horizontal padding (2).
		if got, want := m.table.Width(), width-4; got != want {
			t.Errorf("width %d: table width = %d, want %d", width, got, want)
		}
		for _, line := range strings.Split(m.View(), "\n") {
			if w := lipgloss.Width(line); w > width {
				t.Errorf("width %d: rendered line is %d wide: %q", width, w, line)
			}
		}
	}
}

func TestLayoutCmdColumnAbsorbsExtraWidth(t *testing.T) {
	narrow := sizedModel(t, 100, 30)
	wide := sizedModel(t, 140, 30)

	nCols, wCols := narrow.table.Columns(), wide.table.Columns()
	last := len(nCols) - 1
	if got, want := wCols[last].Width-nCols[last].Width, 40; got != want {
		t.Errorf("Cmd column grew by %d, want %d", got, want)
	}
	for i := 0; i < last; i++ {
		if nCols[i].Width != wCols[i].Width {
			t.Errorf("column %d width changed: %d -> %d", i, nCols[i].Width, wCols[i].Width)
		}
	}
}

func TestViewHasNoSummaryPanel(t *testing.T) {
	out := sizedModel(t, 120, 30).View()
	for _, s := range []string{"Top dirs", "commands", "sessions", "dirs"} {
		if strings.Contains(out, s) {
			t.Errorf("view contains summary text %q", s)
		}
	}
}

// --- mouse ---

func manyRows(n int) []record.Record {
	rows := make([]record.Record, n)
	for i := range rows {
		rows[i] = record.Record{
			ID: int64(i + 1), Ts: 1700000000 + int64(i)*60,
			Dir: "/tmp", Cmd: "cmd-" + strings.Repeat("x", i%7) + string(rune('a'+i%26)) + strconv.Itoa(i),
		}
	}
	return rows
}

func mouseModel(t *testing.T, n int) Model {
	t.Helper()
	m := New(&fakeStore{}, false)
	m.rows = manyRows(n)
	m.table.SetRows(rowsToTable(m.rows, false))
	m.clipboardWrite = func(string) error { return nil }
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	return next.(Model)
}

func click(m Model, x, y int) Model {
	next, _ := m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	return next.(Model)
}

func rowY(m Model, visibleLine int) int {
	return m.regions().table.y + 2 + visibleLine
}

func TestClickFilterFieldsFocusesAndTracksLastField(t *testing.T) {
	m := mouseModel(t, 5)
	reg := m.regions()
	for _, zone := range []focusZone{focusSince, focusDir, focusSession, focusSearch} {
		r := reg.fields[zone]
		m = click(m, r.x+2, r.y+1)
		if m.focus != zone || m.lastField != zone {
			t.Fatalf("click on %v: focus=%v lastField=%v", zone, m.focus, m.lastField)
		}
	}
	m = click(m, reg.fields[focusDir].x+1, reg.fields[focusDir].y)
	if !m.dir.Focused() || m.search.Focused() {
		t.Fatal("expected only dir input focused")
	}
}

func TestClickTableFocusesAndSelectsRow(t *testing.T) {
	m := mouseModel(t, 5)
	m = click(m, 5, rowY(m, 2))
	if m.focus != focusTable || m.table.Cursor() != 2 {
		t.Fatalf("focus=%v cursor=%d", m.focus, m.table.Cursor())
	}
	// ctrl+k returns to the last filter field.
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	if next.(Model).focus != focusSearch {
		t.Fatal("ctrl+k should return to search")
	}
}

func TestClickTableAfterScrollingPastOnePage(t *testing.T) {
	m := mouseModel(t, 100)
	m.setFocus(focusTable)
	m.table.MoveDown(60)
	off := m.tableOffset()
	if off == 0 || m.table.Cursor() < off || m.table.Cursor() >= off+m.table.Height() {
		t.Fatalf("bad derived offset %d for cursor %d", off, m.table.Cursor())
	}
	m = click(m, 5, rowY(m, 1))
	if m.table.Cursor() != off+1 {
		t.Fatalf("cursor=%d want %d", m.table.Cursor(), off+1)
	}
}

func TestClickHeaderBorderAndBelowLastRowOnlyFocus(t *testing.T) {
	m := mouseModel(t, 2)
	m.table.SetCursor(1)
	reg := m.regions()
	for _, y := range []int{reg.table.y, reg.table.y + 1, rowY(m, 5)} {
		m = click(m, 5, y)
		if m.focus != focusTable || m.table.Cursor() != 1 {
			t.Fatalf("y=%d focus=%v cursor=%d", y, m.focus, m.table.Cursor())
		}
		m.setFocus(focusSearch)
	}
}

func TestWheelMovesCursorWithoutChangingFocus(t *testing.T) {
	m := mouseModel(t, 20)
	y := rowY(m, 0)
	next, _ := m.Update(tea.MouseMsg{X: 5, Y: y, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	m = next.(Model)
	if m.table.Cursor() != wheelStep || m.focus != focusSearch {
		t.Fatalf("cursor=%d focus=%v", m.table.Cursor(), m.focus)
	}
	next, _ = m.Update(tea.MouseMsg{X: 5, Y: y, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if next.(Model).table.Cursor() != 0 {
		t.Fatal("wheel up should move back")
	}
}

func TestDoubleClickCopies(t *testing.T) {
	m := mouseModel(t, 5)
	var copied string
	m.clipboardWrite = func(s string) error { copied = s; return nil }
	clock := time.Unix(1000, 0)
	m.now = func() time.Time { return clock }

	run := func(m Model, y int) (Model, tea.Cmd) {
		next, cmd := m.Update(tea.MouseMsg{X: 5, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
		return next.(Model), cmd
	}

	m, cmd := run(m, rowY(m, 1))
	if cmd != nil {
		t.Fatal("single click must not copy")
	}
	clock = clock.Add(100 * time.Millisecond)
	_, cmd = run(m, rowY(m, 1))
	if cmd == nil {
		t.Fatal("double click should copy")
	}
	cmd()
	if copied != m.rows[1].Cmd {
		t.Fatalf("copied %q", copied)
	}
}

func TestSlowOrDifferentRowSecondClickDoesNotCopy(t *testing.T) {
	m := mouseModel(t, 5)
	clock := time.Unix(1000, 0)
	m.now = func() time.Time { return clock }
	m = click(m, 5, rowY(m, 1))

	next, cmd := m.Update(tea.MouseMsg{X: 5, Y: rowY(m, 2), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if cmd != nil {
		t.Fatal("different row must not copy")
	}
	m = click(next.(Model), 5, rowY(m, 1))
	clock = clock.Add(time.Second)
	_, cmd = m.Update(tea.MouseMsg{X: 5, Y: rowY(m, 1), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if cmd != nil {
		t.Fatal("slow second click must not copy")
	}
}

func TestMouseReleaseMotionAndOtherButtonsIgnored(t *testing.T) {
	m := mouseModel(t, 5)
	y := rowY(m, 1)
	for _, ev := range []tea.MouseMsg{
		{X: 5, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease},
		{X: 5, Y: y, Button: tea.MouseButtonNone, Action: tea.MouseActionMotion},
		{X: 5, Y: y, Button: tea.MouseButtonRight, Action: tea.MouseActionPress},
	} {
		next, cmd := m.Update(ev)
		if cmd != nil || next.(Model).focus != focusSearch {
			t.Fatalf("event %+v should be ignored", ev)
		}
	}
}
