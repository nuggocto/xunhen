package termtext_test

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/nuggocto/xunhen/internal/termtext"
)

func TestClip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		line        string
		method      termtext.Method
		from, width int
		want        string
	}{
		{name: "window inside plain text", line: "hello world", from: 6, width: 3, want: "wor"},
		{name: "window past the end", line: "short", from: 10, width: 5, want: ""},
		{name: "no cells", line: "text", from: 0, width: 0, want: ""},
		{name: "tabs expand to stops", line: "a\tb", from: 0, width: 20, want: "a       b"},
		{name: "tab cut by the left edge", line: "\tb", from: 3, width: 20, want: "     b"},
		{name: "tab stop after a wide character", line: "\u754c\tb", from: 0, width: 20, want: "\u754c      b"},
		{name: "terminal escape shown as text", line: "a\x1b[2Jb", from: 0, width: 20, want: `a\x1b[2Jb`},
		{name: "escape cut by the left edge", line: "\x1bz", from: 2, width: 20, want: "1bz"},
		{name: "wide character cut by the left edge", line: "\u754ca", from: 1, width: 2, want: " a"},
		{name: "wide character cut by the right edge", line: "a\u754c", from: 0, width: 2, want: "a "},
		{name: "combining mark joins its base", line: "e\u0301x", from: 0, width: 20, want: "e\u0301x"},
		{name: "lone combining mark", line: "\u0301a", from: 0, width: 20, want: `\u0301a`},
		{name: "invalid byte", line: "a\xffb", from: 0, width: 20, want: `a\xffb`},
		{name: "C1 control introducer", line: "a\u009b2Jb", from: 0, width: 20, want: `a\u009b2Jb`},
		{name: "bidirectional override", line: "\u202eabc", from: 0, width: 20, want: `\u202eabc`},
		{name: "quotes and backslashes stay literal", line: `"\n"`, from: 0, width: 20, want: `"\n"`},
		// The measuring window must never shrink below the first character,
		// or drawing stops advancing.
		// An emoji presentation selector widens a symbol only when the
		// renderer measures whole clusters, so the tail moves right.
		{name: "emoji presentation measured by rune", line: strings.Repeat("\u2764\ufe0f", 20) + "TAIL", from: 20, width: 20, want: "TAIL"},
		{name: "emoji presentation measured by cluster", line: strings.Repeat("\u2764\ufe0f", 20) + "TAIL", method: termtext.Clusters, from: 40, width: 20, want: "TAIL"},
		{name: "stray continuation bytes after a character", line: "\u00e9" + strings.Repeat("\x80", 40), from: 0, width: 20, want: "\u00e9" + `\x80\x80\x80\x80\x8`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, cells := termtext.Clip(tt.line, nil, tt.method, tt.from, tt.width)
			if got != tt.want {
				t.Fatalf("Clip = %q, want %q", got, tt.want)
			}
			checkClip(t, tt.line, got, cells, tt.width, tt.method)
		})
	}
}

// A long line clips the same through its index as by measuring it from the
// start, at every window. The index is what keeps a 16 MiB line cheap, so an
// index that drifts by one cell would misplace everything after it.
func TestClipThroughIndex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line string
	}{
		{name: "ASCII", line: strings.Repeat("0123456789", 2000)},
		{name: "tabs and wide characters", line: strings.Repeat("a\t\u754c\tb", 2800)},
		{name: "escapes and combining marks", line: strings.Repeat("\x1b[1m e\u0301 \xff ", 1600)},
		{name: "one long cluster", line: "a" + strings.Repeat("\u0301", 8000) + "b"},
		{name: "emoji presentation", line: strings.Repeat("\u2764\ufe0f x", 3000)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			for _, method := range []termtext.Method{termtext.Runes, termtext.Clusters} {
				index, err := termtext.Index(t.Context(), tt.line, method)
				if err != nil || index == nil {
					t.Fatalf("index = %v, error = %v; want an index of a long line", index, err)
				}

				total := termtext.Width(tt.line, nil, method)
				if got := termtext.Width(tt.line, index, method); got != total {
					t.Fatalf("method %d: indexed width %d, measured %d", method, got, total)
				}

				for from := 0; from <= total; from += 397 {
					for _, width := range []int{1, 7, 80} {
						want, wantCells := termtext.Clip(tt.line, nil, method, from, width)
						got, cells := termtext.Clip(tt.line, index, method, from, width)
						if got != want || cells != wantCells {
							t.Fatalf("method %d, from %d width %d: indexed %q (%d cells), measured %q (%d cells)",
								method, from, width, got, cells, want, wantCells)
						}
						checkClip(t, tt.line, got, cells, width, method)
					}
				}
			}
		})
	}
}

// An index records the method it measured with. Drawing with the other
// method must measure afresh rather than trust columns that no longer hold.
func TestIndexOfAnotherMethodIsIgnored(t *testing.T) {
	t.Parallel()

	line := strings.Repeat("\u2764\ufe0f", 3000) + "TAIL"
	byRune, err := termtext.Index(t.Context(), line, termtext.Runes)
	if err != nil {
		t.Fatal(err)
	}

	total := termtext.Width(line, byRune, termtext.Clusters)
	if total != 6004 {
		t.Fatalf("cluster width %d, want 6004", total)
	}
	if got, _ := termtext.Clip(line, byRune, termtext.Clusters, total-4, 4); got != "TAIL" {
		t.Fatalf("the last cells by cluster are %q, want TAIL", got)
	}
}

func FuzzClip(f *testing.F) {
	f.Add("plain text", 0, 10)
	f.Add("a\tb\x1b[31m\u754ce\u0301\xff\u009b\u202e", 3, 7)
	f.Add("\u1100\u1100\u1100\u1100\u1100\u1100\u1100\u1100\u1100\u1100\u1100\u1100", 0, 30)
	f.Add("\u0e01\u0e33\u0915\u094d\u0937\u093f \U0001f44d\U0001f3fd \U0001f1eb\U0001f1f7\U0001f1ec", 1, 12)
	f.Add("\u0d4e\ta\u0d4e\x00\uff76\uff9e\u1100\u1161\u11a8\u0645\u064e", 0, 40)

	f.Fuzz(func(t *testing.T, line string, from, width int) {
		if len(line) > 16384 || from < 0 || from > 1<<16 || width < 0 || width > 512 {
			t.Skip()
		}

		for _, method := range []termtext.Method{termtext.Runes, termtext.Clusters} {
			got, cells := termtext.Clip(line, nil, method, from, width)
			checkClip(t, line, got, cells, width, method)

			index, err := termtext.Index(t.Context(), line, method)
			if err != nil {
				t.Fatal(err)
			}
			if indexed, indexedCells := termtext.Clip(line, index, method, from, width); indexed != got || indexedCells != cells {
				t.Fatalf("method %d: indexed clip %q (%d cells), measured %q (%d cells)", method, indexed, indexedCells, got, cells)
			}
		}
	})
}

// checkClip requires terminal-safe output that the renderer, measuring with
// the same method, finds as wide as reported, and never wider than the window.
func checkClip(t *testing.T, line, got string, cells, width int, method termtext.Method) {
	t.Helper()

	if cells < 0 || cells > width {
		t.Fatalf("%q: %d cells in a window of %d", line, cells, width)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("%q: clipped text %q is not valid UTF-8", line, got)
	}
	for _, r := range got {
		if r != ' ' && !unicode.IsGraphic(r) {
			t.Fatalf("%q: clipped text %q contains %U", line, got, r)
		}
	}
	measure := ansi.StringWidthWc
	if method == termtext.Clusters {
		measure = ansi.StringWidth
	}
	if measured := measure(got); measured != cells {
		t.Fatalf("%q: clipped text %q measures %d cells, reported %d", line, got, measured, cells)
	}
}
