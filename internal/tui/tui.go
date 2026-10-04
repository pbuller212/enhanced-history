// Package tui implements the interactive terminal UI for `hist tui`: a
// read-only, live-filtering browser over the history database built with
// Bubble Tea (Model/Update/View).
package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"enhanced-history/internal/query"
	"enhanced-history/internal/record"
)

// searchDebounce is how long the filter fields wait after the last keystroke
// before re-querying, so a fast typist doesn't fire one query per character.
const searchDebounce = 300 * time.Millisecond

// Querier is satisfied by db.DB; defined here to avoid an import cycle.
type Querier interface {
	Query(f query.Filter) ([]record.Record, error)
}

// focusZone identifies which input currently receives keystrokes.
type focusZone int

const (
	focusSearch focusZone = iota
	focusDir
	focusSession
	focusSince
	focusTable
)

var focusOrder = []focusZone{focusSearch, focusDir, focusSession, focusSince, focusTable}

var (
	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(0, 1)
	activePanelStyle = panelStyle.BorderForeground(lipgloss.Color("205"))
	titleStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	helpStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	statusStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	errStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
)

// Model is the Bubble Tea model for the history browser.
type Model struct {
	store Querier

	search  textinput.Model
	dir     textinput.Model
	session textinput.Model
	since   textinput.Model
	table   table.Model

	focus focusZone

	// lastField remembers the most recently focused filter field (one of
	// focusSearch/focusDir/focusSession/focusSince) so ctrl+k can jump back
	// to where the user was, rather than always the first field.
	lastField focusZone

	// showID controls whether the list table has an ID column. Off by
	// default; enabled via `hist tui --ids`.
	showID bool

	rows   []record.Record
	status string
	errMsg string

	// filterGen counts filter-field edits. A debounce tick or query result
	// only takes effect if it was issued for the current generation, so a
	// stale (superseded) tick or slow/out-of-order query response never
	// overwrites what's currently on screen.
	filterGen int

	width  int
	height int

	// clipboardWrite is overridable in tests to avoid touching the real
	// system clipboard.
	clipboardWrite func(string) error

	quitting bool
}

// New builds a Model ready to run against store. currentSession and
// currentDir seed nothing by default; the user opts into filtering by
// pressing the corresponding toggle (see Update). showID adds the DB
// record id as the leading table column.
func New(store Querier, showID bool) Model {
	mkInput := func(placeholder string) textinput.Model {
		ti := textinput.New()
		ti.Placeholder = placeholder
		ti.CharLimit = 256
		ti.Prompt = ""
		return ti
	}

	search := mkInput("substring…")
	search.Focus()
	dir := mkInput("dir contains…")
	session := mkInput("session id…")
	since := mkInput("YYYY-MM-DD…")

	t := table.New(
		table.WithColumns(tableColumns(showID)),
		table.WithFocused(false),
	)

	m := Model{
		store:          store,
		showID:         showID,
		search:         search,
		dir:            dir,
		session:        session,
		since:          since,
		table:          t,
		focus:          focusSearch,
		lastField:      focusSearch,
		clipboardWrite: clipboard.WriteAll,
		width:          80,
		height:         24,
	}
	return m
}

// tableColumns returns the list-table column definitions. The ID column is
// included only when showID is set.
func tableColumns(showID bool) []table.Column {
	cols := make([]table.Column, 0, 4)
	if showID {
		cols = append(cols, table.Column{Title: "ID", Width: 6})
	}
	cols = append(cols,
		table.Column{Title: "Time (UTC)", Width: 16},
		table.Column{Title: "Dir", Width: 30},
		table.Column{Title: "Command", Width: 60},
	)
	return cols
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return m.reload()
}

// filterMsg carries the result of a (re-)query triggered by a filter change.
// gen identifies which filterGen the query was issued for, so a response
// that arrives after a newer edit can be recognized as stale and discarded.
type filterMsg struct {
	gen  int
	rows []record.Record
	err  error
}

// debounceMsg fires searchDebounce after a filter-field edit. gen is the
// filterGen at the time the edit was made.
type debounceMsg struct {
	gen int
}

// reload runs the current filter against the store. It is synchronous but
// wrapped in a tea.Cmd per the Elm architecture so Update stays pure.
func (m Model) reload() tea.Cmd {
	f := m.buildFilter()
	gen := m.filterGen
	store := m.store
	return func() tea.Msg {
		rows, err := store.Query(f)
		return filterMsg{gen: gen, rows: rows, err: err}
	}
}

// debounceReload schedules a reload after searchDebounce, tagged with the
// current filterGen. Update only acts on it if no newer edit has happened
// in the meantime.
func (m Model) debounceReload() tea.Cmd {
	gen := m.filterGen
	return tea.Tick(searchDebounce, func(time.Time) tea.Msg {
		return debounceMsg{gen: gen}
	})
}

// buildFilter translates the current input values into a query.Filter.
func (m Model) buildFilter() query.Filter {
	var f query.Filter
	f.CmdSubstr = strings.TrimSpace(m.search.Value())
	f.DirSubstr = strings.TrimSpace(m.dir.Value())
	f.Session = strings.TrimSpace(m.session.Value())

	since := strings.TrimSpace(m.since.Value())
	if t, ok := parseDate(since); ok {
		f.TsFrom = t
	}
	return f
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layout()
		return m, nil

	case filterMsg:
		if msg.gen != m.filterGen {
			// Superseded by a later edit; discard.
			return m, nil
		}
		m.errMsg = ""
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			return m, nil
		}
		m.rows = msg.rows
		m.table.SetRows(rowsToTable(msg.rows, m.showID))
		return m, nil

	case debounceMsg:
		if msg.gen != m.filterGen {
			// A newer edit already scheduled its own debounce; discard.
			return m, nil
		}
		return m, m.reload()

	case statusMsg:
		m.status = string(msg)
		m.errMsg = ""
		return m, nil

	case clipboardErrMsg:
		m.errMsg = msg.err.Error()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "esc":
		m.quitting = true
		return m, tea.Quit
	case "q":
		if m.focus == focusTable {
			m.quitting = true
			return m, tea.Quit
		}
	case "tab":
		m.advanceFocus(1)
		return m, nil
	case "shift+tab":
		m.advanceFocus(-1)
		return m, nil
	case "ctrl+j":
		// Jump straight from any filter field to the table.
		if m.focus != focusTable {
			m.setFocus(focusTable)
		}
		return m, nil
	case "ctrl+k":
		// Jump from the table back to the last-focused filter field.
		if m.focus == focusTable {
			m.setFocus(m.lastField)
		}
		return m, nil
	case "enter", "y":
		if m.focus == focusTable {
			return m, m.copySelected()
		}
	}

	var cmd tea.Cmd
	switch m.focus {
	case focusSearch:
		m.search, cmd = m.search.Update(msg)
	case focusDir:
		m.dir, cmd = m.dir.Update(msg)
	case focusSession:
		m.session, cmd = m.session.Update(msg)
	case focusSince:
		m.since, cmd = m.since.Update(msg)
	case focusTable:
		m.table, cmd = m.table.Update(msg)
		return m, cmd
	}

	// Any edit to a filter field schedules a debounced re-query rather than
	// firing one immediately, so a burst of keystrokes collapses into a
	// single query after the user pauses.
	m.filterGen++
	return m, tea.Batch(cmd, m.debounceReload())
}

// advanceFocus moves focus by delta positions through focusOrder, wrapping
// around, and updates each input's Focus/Blur state accordingly.
func (m *Model) advanceFocus(delta int) {
	idx := 0
	for i, z := range focusOrder {
		if z == m.focus {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(focusOrder)) % len(focusOrder)
	m.setFocus(focusOrder[idx])
}

// setFocus makes zone the focused zone: it blurs every widget, focuses the
// one for zone, and records lastField when zone is a filter field so ctrl+k
// can return to it.
func (m *Model) setFocus(zone focusZone) {
	m.focus = zone

	m.search.Blur()
	m.dir.Blur()
	m.session.Blur()
	m.since.Blur()
	m.table.Blur()

	switch zone {
	case focusSearch:
		m.search.Focus()
	case focusDir:
		m.dir.Focus()
	case focusSession:
		m.session.Focus()
	case focusSince:
		m.since.Focus()
	case focusTable:
		m.table.Focus()
	}

	if zone != focusTable {
		m.lastField = zone
	}
}

// copySelected copies the selected row's command text to the clipboard.
func (m *Model) copySelected() tea.Cmd {
	sel := m.table.Cursor()
	if sel < 0 || sel >= len(m.rows) {
		return nil
	}
	cmdText := m.rows[sel].Cmd
	write := m.clipboardWrite
	return func() tea.Msg {
		if err := write(cmdText); err != nil {
			return clipboardErrMsg{err: fmt.Errorf("copy to clipboard: %w", err)}
		}
		return statusMsg("copied to clipboard")
	}
}

// statusMsg briefly reports a successful action (e.g. a clipboard copy).
type statusMsg string

// clipboardErrMsg reports a clipboard write failure. It is a distinct type
// from filterMsg so it is never subject to the filterGen staleness check —
// a copy failure must always be surfaced, regardless of filter edits that
// happened in the meantime.
type clipboardErrMsg struct{ err error }

// layout recomputes component widths/heights from the current terminal size.
func (m *Model) layout() {
	// The table panel adds a border (2) and horizontal padding (2).
	tableWidth := m.width - 4
	if tableWidth < 20 {
		tableWidth = 20
	}
	cols := m.table.Columns()
	if len(cols) >= 2 {
		// The Command column (always last) flexes to fill the remaining
		// width after the fixed leading columns.
		last := len(cols) - 1
		fixed := 0
		for _, c := range cols[:last] {
			fixed += c.Width
		}
		cmdWidth := tableWidth - fixed - 8
		if cmdWidth < 10 {
			cmdWidth = 10
		}
		cols[last].Width = cmdWidth
		m.table.SetColumns(cols)
	}
	m.table.SetWidth(tableWidth)
	tableHeight := m.height - 10
	if tableHeight < 5 {
		tableHeight = 5
	}
	m.table.SetHeight(tableHeight)
}

// View implements tea.Model.
func (m Model) View() string {
	if m.quitting {
		return ""
	}

	filters := lipgloss.JoinHorizontal(lipgloss.Top,
		m.renderField("Search", m.search, focusSearch),
		m.renderField("Dir", m.dir, focusDir),
		m.renderField("Session", m.session, focusSession),
		m.renderField("Since", m.since, focusSince),
	)

	tableStyle := panelStyle
	if m.focus == focusTable {
		tableStyle = activePanelStyle
	}
	tablePanel := tableStyle.Render(m.table.View())

	status := ""
	if m.errMsg != "" {
		status = errStyle.Render("error: " + m.errMsg)
	} else if m.status != "" {
		status = statusStyle.Render(m.status)
	}

	help := helpStyle.Render("tab: next field  •  ctrl+j/k: jump to table / back to fields  •  enter/y: copy command  •  esc/ctrl+c: quit")

	return lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("hist tui — history browser"),
		filters,
		tablePanel,
		status,
		help,
	)
}

func (m Model) renderField(label string, ti textinput.Model, zone focusZone) string {
	style := panelStyle.Width(20)
	if m.focus == zone {
		style = activePanelStyle.Width(20)
	}
	return style.Render(fmt.Sprintf("%s\n%s", label, ti.View()))
}

// rowsToTable converts history records into table rows for display. The
// leading id cell is included only when showID is set, matching the column
// set from tableColumns.
func rowsToTable(rows []record.Record, showID bool) []table.Row {
	out := make([]table.Row, 0, len(rows))
	for _, r := range rows {
		row := make(table.Row, 0, 4)
		if showID {
			row = append(row, fmt.Sprintf("%d", r.ID))
		}
		row = append(row, formatTs(r.Ts), r.Dir, r.Cmd)
		out = append(out, row)
	}
	return out
}

// Run launches the interactive TUI against store. It blocks until the user
// quits. showID adds the DB record id as the leading table column.
func Run(store Querier, showID bool) error {
	if !isInteractive() {
		return fmt.Errorf("hist tui requires an interactive terminal")
	}

	m := New(store, showID)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("run tui: %w", err)
	}
	return nil
}

// isInteractive reports whether stdout looks like a real terminal. Callers
// should check this before invoking Run, since Bubble Tea requires a tty.
func isInteractive() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
