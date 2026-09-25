package tui

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/nuggocto/xunhen/internal/history"
	"github.com/nuggocto/xunhen/internal/limits"
	"github.com/nuggocto/xunhen/internal/termtext"
)

// recorder stands in for the worker: it keeps every request, and the test
// answers them in whatever order it wants.
type recorder struct {
	requests []request
}

func (r *recorder) submit(q request) bool {
	r.requests = append(r.requests, q)
	return true
}

func (r *recorder) last(t *testing.T) request {
	t.Helper()
	if len(r.requests) == 0 {
		t.Fatal("no request was made")
	}
	return r.requests[len(r.requests)-1]
}

// browser is a model driven through Update, as Bubble Tea drives it.
type browser struct {
	t    *testing.T
	m    *model
	work *recorder
}

func newBrowser(t *testing.T, width, height int) *browser {
	t.Helper()

	work := &recorder{}
	m := newModel(work, nil, true)
	m.Init()
	m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return &browser{t: t, m: m, work: work}
}

// loaded answers the pending load with a session.
func (b *browser) loaded(s *session) {
	b.t.Helper()
	q := b.work.last(b.t)
	if q.kind != loadRequest {
		b.t.Fatalf("last request is kind %d, want a load", q.kind)
	}
	b.m.Update(result{request: q, session: s})
}

// answer performs a request for real and delivers its result.
func (b *browser) answer(q request) {
	b.t.Helper()
	e := &engine{limits: limits.Default(), cache: newCache(cacheBudget)}
	b.m.Update(e.perform(b.t.Context(), q))
}

func (b *browser) press(keys ...string) tea.Cmd {
	var cmd tea.Cmd
	for _, k := range keys {
		_, cmd = b.m.Update(keyPress(k))
	}
	return cmd
}

func (b *browser) selected() history.NodeID {
	return b.m.session.tree.ids[b.m.selected]
}

func keyPress(name string) tea.KeyPressMsg {
	special := map[string]tea.Key{
		"up": {Code: tea.KeyUp}, "down": {Code: tea.KeyDown},
		"left": {Code: tea.KeyLeft}, "right": {Code: tea.KeyRight},
		"pgup": {Code: tea.KeyPgUp}, "pgdown": {Code: tea.KeyPgDown},
		"home": {Code: tea.KeyHome}, "end": {Code: tea.KeyEnd},
		"tab": {Code: tea.KeyTab}, "enter": {Code: tea.KeyEnter},
		"esc": {Code: tea.KeyEscape}, "backspace": {Code: tea.KeyBackspace},
		"space":  {Code: tea.KeySpace, Text: " "},
		"ctrl+c": {Code: 'c', Mod: tea.ModCtrl},
		"ctrl+z": {Code: 'z', Mod: tea.ModCtrl},
	}
	if k, ok := special[name]; ok {
		return tea.KeyPressMsg(k)
	}

	r, _ := utf8.DecodeRuneInString(name)
	return tea.KeyPressMsg{Code: r, Text: name}
}

// screen renders the view as plain text, with the model's own SGR
// attributes removed, one string per row.
func (b *browser) screen() []string {
	content := b.m.View().Content
	return strings.Split(sgr.ReplaceAllString(content, ""), "\n")
}

var sgr = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func (b *browser) shows(text string) bool {
	return strings.Contains(strings.Join(b.screen(), "\n"), text)
}

func TestBrowserStartsAtTheReference(t *testing.T) {
	t.Parallel()

	b := newBrowser(t, 100, 20)
	b.loaded(load(t, readFixture(t, "abandoned-branch").loader()))

	q := b.work.last(t)
	if b.selected() != 3 || b.m.session.tree.ids[b.m.origin] != 3 {
		t.Fatalf("selected node %d, origin node %d; want the reference, node 3", b.selected(), b.m.session.tree.ids[b.m.origin])
	}
	if q.kind != previewRequest || q.target != ref(t, b.m.session, 3) {
		t.Fatalf("first content request = %+v, want a preview of node 3", q)
	}
}

func TestNavigationAndComparisonDirection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		keys     []string
		selected history.NodeID
		// origin is the left side of the last comparison requested, or -1
		// when the last request is a preview.
		origin history.NodeID
	}{
		{name: "down moves to the abandoned branch", keys: []string{"j"}, selected: 2, origin: -1},
		{name: "compare from the reference", keys: []string{"j", "d"}, selected: 2, origin: 3},
		{name: "pin and compare the other way", keys: []string{"j", "space", "k", "d"}, selected: 3, origin: 2},
		{name: "pinning while comparing asks again", keys: []string{"d", "j", "space"}, selected: 2, origin: 2},
		{name: "left folds, then selects the parent", keys: []string{"k", "h", "h"}, selected: 0, origin: -1},
		{name: "home and end", keys: []string{"home", "end"}, selected: 2, origin: -1},
		{name: "a folded branch is skipped", keys: []string{"k", "h", "j"}, selected: 1, origin: -1},
		{name: "tab gives the arrows to the text", keys: []string{"tab", "j"}, selected: 3, origin: -1},
		{name: "jump to an ID", keys: []string{"g", "2", "enter"}, selected: 2, origin: -1},
		{name: "jump to the root", keys: []string{"g", "0", "enter"}, selected: 0, origin: -1},
		{name: "an unknown ID keeps the selection", keys: []string{"g", "9", "9", "enter"}, selected: 3, origin: -1},
		{name: "a cancelled jump keeps the selection", keys: []string{"g", "2", "esc"}, selected: 3, origin: -1},
		{name: "an ID too large for a node keeps the selection", keys: []string{"g", "9", "9", "9", "9", "9", "9", "9", "9", "9", "9", "enter"}, selected: 3, origin: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			b := newBrowser(t, 100, 20)
			b.loaded(load(t, readFixture(t, "abandoned-branch").loader()))
			b.press(tt.keys...)

			if b.selected() != tt.selected {
				t.Fatalf("selected node %d, want %d", b.selected(), tt.selected)
			}

			q := b.work.last(t)
			switch {
			case tt.origin < 0 && q.kind != previewRequest:
				t.Fatalf("last request is kind %d, want a preview", q.kind)
			case tt.origin >= 0 && (q.kind != compareRequest || q.origin != ref(t, b.m.session, tt.origin)):
				t.Fatalf("last request = %+v, want a comparison from node %d", q, tt.origin)
			}
			if q.target != ref(t, b.m.session, tt.selected) {
				t.Fatal("the last request is not for the selected node")
			}
		})
	}
}

// A jump to a node inside folded branches unfolds them and brings the node
// into view, however far down it is.
func TestJumpRevealsHiddenNodes(t *testing.T) {
	t.Parallel()

	states := make([][]string, 300)
	for i := range states {
		states[i] = []string{fmt.Sprint("state ", i)}
	}
	b := newBrowser(t, 100, 20)
	b.loaded(load(t, loaderOf(chain(states))))
	b.press("home", "h") // fold the root, hiding every other node

	b.press("g", "1", "5", "0", "enter")
	if b.selected() != 150 || b.m.session.tree.shown(b.m.folds, b.m.selected) != b.m.selected {
		t.Fatalf("selected node %d, visible %t; want node 150 in view", b.selected(),
			b.m.session.tree.shown(b.m.folds, b.m.selected) == b.m.selected)
	}
	if !b.shows("> 150") {
		t.Fatalf("node 150 is not on screen:\n%s", strings.Join(b.screen(), "\n"))
	}
}

// Moving through A, B, and C while A runs makes A and B obsolete. Their
// results, successes and failures alike, arrive late and must change nothing;
// only C's result is shown.
func TestLateResultsAreDiscarded(t *testing.T) {
	t.Parallel()

	b := newBrowser(t, 100, 20)
	b.loaded(load(t, readFixture(t, "abandoned-branch").loader()))
	a := b.work.last(t) // preview of node 3

	b.press("j") // node 2
	failing := b.work.last(t)
	b.press("k", "k") // node 1
	c := b.work.last(t)

	b.answer(a)
	b.m.Update(result{request: failing, err: errors.New("replay failed")})
	if b.m.doc != nil || b.m.failure != nil || !b.m.pending {
		t.Fatalf("an obsolete result changed the view: doc %v, failure %v, pending %t", b.m.doc, b.m.failure, b.m.pending)
	}

	b.answer(c)
	if b.m.doc == nil || b.m.doc.id != 1 || b.m.pending {
		t.Fatalf("the current result was not shown: %+v", b.m.doc)
	}
	if !b.shows("// common") {
		t.Fatalf("node 1's text is not on screen:\n%s", strings.Join(b.screen(), "\n"))
	}
}

// While the next state is pending, the view must not present the earlier
// result as the new selection.
func TestPendingStatesAreLabelled(t *testing.T) {
	t.Parallel()

	b := newBrowser(t, 100, 20)
	b.loaded(load(t, readFixture(t, "abandoned-branch").loader()))
	b.answer(b.work.last(t))
	b.press("j")

	if !b.shows("Node 2: reconstructing; showing node 3 until it is ready") {
		t.Fatalf("the pending state is not labelled:\n%s", strings.Join(b.screen(), "\n"))
	}
}

func TestReload(t *testing.T) {
	t.Parallel()

	abandoned := readFixture(t, "abandoned-branch")
	three := [][]string{{"one"}, {"two"}, {"three"}}

	tests := []struct {
		name string
		// next answers the reload request.
		next func(t *testing.T, b *browser, q request)
		// keys are pressed while the reload is pending.
		keys          []string
		selected      history.NodeID
		reloaded      bool
		noticeContent string
	}{
		{
			name: "the selection keeps its ID", keys: nil,
			next: func(t *testing.T, b *browser, q request) {
				b.m.Update(result{request: q, session: load(t, abandoned.loader())})
			},
			selected: 2, reloaded: true,
		},
		{
			name: "a missing ID falls back to the reference",
			next: func(t *testing.T, b *browser, q request) {
				b.m.Update(result{request: q, session: load(t, loaderOf(chain(three[:2])))})
			},
			selected: 1, reloaded: true,
		},
		{
			name: "a failed reload keeps the earlier load",
			next: func(t *testing.T, b *browser, q request) {
				b.m.Update(result{request: q, err: errors.New("undo file changed while it was read")})
			},
			selected: 2, noticeContent: "Reload failed; still showing the earlier load. undo file changed",
		},
		{
			name: "moving during a reload waits for the new load", keys: []string{"k", "k"},
			next: func(t *testing.T, b *browser, q request) {
				b.m.Update(result{request: q, session: load(t, abandoned.loader())})
			},
			selected: 1, reloaded: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			b := newBrowser(t, 100, 20)
			old := load(t, abandoned.loader())
			b.loaded(old)
			b.press("j", "space", "d") // node 2, pinned, compared
			b.answer(b.work.last(t))

			b.press("r")
			reload := b.work.last(t)
			if reload.kind != loadRequest || b.m.cmp != nil {
				t.Fatalf("reload request = %+v, comparison still held: %t", reload, b.m.cmp != nil)
			}
			stale := b.work.requests[len(b.work.requests)-2]

			before := len(b.work.requests)
			b.press(tt.keys...)
			if len(b.work.requests) != before {
				t.Fatal("moving during a reload made a request that could replace it")
			}

			tt.next(t, b, reload)
			// A result for a request made before the reload is obsolete.
			b.answer(stale)

			if b.selected() != tt.selected {
				t.Fatalf("selected node %d, want %d", b.selected(), tt.selected)
			}
			if replaced := b.m.session != old; replaced != tt.reloaded {
				t.Fatalf("session replaced = %t, want %t", replaced, tt.reloaded)
			}
			if tt.reloaded && b.m.origin != b.m.session.tree.reference {
				t.Fatal("the comparison origin did not return to the reference")
			}
			if tt.noticeContent != "" && !strings.Contains(b.m.notice, tt.noticeContent) {
				t.Fatalf("notice %q, want it to contain %q", b.m.notice, tt.noticeContent)
			}

			q := b.work.last(t)
			if q.kind == loadRequest || q.session != b.m.session || q.load != b.m.load {
				t.Fatalf("after the reload, last request = %+v; want content for the installed load", q)
			}
			if b.m.cmp != nil || b.m.doc != nil {
				t.Fatal("an obsolete result reached the view")
			}
		})
	}
}

// A load result is current only for the latest load request. One from an
// earlier load, success or failure, must not install or cancel anything.
func TestEarlierLoadResultsAreDiscarded(t *testing.T) {
	t.Parallel()

	f := readFixture(t, "abandoned-branch")
	tests := []struct {
		name  string
		early result
	}{
		{name: "an earlier success", early: result{session: load(t, f.loader())}},
		{name: "an earlier failure", early: result{err: errors.New("changed while read")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			b := newBrowser(t, 100, 20)
			first := load(t, f.loader())
			b.loaded(first)
			b.press("r")
			reload := b.work.last(t)

			early := tt.early
			early.request = request{kind: loadRequest, load: reload.load - 1}
			b.m.Update(early)
			if b.m.session != first || !b.m.loading || b.m.notice != "" {
				t.Fatalf("an earlier load result was applied: session replaced %t, loading %t, notice %q",
					b.m.session != first, b.m.loading, b.m.notice)
			}
		})
	}
}

func TestFirstLoadFailureEndsTheBrowser(t *testing.T) {
	t.Parallel()

	b := newBrowser(t, 100, 20)
	failure := errors.New("no undo history matches the source")
	_, cmd := b.m.Update(result{request: b.work.last(t), err: failure})

	if !errors.Is(b.m.fatal, failure) || b.m.crashed || cmd == nil {
		t.Fatalf("fatal = %v, crashed = %t; want the load failure and a quit", b.m.fatal, b.m.crashed)
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("the browser did not quit")
	}
}

func TestQuitKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		keys []string
		want tea.Msg
	}{
		{name: "q quits", keys: []string{"q"}, want: tea.QuitMsg{}},
		{name: "q quits from help", keys: []string{"?", "q"}, want: tea.QuitMsg{}},
		{name: "ctrl+c interrupts", keys: []string{"ctrl+c"}, want: tea.InterruptMsg{}},
		{name: "ctrl+c interrupts a jump prompt", keys: []string{"g", "ctrl+c"}, want: tea.InterruptMsg{}},
		{name: "ctrl+z suspends", keys: []string{"ctrl+z"}, want: tea.SuspendMsg{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Before and after the first load: quitting must not wait for
			// the inputs.
			for _, loaded := range []bool{false, true} {
				b := newBrowser(t, 100, 20)
				if loaded {
					b.loaded(load(t, readFixture(t, "abandoned-branch").loader()))
				}

				cmd := b.press(tt.keys...)
				if cmd == nil {
					t.Fatalf("loaded %t: no command", loaded)
				}
				if got := cmd(); got != tt.want {
					t.Fatalf("loaded %t: command gave %#v, want %#v", loaded, got, tt.want)
				}
			}
		})
	}
}

// Recovered text, file names, and errors come from untrusted input. Nothing
// in them may reach the terminal as a control sequence, whatever the size.
func TestUntrustedTextCannotControlTheTerminal(t *testing.T) {
	t.Parallel()

	hostile := []string{
		"\x1b]52;c;dGVzdA==\a clipboard",
		"\x1b[2J\x1b[H clear",
		"\u009b31m C1 introducer",
		"‮ reversed",
		"\xff\xfe invalid",
		"tab\there \r carriage",
	}
	undo, base := chain([][]string{{"safe"}, hostile})
	loader := func(t *testing.T) *session {
		s := load(t, loaderOf(undo, base))
		s.labels = Labels{Undo: "evil\x1b]0;title\a.undo", Base: "\x1b[31mbase", Inputs: []string{"--undo", "evil\x1b]0;title\a.undo"}}
		return s
	}

	sizes := []struct{ width, height int }{{100, 20}, {60, 12}, {20, 4}}
	views := []struct {
		name string
		keys []string
	}{
		{name: "preview"},
		{name: "comparison", keys: []string{"k", "space", "j", "d"}},
		{name: "export", keys: []string{"e"}},
		{name: "failure"},
	}

	for _, size := range sizes {
		for _, view := range views {
			t.Run(fmt.Sprintf("%s at %dx%d", view.name, size.width, size.height), func(t *testing.T) {
				t.Parallel()

				b := newBrowser(t, size.width, size.height)
				b.loaded(loader(t))
				b.press(view.keys...)
				if view.name == "failure" {
					b.m.Update(result{request: b.work.last(t), err: errors.New("bad \x1b[2J path")})
				} else {
					b.answer(b.work.last(t))
				}

				for i, row := range b.screen() {
					for _, r := range row {
						if r == utf8.RuneError || (r != ' ' && !unicode.IsGraphic(r)) {
							t.Fatalf("row %d holds %U: %q", i, r, row)
						}
					}
				}
			})
		}
	}
}

// Every size, down to nothing, must draw without panicking, keep each row
// within the width, and keep the keys working.
func TestAnySizeDraws(t *testing.T) {
	t.Parallel()

	sizes := []struct{ width, height int }{
		{0, 0}, {1, 1}, {19, 3}, {20, 4}, {40, 5}, {79, 24}, {80, 24}, {300, 100},
	}
	for _, size := range sizes {
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			t.Parallel()

			lines := make([]string, 500)
			for i := range lines {
				lines[i] = strings.Repeat("wide 界 text ", 40)
			}
			b := newBrowser(t, size.width, size.height)
			b.loaded(load(t, loaderOf(chain([][]string{{"x"}, lines}))))
			b.answer(b.work.last(t))
			b.press("tab", "end", "right", "right", "pgup", "?", "down", "esc", "e", "esc", "tab", "home")

			rows := b.screen()
			if len(rows) > max(size.height, 1) {
				t.Fatalf("%d rows on a %d-row terminal", len(rows), size.height)
			}
			for i, row := range rows {
				if w := ansi.StringWidthWc(row); w > size.width {
					t.Fatalf("row %d is %d cells on a %d-cell terminal: %q", i, w, size.width, row)
				}
			}
			if cmd := b.press("q"); cmd == nil || cmd() != (tea.QuitMsg{}) {
				t.Fatal("q no longer quits")
			}
		})
	}
}

func TestContentScrolling(t *testing.T) {
	t.Parallel()

	lines := make([]string, 1000)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %04d %s end", i+1, strings.Repeat("-", 200))
	}

	tests := []struct {
		name  string
		keys  []string
		shows []string
		hides []string
	}{
		{name: "top", shows: []string{"line 0001"}},
		{name: "end shows the last line", keys: []string{"tab", "end"}, shows: []string{"line 1000"}, hides: []string{"line 0001"}},
		{name: "page down", keys: []string{"tab", "pgdown"}, shows: []string{"line 0017"}, hides: []string{"line 0001"}},
		{name: "scrolling up stops at the top", keys: []string{"tab", "up", "up", "pgup"}, shows: []string{"line 0001"}},
		{name: "sideways scrolling reaches the end of a long line", keys: append([]string{"tab"}, repeated(40, "right")...), shows: []string{"- end"}, hides: []string{"line 0001"}},
		{name: "dollar jumps to the end of the longest line", keys: []string{"tab", "$"}, shows: []string{"- end"}, hides: []string{"line 0001"}},
		{name: "zero returns to the start", keys: []string{"tab", "$", "0"}, shows: []string{"line 0001"}},
		// 56 rows of text end at line 1000 only if the view scrolled back up.
		{name: "a taller terminal scrolls back to fill the screen", keys: []string{"tab", "end", "resize"}, shows: []string{"line 0945", "line 1000"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			b := newBrowser(t, 100, 20)
			b.loaded(load(t, loaderOf(chain([][]string{{"x"}, lines}))))
			b.answer(b.work.last(t))
			for _, k := range tt.keys {
				if k == "resize" {
					b.m.Update(tea.WindowSizeMsg{Width: 90, Height: 60})
					continue
				}
				b.press(k)
			}

			for _, text := range tt.shows {
				if !b.shows(text) {
					t.Fatalf("%q is not on screen:\n%s", text, strings.Join(b.screen(), "\n"))
				}
			}
			for _, text := range tt.hides {
				if b.shows(text) {
					t.Fatalf("%q is still on screen", text)
				}
			}
		})
	}
}

func repeated(n int, key string) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = key
	}
	return out
}

// Once the terminal reports Unicode core mode, Bubble Tea measures whole
// clusters, and an emoji presentation selector makes a symbol two cells wide.
// The browser must measure the same way, or a line's end falls off the pane
// where no sideways scrolling can reach it.
func TestClusterWidthsFollowTheRenderer(t *testing.T) {
	t.Parallel()

	b := newBrowser(t, 100, 20)
	b.loaded(load(t, loaderOf(chain([][]string{{"x"}, {strings.Repeat("\u2764\ufe0f", 40) + "TAIL"}}))))
	b.m.Update(tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value: ansi.ModeReset})

	q := b.work.last(t)
	if q.method != termtext.Clusters {
		t.Fatal("the shown state was not prepared again for cluster widths")
	}
	b.answer(q)
	b.press("tab", "$")

	for _, row := range b.screen() {
		if strings.Contains(row, "\u2764") && (!strings.Contains(row, "TAIL") || ansi.StringWidth(row) > 100) {
			t.Fatalf("by cluster widths, the row is %d cells and ends %q", ansi.StringWidth(row), row[max(0, len(row)-20):])
		}
	}
}

// A sideways offset that suited one state must not leave the next state's
// text scrolled out of view.
func TestNewContentIsNotScrolledPastItsText(t *testing.T) {
	t.Parallel()

	b := newBrowser(t, 100, 20)
	b.loaded(load(t, loaderOf(chain([][]string{{"short text"}, {strings.Repeat("long ", 2000)}}))))
	b.answer(b.work.last(t))
	b.press("tab", "$", "tab", "k")
	b.answer(b.work.last(t))

	if !b.shows("short text") {
		t.Fatalf("the short state is scrolled out of view:\n%s", strings.Join(b.screen(), "\n"))
	}
}

// Every part of the export command must be reachable, with the most undo
// directories an invocation accepts or with one very long path.
func TestExportCommandIsReachable(t *testing.T) {
	t.Parallel()

	many := []string{"--source", "retry.go"}
	for i := range 32 {
		many = append(many, "--undo-dir", fmt.Sprintf("/undo/%03d/%s-END%d", i, strings.Repeat("d", 150), i))
	}

	tests := []struct {
		name   string
		inputs []string
		tail   string
		keys   int // sideways key presses needed, with room to spare
	}{
		{name: "32 undo directories", inputs: many, tail: "-END31 \\", keys: 30},
		{name: "a 6,000-character path", inputs: []string{"--undo", strings.Repeat("u", 6000) + "-END", "--base", "retry.go"}, tail: "-END \\", keys: 800},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			b := newBrowser(t, 100, 60)
			s := load(t, readFixture(t, "abandoned-branch").loader())
			s.labels.Inputs = tt.inputs
			b.loaded(s)
			b.press("e")
			b.press(repeated(tt.keys, "l")...)

			if !b.shows(tt.tail) {
				t.Fatalf("the end of the command is out of reach:\n%s", strings.Join(b.screen(), "\n"))
			}
		})
	}
}

func TestIdenticalComparison(t *testing.T) {
	t.Parallel()

	b := newBrowser(t, 100, 20)
	b.loaded(load(t, readFixture(t, "abandoned-branch").loader()))
	b.press("d")
	b.answer(b.work.last(t))

	if !b.shows("Node 3 -> node 3: identical states") || !b.shows("The two states are identical.") {
		t.Fatalf("an identical comparison is not stated:\n%s", strings.Join(b.screen(), "\n"))
	}
}

// Colors carry meaning only as a convenience. With NO_COLOR, the view uses
// no color at all, and the selection stays marked in plain text.
func TestNoColor(t *testing.T) {
	t.Parallel()

	b := newBrowser(t, 100, 20)
	b.m.colored = colorAllowed([]string{"TERM=xterm", "NO_COLOR=1"})
	b.loaded(load(t, readFixture(t, "abandoned-branch").loader()))
	b.press("j", "d")
	b.answer(b.work.last(t))

	content := b.m.View().Content
	for _, match := range regexp.MustCompile(`\x1b\[([0-9;]*)m`).FindAllStringSubmatch(content, -1) {
		for _, parameter := range strings.Split(match[1], ";") {
			if parameter != "" && parameter != sgrReverse && parameter != sgrBold && parameter != sgrDim {
				t.Fatalf("NO_COLOR view uses SGR %q", match[0])
			}
		}
	}
	if !b.shows("> ") {
		t.Fatal("the selection has no plain-text marker")
	}
}
