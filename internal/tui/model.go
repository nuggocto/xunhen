package tui

import (
	"strconv"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/nuggocto/xunhen/internal/history"
	"github.com/nuggocto/xunhen/internal/termtext"
)

type pane uint8

const (
	treePane pane = iota
	contentPane
)

type mode uint8

const (
	previewMode mode = iota
	compareMode
)

type overlay uint8

const (
	noOverlay overlay = iota
	helpOverlay
	exportOverlay
	promptOverlay
)

// maxPromptDigits is enough for any node ID, which fits in 31 bits.
const maxPromptDigits = 10

// scheduler accepts background requests without blocking. The worker is the
// real one; tests record requests and answer them in any order they like.
type scheduler interface {
	submit(request) bool
}

// model is the browser's state. Update changes it only in response to
// messages and never performs slow work: loading, replay, and comparison go
// to the worker, and View reads only what is already prepared.
type model struct {
	work scheduler
	// wait returns the worker's next result. The model keeps one call of it
	// outstanding at a time. Tests leave it nil.
	wait    tea.Cmd
	colored bool
	// method is the renderer's width method. Bubble Tea switches to whole
	// clusters when the terminal reports Unicode core mode, and drawing
	// must measure the same way.
	method termtext.Method

	// session is the installed load, nil until the first load succeeds.
	// load and selection are the generations stamped on requests: load
	// changes with every (re)load request, selection with every change to
	// what the content pane should show.
	session         *session
	load, selection uint64
	loading         bool

	// Navigation, by preorder position in session.tree. origin is the left
	// side of a comparison; the selected node is the right side.
	folds    folds
	selected int32
	origin   int32
	top      int32

	focus   pane
	mode    mode
	overlay overlay
	prompt  string
	notice  string

	// The content pane shows the last accepted result, which may belong to
	// an earlier selection while the current request is pending.
	pending bool
	failure error
	doc     *document
	cmp     *comparison
	scrollY int
	scrollX int

	// Overlays scroll separately, so help does not lose the content's place.
	overlayY, overlayX int

	width, height int

	// fatal ends the browser: the first load failed, or the worker crashed.
	fatal   error
	crashed bool
}

func newModel(work scheduler, wait tea.Cmd, colored bool) *model {
	return &model{work: work, wait: wait, colored: colored}
}

// Init starts the first load. The browser opens at once and shows loading
// progress, and quitting stays possible while the inputs are read.
func (m *model) Init() tea.Cmd {
	m.startLoad()
	return m.wait
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = max(msg.Width, 0), max(msg.Height, 0)
		m.settle()
		m.clampSideways()
	case tea.KeyPressMsg:
		return m, m.key(msg.String())
	case result:
		return m, m.accept(msg)
	case tea.ResumeMsg:
		// Bubble Tea redraws and rechecks the size after a resume.
		m.notice = ""
	case tea.ModeReportMsg:
		m.report(msg)
	}

	return m, nil
}

// report follows the renderer's width method. Bubble Tea switches to
// cluster widths on these same replies to its Unicode core mode query. The
// shown result was measured the old way, so it is prepared again.
func (m *model) report(msg tea.ModeReportMsg) {
	if msg.Mode != ansi.ModeUnicodeCore {
		return
	}
	switch msg.Value {
	case ansi.ModeReset, ansi.ModeSet, ansi.ModePermanentlySet:
	default:
		return
	}
	if m.method == termtext.Clusters {
		return
	}

	m.method = termtext.Clusters
	m.request()
	m.settle()
	m.clampSideways()
}

// startLoad asks the worker for a new load. Displayed results are released
// first, so the old and new loads overlap by their histories alone; the old
// session stays installed until the new one validates.
func (m *model) startLoad() {
	m.load++
	m.selection++
	m.loading = true
	m.pending = false
	m.failure = nil
	m.doc, m.cmp = nil, nil
	m.work.submit(request{kind: loadRequest, load: m.load, selection: m.selection})
}

// request asks for whatever the content pane should now show. It makes every
// earlier content request obsolete, including one still running. Nothing is
// requested while a load is outstanding: the load result requests again.
func (m *model) request() {
	m.selection++
	m.failure = nil
	m.pending = false
	if m.session == nil || m.loading {
		return
	}

	r := request{load: m.load, selection: m.selection, session: m.session, target: m.ref(m.selected), method: m.method}
	switch m.mode {
	case previewMode:
		r.kind = previewRequest
	case compareMode:
		r.kind = compareRequest
		r.origin = m.ref(m.origin)
	}
	m.pending = m.work.submit(r)
}

func (m *model) ref(p int32) history.NodeRef {
	// Positions come from the session's own index, so At cannot fail.
	ref, _ := m.session.history.At(int(p))
	return ref
}

// accept applies a worker result if it still answers the latest request of
// its kind, and discards it otherwise, whether it succeeded or failed.
func (m *model) accept(r result) tea.Cmd {
	if r.crash != nil {
		m.fatal, m.crashed = r.crash, true
		return tea.Quit
	}

	switch r.kind {
	case loadRequest:
		if !m.loading || r.load != m.load {
			return m.wait
		}
		m.loading = false

		switch {
		case r.err != nil && m.session == nil:
			m.fatal = r.err
			return tea.Quit
		case r.err != nil:
			m.notice = "Reload failed; still showing the earlier load. " + firstLine(r.err)
			m.request()
		default:
			m.install(r.session)
		}

	default:
		if r.load != m.load || r.selection != m.selection {
			return m.wait
		}
		m.pending = false
		if r.err != nil {
			m.failure = r.err
			return m.wait
		}
		m.doc, m.cmp = r.doc, r.cmp
		m.settle()
		m.clampSideways()
	}

	return m.wait
}

// install replaces the session after a successful load. Old node positions
// mean nothing in the new history, so the selection is carried over by
// numeric ID when that ID still exists, and the comparison origin returns to
// the new reference.
func (m *model) install(s *session) {
	previous, carried := history.NodeID(0), false
	if m.session != nil {
		previous, carried = m.session.tree.ids[m.selected], true
	}

	m.session = s
	m.folds = make(folds, s.tree.len())
	m.selected = s.tree.reference
	if carried {
		if p, ok := s.tree.position(previous); ok {
			m.selected = p
		}
	}
	m.origin = s.tree.reference
	m.top = 0
	m.doc, m.cmp = nil, nil
	m.scrollY, m.scrollX = 0, 0
	m.settle()
	m.request()
}

func (m *model) key(k string) tea.Cmd {
	switch k {
	case "ctrl+c":
		return tea.Interrupt
	case "ctrl+z":
		return tea.Suspend
	}

	if m.overlay == promptOverlay {
		m.promptKey(k)
		return nil
	}

	switch k {
	case "q":
		return tea.Quit
	case "esc":
		m.overlay = noOverlay
		m.notice = ""
		return nil
	case "?":
		m.toggle(helpOverlay)
		return nil
	}

	if m.overlay != noOverlay {
		if k == "e" {
			m.toggle(exportOverlay)
			return nil
		}
		m.scrollOverlay(k)
		return nil
	}
	if m.session == nil {
		return nil
	}

	m.notice = ""
	switch k {
	case "tab":
		m.focus = 1 - m.focus
	case "p":
		m.show(previewMode)
	case "d":
		m.show(compareMode)
	case "space":
		m.pin()
	case "g":
		m.overlay, m.prompt = promptOverlay, ""
	case "r":
		if !m.loading {
			m.startLoad()
		}
	case "e":
		m.toggle(exportOverlay)
	default:
		if m.focus == treePane {
			m.treeKey(k)
		} else {
			m.contentKey(k)
		}
	}

	return nil
}

func (m *model) toggle(o overlay) {
	if m.overlay == o {
		m.overlay = noOverlay
		return
	}
	m.overlay = o
	m.overlayY, m.overlayX = 0, 0
}

func (m *model) show(mode mode) {
	if m.mode == mode {
		return
	}
	m.mode = mode
	m.scrollY, m.scrollX = 0, 0
	m.request()
}

func (m *model) pin() {
	m.origin = m.selected
	m.notice = "Comparisons now start from node " + strconv.Itoa(int(m.session.tree.ids[m.origin])) + "."
	if m.mode == compareMode {
		m.request()
	}
}

func (m *model) treeKey(k string) {
	t := m.session.tree
	switch k {
	case "up", "k":
		if p, ok := t.previous(m.folds, m.selected); ok {
			m.choose(p)
		}
	case "down", "j":
		if p, ok := t.next(m.folds, m.selected); ok {
			m.choose(p)
		}
	case "pgup":
		p := m.selected
		for range max(m.bodyRows()-1, 1) {
			p, _ = t.previous(m.folds, p)
		}
		m.choose(p)
	case "pgdown":
		p := m.selected
		for range max(m.bodyRows()-1, 1) {
			p, _ = t.next(m.folds, p)
		}
		m.choose(p)
	case "home":
		m.choose(0)
	case "end":
		m.choose(t.last(m.folds))
	case "left", "h":
		switch {
		case t.hasChildren(m.selected) && !m.folds[m.selected]:
			m.folds[m.selected] = true
			m.top = t.shown(m.folds, m.top)
			m.settle()
		case t.parent[m.selected] >= 0:
			m.choose(t.parent[m.selected])
		}
	case "right", "l":
		switch {
		case m.folds[m.selected]:
			m.folds[m.selected] = false
		case t.hasChildren(m.selected):
			m.choose(m.selected + 1)
		}
	}
}

// choose selects a visible row and asks for its content.
func (m *model) choose(p int32) {
	if p == m.selected {
		return
	}
	m.selected = p
	m.settle()
	m.request()
}

func (m *model) promptKey(k string) {
	switch {
	case k == "esc":
		m.overlay = noOverlay
	case k == "backspace":
		if len(m.prompt) > 0 {
			m.prompt = m.prompt[:len(m.prompt)-1]
		}
	case k == "enter":
		m.overlay = noOverlay
		m.jump(m.prompt)
	case len(k) == 1 && k[0] >= '0' && k[0] <= '9' && len(m.prompt) < maxPromptDigits:
		m.prompt += k
	}
}

// jump selects a node by ID, unfolding its ancestors. An unknown or invalid
// ID leaves the selection where it was.
func (m *model) jump(text string) {
	if text == "" || m.session == nil {
		return
	}

	n, err := strconv.ParseUint(text, 10, 31)
	if err != nil {
		m.notice = "Node IDs are whole numbers from 0 to 2147483647."
		return
	}
	p, ok := m.session.tree.position(history.NodeID(n))
	if !ok {
		m.notice = "No node " + text + " in this history."
		return
	}

	m.session.tree.reveal(m.folds, p)
	m.choose(p)
	m.settle()
}

func (m *model) contentKey(k string) {
	rows := max(m.contentRows(), 1)
	switch k {
	case "up", "k":
		m.scrollY--
	case "down", "j":
		m.scrollY++
	case "pgup":
		m.scrollY -= rows
	case "pgdown":
		m.scrollY += rows
	case "home":
		m.scrollY = 0
	case "end":
		m.scrollY = m.contentLen()
	case "left", "h":
		m.scrollX -= horizontalStep
	case "right", "l":
		m.scrollX = min(m.scrollX+horizontalStep, max(m.widestContent()-m.textWidth(), 0))
	case "0":
		m.scrollX = 0
	case "$":
		m.scrollX = max(m.widestContent()-m.textWidth(), 0)
	}
	m.settle()
}

func (m *model) scrollOverlay(k string) {
	switch k {
	case "up", "k":
		m.overlayY--
	case "down", "j":
		m.overlayY++
	case "pgup":
		m.overlayY -= max(m.bodyRows(), 1)
	case "pgdown":
		m.overlayY += max(m.bodyRows(), 1)
	case "home":
		m.overlayY = 0
	case "end":
		m.overlayY = len(m.overlayLines())
	case "left", "h":
		m.overlayX -= horizontalStep
	case "right", "l":
		m.overlayX += horizontalStep
	}
	m.overlayY = min(max(m.overlayY, 0), max(len(m.overlayLines())-m.bodyRows(), 0))

	// Scrolling stops once the widest line's end is in view.
	widest := 0
	for _, line := range m.overlayLines() {
		widest = max(widest, termtext.Width(line, nil, m.method))
	}
	m.overlayX = min(max(m.overlayX, 0), max(widest-m.width, 0))
}

// clampSideways keeps the sideways scroll within the widest line on screen.
// It runs when the content or the pane changes, so a new state never opens
// scrolled past all of its text. Scrolling down to shorter lines leaves the
// offset alone, as a pager would; 0 returns to the start.
func (m *model) clampSideways() {
	m.scrollX = min(m.scrollX, max(m.widestContent()-m.textWidth(), 0))
}

// horizontalStep is how far one sideways key scrolls, in cells.
const horizontalStep = 8

// settle clamps every scroll position to the current size and content and
// keeps the selected row in view. Every size or content change calls it, so
// no drawing step sees a position outside its range.
func (m *model) settle() {
	m.scrollY = min(max(m.scrollY, 0), max(m.contentLen()-m.contentRows(), 0))
	m.scrollX = max(m.scrollX, 0)
	m.overlayY = min(max(m.overlayY, 0), max(len(m.overlayLines())-m.bodyRows(), 0))
	if m.session == nil {
		return
	}

	t := m.session.tree
	rows := m.bodyRows()
	m.top = t.shown(m.folds, min(max(m.top, 0), t.len()-1))
	if rows <= 0 || m.selected < m.top {
		m.top = m.selected
		return
	}

	// Count rows from the top; when the selection is past the last one,
	// scroll so it becomes the last.
	q := m.top
	for range rows - 1 {
		if q == m.selected {
			return
		}
		q, _ = t.next(m.folds, q)
	}
	if q == m.selected {
		return
	}

	m.top = m.selected
	for range rows - 1 {
		m.top, _ = t.previous(m.folds, m.top)
	}
}

// contentLen is the number of rows the content pane can scroll through.
func (m *model) contentLen() int {
	switch {
	case m.mode == previewMode && m.doc != nil:
		return m.doc.len()
	case m.mode == compareMode && m.cmp != nil:
		return m.cmp.rows
	}

	return 0
}
