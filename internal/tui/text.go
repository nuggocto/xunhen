package tui

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/nuggocto/xunhen/internal/history"
)

var helpLines = []string{
	"Keys",
	"",
	"  In the tree",
	"    up/down, j/k       select the previous or next row",
	"    left/right, h/l    fold or unfold a node's descendants; left on a",
	"                       folded or childless node selects its parent",
	"    page up/down       move by a screen",
	"    home/end           select the first or last row",
	"",
	"  In the text",
	"    up/down, j/k       scroll one line",
	"    left/right, h/l    scroll sideways",
	"    0 and $            scroll to the start, or to the end of the",
	"                       longest line on screen",
	"    page up/down       scroll by a screen",
	"    home/end           go to the top or bottom",
	"",
	"  Anywhere",
	"    tab     move between the tree and the text; a narrow terminal",
	"            shows one of them at a time",
	"    p       preview the selected node's text",
	"    d       compare the pinned node (\"-\" lines) with the selected",
	"            node (\"+\" lines)",
	"    space   pin the selected node as the left side of comparisons",
	"    g       go to a node by its ID",
	"    e       show how to save the selected node's text to a file",
	"    r       reload: find, read, and validate the inputs again",
	"    ?       show or hide this help; esc also closes it",
	"    q       quit; ctrl+c interrupts; ctrl+z suspends",
	"",
	"Reading the tree",
	"",
	"  Each row is one state of the buffer, after one undo block. Rows",
	"  follow the recorded branch order. A node's preferred child continues",
	"  on the same level below it; each other child starts a branch, marked",
	"  `-, one level deeper. [+N] counts the rows a fold hides, and <N>",
	"  stands in for indentation too deep for the pane.",
	"",
	"  [ref] marks the reference: the buffer when the undo file was",
	"  written, which the base text was verified against. [from] marks the",
	"  pinned left side of comparisons, the reference until you pin another.",
	"",
	"What the history cannot tell you",
	"",
	"  Times are when Neovim recorded each change, shown in local time.",
	"  They label changes; they are not a complete timeline, and the",
	"  root's time is unknown. Only history written to the undo file is",
	"  here: edits after the last write and states dropped by 'undolevels'",
	"  are gone. The file records no encoding, line endings, or final",
	"  newline.",
	"",
	"  Text is shown with tabs expanded, and control characters and",
	"  invalid bytes shown as escapes such as \\x1b. Export with",
	"  xunhen show --raw for the exact bytes; press e for the command.",
	"",
	"Reloading",
	"",
	"  r finds and reads the inputs the same way as at startup and",
	"  validates them before anything changes. The selection keeps its",
	"  node ID when that node still exists, and the pinned node returns to",
	"  the new reference. When a reload fails, the earlier load stays.",
}

// exportLines explains how to save the selected state with xunhen show, using
// the arguments that loaded this history. Each input flag gets its own
// continuation line, so 32 undo directories make 32 short lines rather than
// one line thousands of columns wide.
func exportLines(labels Labels, node history.NodeID) []string {
	lines := []string{
		fmt.Sprintf("Save node %d to a file", node),
		"",
		"The browser does not write files. Quit, or use another shell, and run:",
		"",
		"  xunhen show \\",
	}
	for i := 0; i+1 < len(labels.Inputs); i += 2 {
		lines = append(lines, "    "+shellQuote(labels.Inputs[i])+" "+shellQuote(labels.Inputs[i+1])+" \\")
	}

	return append(lines,
		fmt.Sprintf("    --node %d --raw --final-newline=include > recovered.go", node),
		"",
		"--final-newline sets how the file ends: include puts a newline after",
		"the last line, as most source files have; omit leaves it off. The undo",
		"file does not record which one the original file used.",
		"",
		"--raw writes the exact UTF-8 text and refuses to write to a terminal,",
		"so redirect it. It refuses a state holding invalid UTF-8 or NUL bytes;",
		"without --raw, show prints any state with escapes instead.",
		"",
		"Either input form works:",
		"  --undo HISTORY --base TEXT        an undo file and a matching copy of",
		"                                    its text",
		"  --source FILE --undo-dir DIR...   find the history by the source",
		"                                    file's path",
	)
}

// shellQuote quotes one argument for a POSIX shell. Arguments made of common
// path characters stay bare; printable text goes in single quotes; anything
// else uses $'...' with \xHH escapes, which bash and zsh read and which
// displays as printable ASCII. Terminal escaping is separate: the result
// is still drawn through termtext like any other text.
func shellQuote(arg string) string {
	if arg != "" && strings.Trim(arg, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_@%+=:,./-") == "" {
		return arg
	}

	printable := utf8.ValidString(arg)
	for _, r := range arg {
		if !unicode.IsPrint(r) {
			printable = false
			break
		}
	}
	if printable {
		return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
	}

	var b strings.Builder
	b.WriteString("$'")
	for i := range len(arg) {
		c := arg[i]
		switch {
		case c == '\'' || c == '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c >= 0x20 && c < 0x7f:
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "\\x%02x", c)
		}
	}
	b.WriteString("'")

	return b.String()
}
