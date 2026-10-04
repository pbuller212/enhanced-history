package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	// doubleClickWindow is the maximum gap between two clicks on the same
	// row for them to count as a double-click.
	doubleClickWindow = 400 * time.Millisecond

	// wheelStep is how many rows one scroll-wheel notch moves the selection.
	wheelStep = 3
)

// rect is a screen region; x/y are zero-based cell coordinates.
type rect struct {
	x, y, w, h int
}

func (r rect) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

// screenRegions holds the clickable regions, computed from the same
// rendered pieces View() uses so layout and hit-testing cannot drift.
type screenRegions struct {
	fields map[focusZone]rect
	table  rect
}

func (m Model) regions() screenRegions {
	titleH := lipgloss.Height(titleStyle.Render("hist tui — history browser"))

	fields := map[focusZone]rect{}
	x := 0
	zones := []struct {
		zone  focusZone
		label string
	}{
		{focusSearch, "Search"},
		{focusDir, "Dir"},
		{focusSession, "Session"},
		{focusSince, "Since"},
	}
	filtersH := 0
	for _, z := range zones {
		panel := m.renderField(z.label, m.fieldInput(z.zone), z.zone)
		w, h := lipgloss.Width(panel), lipgloss.Height(panel)
		fields[z.zone] = rect{x: x, y: titleH, w: w, h: h}
		x += w
		filtersH = max(filtersH, h)
	}

	panel := m.renderTablePanel()
	return screenRegions{
		fields: fields,
		table: rect{
			x: 0, y: titleH + filtersH,
			w: lipgloss.Width(panel), h: lipgloss.Height(panel),
		},
	}
}

// handleMouse implements click-to-focus, click-to-select, wheel scrolling
// and double-click copy. Only left-button presses and the wheel are acted
// on; releases and motion are ignored. Keyboard behaviour is unaffected.
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Action != tea.MouseActionPress {
		return m, nil
	}
	reg := m.regions()

	switch msg.Button {
	case tea.MouseButtonWheelUp:
		if reg.table.contains(msg.X, msg.Y) {
			m.table.MoveUp(wheelStep)
		}
		return m, nil
	case tea.MouseButtonWheelDown:
		if reg.table.contains(msg.X, msg.Y) {
			m.table.MoveDown(wheelStep)
		}
		return m, nil
	case tea.MouseButtonLeft:
	default:
		return m, nil
	}

	for zone, r := range reg.fields {
		if r.contains(msg.X, msg.Y) {
			m.setFocus(zone)
			m.lastClickRow = -1
			return m, nil
		}
	}

	if !reg.table.contains(msg.X, msg.Y) {
		return m, nil
	}
	m.setFocus(focusTable)

	row, ok := m.rowAt(reg.table, msg.Y)
	if !ok {
		m.lastClickRow = -1
		return m, nil
	}
	m.table.SetCursor(row)

	now := m.now()
	double := row == m.lastClickRow && now.Sub(m.lastClickAt) <= doubleClickWindow
	if double {
		m.lastClickRow = -1
		return m, m.copySelected()
	}
	m.lastClickRow, m.lastClickAt = row, now
	return m, nil
}

// rowAt maps a screen y inside the table panel to a record index. Clicks on
// the border, header or below the last row report ok=false.
func (m Model) rowAt(panel rect, y int) (int, bool) {
	// Panel border (1) plus the header line(s) precede the first data row.
	top := panel.y + 1 + lipgloss.Height(m.headerView())
	visible := m.table.Height()
	if y < top || y >= top+visible {
		return 0, false
	}
	idx := m.tableOffset() + (y - top)
	if idx < 0 || idx >= len(m.rows) {
		return 0, false
	}
	return idx, true
}

// headerView renders just the table header, via a row-less clone.
func (m Model) headerView() string {
	t := table.New(table.WithColumns(m.table.Columns()), table.WithHeight(1))
	return strings.SplitN(t.View(), "\n", 2)[0]
}

// tableOffset derives the index of the first visible row. bubbles/table
// does not expose its viewport offset, so the rendered view is the source
// of truth: find the offset whose expected row texts match the visible
// lines. Ambiguity is only possible when a whole page of rows renders
// identically, in which case any choice looks the same on screen.
func (m Model) tableOffset() int {
	n := len(m.rows)
	h := m.table.Height()
	cursor := m.table.Cursor()
	if n == 0 || h <= 0 {
		return 0
	}

	actual := visibleLines(m.table.View())
	if len(actual) > 0 {
		actual = actual[1:] // drop header
	}

	lo := max(0, cursor-h+1)
	hi := min(n, cursor+h)
	ref := table.New(
		table.WithColumns(m.table.Columns()),
		table.WithRows(rowsToTable(m.rows[lo:hi], m.showID)),
		table.WithHeight(hi-lo+1),
	)
	expected := visibleLines(ref.View())[1:]

	for off := lo; off <= cursor; off++ {
		count := min(h, n-off)
		if count > len(actual) || off-lo+count > len(expected) {
			continue
		}
		match := true
		for i := range count {
			if actual[i] != expected[off-lo+i] {
				match = false
				break
			}
		}
		if match {
			return off
		}
	}
	return lo
}

// visibleLines splits a rendered view into ANSI-stripped, right-trimmed lines.
func visibleLines(view string) []string {
	lines := strings.Split(ansi.Strip(view), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return lines
}

func (m Model) fieldInput(zone focusZone) textinput.Model {
	switch zone {
	case focusDir:
		return m.dir
	case focusSession:
		return m.session
	case focusSince:
		return m.since
	}
	return m.search
}
