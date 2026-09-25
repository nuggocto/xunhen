package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/nuggocto/xunhen/internal/diff"
	"github.com/nuggocto/xunhen/internal/termtext"
)

// Terminal sizes below these show a short notice instead of the browser.
// Keys keep working, so the browser can still be quit or resized back.
const (
	minWidth  = 20
	minHeight = 4
)

// splitWidth is the narrowest terminal that shows the tree and the content
// side by side. Narrower terminals show the focused pane alone.
const splitWidth = 80

// Every row outside the body is one of these: a title row above, and a
// status row and a key row below.
const chromeRows = 3

// SGR attributes. Reverse video and bold are not colors, so they stay under
// NO_COLOR; the colors themselves do not.
const (
	sgrReverse = "7"
	sgrBold    = "1"
	sgrDim     = "2"
	sgrRed     = "31"
	sgrGreen   = "32"
	sgrCyan    = "36"
)

func (m *model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m *model) render() string {
	if m.width < minWidth || m.height < minHeight {
		return m.renderTiny()
	}

	rows := make([]string, 0, m.height)
	rows = append(rows, m.style(sgrBold, m.pad(m.title(), m.width)))

	body := m.bodyRows()
	switch {
	case m.overlay == helpOverlay || m.overlay == exportOverlay:
		rows = append(rows, m.renderOverlay(body)...)
	case m.width >= splitWidth:
		tree, content := m.renderTree(body, m.treeWidth()), m.renderContent(body, m.contentWidth())
		for i := range body {
			rows = append(rows, tree[i]+m.style(sgrDim, "|")+content[i])
		}
	case m.focus == treePane:
		rows = append(rows, m.renderTree(body, m.width)...)
	default:
		rows = append(rows, m.renderContent(body, m.width)...)
	}

	rows = append(rows, m.pad(m.status(), m.width))
	rows = append(rows, m.style(sgrDim, m.pad(m.keys(), m.width)))

	return strings.Join(rows, "\n")
}

func (m *model) renderTiny() string {
	lines := []string{"xunhen: enlarge the window", "q quits"}
	rows := make([]string, 0, m.height)
	for i := range min(m.height, len(lines)) {
		rows = append(rows, m.pad(lines[i], m.width))
	}

	return strings.Join(rows, "\n")
}

// bodyRows is the number of rows between the title and the status rows.
func (m *model) bodyRows() int {
	return max(m.height-chromeRows, 0)
}

// contentRows is the number of scrolling rows under the content header.
func (m *model) contentRows() int {
	return max(m.bodyRows()-1, 0)
}

func (m *model) treeWidth() int {
	return min(max(m.width/3, 28), 44)
}

// contentWidth is the width of the content pane, whichever layout is used.
func (m *model) contentWidth() int {
	if m.width >= splitWidth {
		return m.width - m.treeWidth() - 1
	}

	return m.width
}

// gutterWidth is the width drawn before each content line: a line number
// and a space in a preview, an operation sign in a comparison.
func (m *model) gutterWidth() int {
	if m.mode == compareMode {
		return 1
	}

	return len(strconv.Itoa(max(m.contentLen(), 1))) + 1
}

// textWidth is the number of cells of line text a content row shows.
func (m *model) textWidth() int {
	return max(m.contentWidth()-m.gutterWidth(), 0)
}

func (m *model) title() string {
	if m.session == nil {
		return " xunhen browse"
	}

	labels := m.session.labels
	base := "base"
	if labels.Source {
		base = "source"
	}

	return fmt.Sprintf(" xunhen browse  %s  %s %s  reference node %d",
		termtext.Escape(labels.Undo), base, termtext.Escape(labels.Base), m.session.tree.ids[m.session.tree.reference])
}

// renderTree draws the visible tree rows from the top row down. It visits
// only the rows it draws.
func (m *model) renderTree(rows, width int) []string {
	out := make([]string, 0, rows)
	if m.session == nil {
		for range rows {
			out = append(out, m.pad("", width))
		}
		return out
	}

	t := m.session.tree
	p, more := m.top, true
	for range rows {
		if !more {
			out = append(out, m.pad("", width))
			continue
		}

		line := m.pad(m.treeRow(p, width), width)
		if p == m.selected {
			attribute := sgrBold
			if m.focus == treePane || m.width < splitWidth {
				attribute = sgrReverse
			}
			line = m.style(attribute, line)
		}
		out = append(out, line)
		p, more = t.next(m.folds, p)
	}

	return out
}

// treeRow labels one node: the cursor, the indentation of its branch, a mark
// where an alternate branch starts, the ID, the recorded time, and tags.
// Indentation stops at a third of the pane; deeper rows show their level
// instead, so the ID and time stay on screen however deep the history is.
func (m *model) treeRow(p int32, width int) string {
	t := m.session.tree
	var b strings.Builder

	if p == m.selected {
		b.WriteString("> ")
	} else {
		b.WriteString("  ")
	}

	level, limit := int(t.level[p]), max((width/3)/2, 1)
	if level > limit {
		fmt.Fprintf(&b, "%-*s", limit*2, "<"+strconv.Itoa(level)+">")
	} else {
		b.WriteString(strings.Repeat("  ", level))
	}
	if t.branches(p) {
		b.WriteString("`- ")
	}

	fmt.Fprintf(&b, "%d", t.ids[p])
	if m.folds[p] && t.hasChildren(p) {
		fmt.Fprintf(&b, " [+%d]", t.end[p]-p-1)
	}

	info, err := m.session.history.Info(m.ref(p))
	switch {
	case err != nil:
	case !info.HasEvent:
		b.WriteString("  root")
	default:
		b.WriteString("  " + time.Unix(info.Time, 0).Format("01-02 15:04"))
	}

	if p == t.reference {
		b.WriteString(" [ref]")
	}
	if p == m.origin {
		b.WriteString(" [from]")
	}

	return b.String()
}

func (m *model) renderContent(rows, width int) []string {
	out := make([]string, 0, rows)
	if rows == 0 {
		return out
	}
	out = append(out, m.style(sgrBold, m.pad(m.contentHeader(), width)))

	lines := m.contentLines(rows-1, width)
	for i := range rows - 1 {
		if i < len(lines) {
			out = append(out, lines[i])
		} else {
			out = append(out, m.pad("", width))
		}
	}

	return out
}

// current reports whether the shown result answers the current selection.
func (m *model) current() bool {
	t := m.session.tree
	switch {
	case m.mode == previewMode && m.doc != nil:
		return m.doc.id == t.ids[m.selected]
	case m.mode == compareMode && m.cmp != nil:
		return m.cmp.from.id == t.ids[m.origin] && m.cmp.to.id == t.ids[m.selected]
	}

	return false
}

// contentHeader names what the pane shows. While the request for the current
// selection is pending, it names the pending state and says which earlier
// result is still on screen, so an old state is never shown as the new one.
func (m *model) contentHeader() string {
	switch {
	case m.session == nil:
		return "Loading the history..."
	case m.loading:
		return "Reloading the inputs..."
	}

	t := m.session.tree
	target := t.ids[m.selected]
	var subject string
	if m.mode == previewMode {
		subject = fmt.Sprintf("Node %d", target)
	} else {
		subject = fmt.Sprintf("Node %d -> node %d", t.ids[m.origin], target)
	}

	switch {
	case m.failure != nil:
		return subject + ": failed: " + termtext.Escape(firstLine(m.failure))
	case m.current() && m.mode == previewMode:
		return subject + m.describe(m.selected) + ", " + count(m.doc.len(), "line")
	case m.current():
		if len(m.cmp.hunks) == 0 {
			return subject + ": identical states"
		}
		return subject + ": " + count(len(m.cmp.hunks), "hunk")
	case m.pending && m.doc != nil && m.mode == previewMode:
		return subject + fmt.Sprintf(": reconstructing; showing node %d until it is ready", m.doc.id)
	case m.pending && m.cmp != nil && m.mode == compareMode:
		return subject + fmt.Sprintf(": comparing; showing node %d -> node %d until it is ready", m.cmp.from.id, m.cmp.to.id)
	case m.pending && m.mode == previewMode:
		return subject + ": reconstructing..."
	case m.pending:
		return subject + ": comparing..."
	}

	return subject
}

// describe gives a node's recorded metadata, marking what is unknown.
func (m *model) describe(p int32) string {
	info, err := m.session.history.Info(m.ref(p))
	if err != nil {
		return ""
	}
	if !info.HasEvent {
		return ", retained root, time unknown"
	}

	return ", recorded " + time.Unix(info.Time, 0).Format("2006-01-02 15:04:05 MST")
}

// contentLines draws the visible content rows. It reads only those rows,
// each through its column index when it has one.
func (m *model) contentLines(rows, width int) []string {
	if m.session == nil || m.loading || m.failure != nil {
		return nil
	}

	text := max(width-m.gutterWidth(), 0)
	var out []string

	switch {
	case m.mode == previewMode && m.doc != nil:
		digits := m.gutterWidth() - 1
		for i := m.scrollY; i < m.doc.len() && len(out) < rows; i++ {
			line, columns := m.doc.line(i)
			clipped, cells := termtext.Clip(line, columns, m.method, m.scrollX, text)
			number := m.style(sgrDim, fmt.Sprintf("%*d ", digits, i+1))
			out = append(out, number+clipped+strings.Repeat(" ", max(text-cells, 0)))
		}

	case m.mode == compareMode && m.cmp != nil && m.cmp.rows == 0:
		out = append(out, m.pad("The two states are identical.", width))

	case m.mode == compareMode && m.cmp != nil:
		for r := m.scrollY; r < m.cmp.rows && len(out) < rows; r++ {
			out = append(out, m.comparisonRow(r, width, text))
		}
	}

	return out
}

func (m *model) comparisonRow(r, width, text int) string {
	hunk, index := m.cmp.row(r)
	h := m.cmp.hunks[hunk]
	if index < 0 {
		header := fmt.Sprintf("@@ -%s +%s @@", hunkRange(h.LeftStart, h.LeftCount), hunkRange(h.RightStart, h.RightCount))
		return m.color(sgrCyan, m.pad(header, width))
	}

	line, _ := h.Line(index)
	source, number := m.cmp.from, line.Left
	if line.Op == diff.Insert {
		source, number = m.cmp.to, line.Right
	}
	body, columns := source.line(number)
	clipped, cells := termtext.Clip(body, columns, m.method, m.scrollX, text)
	row := " -+"[line.Op:line.Op+1] + clipped + strings.Repeat(" ", max(text-cells, 0))

	switch line.Op {
	case diff.Delete:
		return m.color(sgrRed, row)
	case diff.Insert:
		return m.color(sgrGreen, row)
	}
	return row
}

// hunkRange follows the unified header format that xunhen diff prints.
func hunkRange(start, count int) string {
	switch count {
	case 0:
		return fmt.Sprintf("%d,0", start)
	case 1:
		return strconv.Itoa(start + 1)
	default:
		return fmt.Sprintf("%d,%d", start+1, count)
	}
}

// widestContent measures the visible content rows, to bound sideways
// scrolling. Only drawn rows are measured, each through its index.
func (m *model) widestContent() int {
	widest := 0
	for r := m.scrollY; r < m.scrollY+m.contentRows() && r < m.contentLen(); r++ {
		var line string
		var index *termtext.Columns
		switch {
		case m.mode == previewMode:
			line, index = m.doc.line(r)
		default:
			hunk, i := m.cmp.row(r)
			if i < 0 {
				continue
			}
			l, _ := m.cmp.hunks[hunk].Line(i)
			if l.Op == diff.Insert {
				line, index = m.cmp.to.line(l.Right)
			} else {
				line, index = m.cmp.from.line(l.Left)
			}
		}
		widest = max(widest, termtext.Width(line, index, m.method))
	}

	return widest
}

func (m *model) renderOverlay(rows int) []string {
	lines := m.overlayLines()
	out := make([]string, 0, rows)
	for i := range rows {
		line := ""
		if at := m.overlayY + i; at < len(lines) {
			line, _ = termtext.Clip(lines[at], nil, m.method, m.overlayX, m.width)
		}
		out = append(out, m.pad(line, m.width))
	}

	return out
}

func (m *model) overlayLines() []string {
	switch m.overlay {
	case helpOverlay:
		return helpLines
	case exportOverlay:
		if m.session == nil {
			return []string{"Nothing is loaded yet."}
		}
		return exportLines(m.session.labels, m.session.tree.ids[m.selected])
	}

	return nil
}

// status is the row below the body: the jump prompt, the last notice, or
// what the worker is doing.
func (m *model) status() string {
	switch {
	case m.overlay == promptOverlay:
		return " Go to node: " + m.prompt + "_  (enter to jump, esc to cancel)"
	case m.notice != "":
		return " " + termtext.Escape(m.notice)
	case m.session == nil:
		return " Loading and validating the inputs..."
	case m.loading:
		return " Reloading and validating the inputs..."
	case m.failure != nil:
		return " " + termtext.Escape(firstLine(m.failure))
	case m.pending:
		return " Working..."
	case m.mode == compareMode:
		return fmt.Sprintf(" Comparing node %d with node %d",
			m.session.tree.ids[m.origin], m.session.tree.ids[m.selected])
	}

	return fmt.Sprintf(" Previewing node %d", m.session.tree.ids[m.selected])
}

func (m *model) keys() string {
	if m.overlay == helpOverlay || m.overlay == exportOverlay {
		return " esc close  arrows scroll  q quit"
	}

	return " q quit  ? help  tab pane  p preview  d compare  space pin  g go to  e export  r reload"
}

// style applies an SGR attribute to already-safe text.
func (m *model) style(attribute, text string) string {
	return "\x1b[" + attribute + "m" + text + "\x1b[m"
}

// color applies an SGR color unless color is off.
func (m *model) color(code, text string) string {
	if !m.colored {
		return text
	}

	return m.style(code, text)
}

// pad clips text to width cells and fills the rest with spaces. Text is
// drawn through Clip, so even a label built from untrusted parts cannot
// carry a terminal control.
func (m *model) pad(text string, width int) string {
	clipped, cells := termtext.Clip(text, nil, m.method, 0, width)
	return clipped + strings.Repeat(" ", max(width-cells, 0))
}

// count writes n with a noun that agrees with it.
func count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}

	return strconv.Itoa(n) + " " + noun + "s"
}

func firstLine(err error) string {
	text := err.Error()
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[:i]
	}

	return text
}
