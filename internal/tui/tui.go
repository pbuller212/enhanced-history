// Package tui implements the interactive terminal UI for `hist tui`: a
// read-only, live-filtering browser over the history database built with
// Bubble Tea (Model/Update/View).
package tui

import (
	"fmt"
	"os"
	"sort"
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
// pressing the corresponding toggle (see Update).
func New(store Querier) Model {
	mkInput := func(placeholder string) textinput.Model {
		ti := textinput.New()
		ti.Placeholder = placeholder
		ti.CharLimit = 256
		ti.Prompt = ""
		return ti
	}

	search := mkInput("substring…")
	search.Focus()
	dir := mkInput("dir prefix…")
	session := mkInput("session id…")
	since := mkInput("YYYY-MM-DD…")

	t := table.New(
		table.WithColumns([]table.Column{
			{Title: "ID", Width: 6},
			{Title: "Time (UTC)", Width: 16},
			{Title: "Dir", Width: 30},
			{Title: "Command", Width: 60},
		}),
		table.WithFocused(false),
	)

	m := Model{
		store:          store,
		search:         search,
		dir:            dir,
		session:        session,
		since:          since,
		table:          t,
		focus:          focusSearch,
		clipboardWrite: clipboard.WriteAll,
		width:          80,
		height:         24,
	}
	return m
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
	f.Dir = strings.TrimSpace(m.dir.Value())
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
		m.table.SetRows(rowsToTable(msg.rows))
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
	m.focus = focusOrder[idx]

	m.search.Blur()
	m.dir.Blur()
	m.session.Blur()
	m.since.Blur()
	m.table.Blur()

	switch m.focus {
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
			return filterMsg{rows: m.rows, err: fmt.Errorf("copy to clipboard: %w", err)}
		}
		return statusMsg("copied to clipboard")
	}
}

// statusMsg briefly reports a successful action (e.g. a clipboard copy).
type statusMsg string

// layout recomputes component widths/heights from the current terminal size.
func (m *Model) layout() {
	tableWidth := m.width - summaryPanelWidth - 6
	if tableWidth < 20 {
		tableWidth = 20
	}
	cols := m.table.Columns()
	if len(cols) == 4 {
		cmdWidth := tableWidth - cols[0].Width - cols[1].Width - cols[2].Width - 8
		if cmdWidth < 10 {
			cmdWidth = 10
		}
		cols[3].Width = cmdWidth
		m.table.SetColumns(cols)
	}
	m.table.SetWidth(tableWidth)
	tableHeight := m.height - 10
	if tableHeight < 5 {
		tableHeight = 5
	}
	m.table.SetHeight(tableHeight)
}

const summaryPanelWidth = 28

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

	summary := panelStyle.Width(summaryPanelWidth).Render(m.renderSummary())

	main := lipgloss.JoinHorizontal(lipgloss.Top, tablePanel, summary)

	status := ""
	if m.errMsg != "" {
		status = errStyle.Render("error: " + m.errMsg)
	} else if m.status != "" {
		status = statusStyle.Render(m.status)
	}

	help := helpStyle.Render("tab: next field  •  enter/y: copy command  •  esc/ctrl+c: quit")

	return lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("hist tui — history browser"),
		filters,
		main,
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

// renderSummary builds the session/directory overview panel from the
// currently-visible rows.
func (m Model) renderSummary() string {
	if len(m.rows) == 0 {
		return "No matches"
	}

	dirCounts := map[string]int{}
	sessionCounts := map[string]int{}
	for _, r := range m.rows {
		dirCounts[r.Dir]++
		sessionCounts[r.Session]++
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%d commands\n", len(m.rows))
	fmt.Fprintf(&b, "%d sessions\n", len(sessionCounts))
	fmt.Fprintf(&b, "%d dirs\n\n", len(dirCounts))

	b.WriteString("Top dirs:\n")
	for _, dc := range topN(dirCounts, 5) {
		fmt.Fprintf(&b, " %3d  %s\n", dc.count, truncate(dc.key, 20))
	}

	return b.String()
}

type countEntry struct {
	key   string
	count int
}

// topN returns the top-N entries of counts sorted by count descending, then
// key ascending for a stable order.
func topN(counts map[string]int, n int) []countEntry {
	entries := make([]countEntry, 0, len(counts))
	for k, v := range counts {
		entries = append(entries, countEntry{key: k, count: v})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].count != entries[j].count {
			return entries[i].count > entries[j].count
		}
		return entries[i].key < entries[j].key
	})
	if len(entries) > n {
		entries = entries[:n]
	}
	return entries
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

// rowsToTable converts history records into table rows for display.
func rowsToTable(rows []record.Record) []table.Row {
	out := make([]table.Row, 0, len(rows))
	for _, r := range rows {
		out = append(out, table.Row{
			fmt.Sprintf("%d", r.ID),
			formatTs(r.Ts),
			r.Dir,
			r.Cmd,
		})
	}
	return out
}

// Run launches the interactive TUI against store. It blocks until the user
// quits.
func Run(store Querier) error {
	if !isInteractive() {
		return fmt.Errorf("hist tui requires an interactive terminal")
	}

	m := New(store)
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
