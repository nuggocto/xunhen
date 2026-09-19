package main

import "encoding/hex"

// Expected states use nvim_buf_get_lines bytes, including literal NULs.
type fixtureCase struct {
	Name    string
	Purpose string

	Initial string
	Final   string
	Steps   []step

	// This text must come from the base, not a saved edit.
	BaseOnlyText string

	States  map[int][]string
	Parents map[int]int
	Visit   []int
	Pruned  []int

	Encoding       string
	FileFormat     string
	UndoLevels     int
	Persistence    string
	Classification string
}

func (fixture fixtureCase) classification() string {
	if fixture.Classification == "" {
		return "buffer-lines-with-explicit-export-policy"
	}
	return fixture.Classification
}

type step struct {
	Op       string   `json:"op"`
	Start    int      `json:"start,omitempty"`
	End      int      `json:"end,omitempty"`
	LinesHex []string `json:"lines_hex,omitempty"`
	Seq      int      `json:"seq,omitempty"`
	Value    bool     `json:"value,omitempty"`
}

func edit(start, end int, lines ...string) step {
	change := step{
		Op:    "edit",
		Start: start,
		End:   end,
	}

	for _, line := range lines {
		change.LinesHex = append(change.LinesHex, hex.EncodeToString([]byte(line)))
	}
	return change
}

func corpusCases() []fixtureCase {
	const (
		anchor     = "unchanged anchor: this text is not present in any saved edit"
		experiment = "func experiment() int { return 42 }"
		chosen     = "func chosen() int { return 1 }"
		controls   = "x\x1b[31mred\x1b[0m\ttab\a"
	)

	return []fixtureCase{
		{
			Name:    "linear",
			Purpose: "Insert, replace, and delete while an untouched line requires a base.",

			Initial:      "stem\n" + anchor + "\n",
			Final:        anchor + "\nleaf\n",
			BaseOnlyText: anchor,
			Steps: []step{
				edit(0, 1, "sprout"),
				edit(2, 2, "leaf"),
				edit(0, 1),
			},

			States: map[int][]string{
				0: {"stem", anchor},
				1: {"sprout", anchor},
				2: {"sprout", anchor, "leaf"},
				3: {anchor, "leaf"},
			},
			Parents: map[int]int{1: 0, 2: 1, 3: 2},
			Visit:   []int{0, 3, 1, 2, 0, 3},
		},
		{
			Name: "abandoned-branch",
			Purpose: "An experiment is undone before any source write, " +
				"then survives another branch's save.",

			Initial: "package sample\n\n// seed\n",
			Final:   "package sample\n\n" + chosen + "\n",
			Steps: []step{
				edit(2, 3, "// common"),
				edit(2, 3, experiment),
				{Op: "undo", Seq: 1},
				edit(2, 3, chosen),
			},

			States: map[int][]string{
				0: {"package sample", "", "// seed"},
				1: {"package sample", "", "// common"},
				2: {"package sample", "", experiment},
				3: {"package sample", "", chosen},
			},
			Parents: map[int]int{1: 0, 2: 1, 3: 1},
			Visit:   []int{2, 3, 0, 2, 1, 3},
		},
		{
			Name:    "root-branches",
			Purpose: "Alternate changes share the retained root rather than another header.",

			Initial: "seed\n",
			Final:   "right\n",
			Steps: []step{
				edit(0, 1, "left"),
				{Op: "undo", Seq: 0},
				edit(0, 1, "right"),
			},

			States: map[int][]string{
				0: {"seed"},
				1: {"left"},
				2: {"right"},
			},
			Parents: map[int]int{1: 0, 2: 0},
			Visit:   []int{1, 2, 0, 2},
		},
		{
			Name:    "pruned",
			Purpose: "The retained root is an intermediate state, not the original file.",

			Initial:    "seed\n",
			Final:      "five\n",
			UndoLevels: 2,
			Steps: []step{
				edit(0, 1, "one"),
				edit(0, 1, "two"),
				edit(0, 1, "three"),
				edit(0, 1, "four"),
				edit(0, 1, "five"),
			},

			States: map[int][]string{
				0: {"two"},
				3: {"three"},
				4: {"four"},
				5: {"five"},
			},
			Parents: map[int]int{3: 0, 4: 3, 5: 4},
			Visit:   []int{0, 5, 3, 4, 5},
			Pruned:  []int{1, 2},
		},
		{
			Name:    "empty",
			Purpose: "Deleting the last line retains a logical one-empty-line buffer.",

			Initial: "",
			Final:   "",
			Steps: []step{
				edit(0, 1, "first"),
				edit(0, 1),
			},

			States: map[int][]string{
				0: {""},
				1: {"first"},
				2: {""},
			},
			Parents: map[int]int{1: 0, 2: 1},
			Visit:   []int{0, 1, 2, 1, 0},
		},
		{
			Name: "empty-wundo",
			Purpose: "Explicit empty-buffer persistence hashes a NUL terminator " +
				"rather than the automatic writer's empty byte stream.",

			Initial:     "first\n",
			Final:       "",
			Persistence: "explicit",
			Steps:       []step{edit(0, 1)},

			States: map[int][]string{
				0: {"first"},
				1: {""},
			},
			Parents: map[int]int{1: 0},
			Visit:   []int{0, 1, 0},
		},
		{
			Name:    "repeated-lines",
			Purpose: "Line identity comes from positions, not unique content.",

			Initial: "echo\necho\necho\n",
			Final:   "middle\necho\necho\n",
			Steps: []step{
				edit(1, 2, "middle"),
				edit(0, 1),
				edit(2, 2, "echo"),
			},

			States: map[int][]string{
				0: {"echo", "echo", "echo"},
				1: {"echo", "middle", "echo"},
				2: {"middle", "echo"},
				3: {"middle", "echo", "echo"},
			},
			Parents: map[int]int{1: 0, 2: 1, 3: 2},
			Visit:   []int{3, 0, 2, 1},
		},
		{
			Name:    "save-reopen",
			Purpose: "Automatic persistent undo reloads before further editing.",

			Initial: "old\n",
			Final:   "new\ntail\n",
			Steps: []step{
				edit(0, 1, "new"),
				{Op: "save"},
				{Op: "reopen"},
				edit(1, 1, "tail"),
			},

			States: map[int][]string{
				0: {"old"},
				1: {"new"},
				2: {"new", "tail"},
			},
			Parents: map[int]int{1: 0, 2: 1},
			Visit:   []int{0, 2, 1},
		},
		{
			Name: "undone-anchor",
			Purpose: "The persisted current state is not the latest leaf; " +
				"records have mixed directions.",

			Initial: "seed\n",
			Final:   "one\n",
			Steps: []step{
				edit(0, 1, "one"),
				edit(0, 1, "two"),
				{Op: "undo", Seq: 1},
			},

			States: map[int][]string{
				0: {"seed"},
				1: {"one"},
				2: {"two"},
			},
			Parents: map[int]int{1: 0, 2: 1},
			Visit:   []int{2, 0, 2, 1},
		},
		{
			Name: "joined-edits",
			Purpose: "A joined insertion shifts the next entry's range, " +
				"making inverse-list reversal necessary.",

			Initial: "a\nb\nc\n",
			Final:   "A\ninserted\nb\nC\n",
			Steps: []step{
				edit(0, 1, "A", "inserted"),
				{Op: "join"},
				edit(3, 4, "C"),
			},

			States: map[int][]string{
				0: {"a", "b", "c"},
				1: {"A", "inserted", "b", "C"},
			},
			Parents: map[int]int{1: 0},
			Visit:   []int{0, 1, 0, 1},
		},
		{
			Name:    "no-final-newline",
			Purpose: "The base lacks a final newline although line hashing is unchanged.",

			Initial: "seed",
			Final:   "changed",
			Steps:   []step{edit(0, 1, "changed")},

			States: map[int][]string{
				0: {"seed"},
				1: {"changed"},
			},
			Parents: map[int]int{1: 0},
			Visit:   []int{0, 1},
		},
		{
			Name:    "eol-option-change",
			Purpose: "Undo restores lines but does not restore an earlier endofline option.",

			Initial: "seed\n",
			Final:   "changed",
			Steps: []step{
				edit(0, 1, "changed"),
				{Op: "endofline", Value: false},
			},

			States: map[int][]string{
				0: {"seed"},
				1: {"changed"},
			},
			Parents: map[int]int{1: 0},
			Visit:   []int{0, 1},
		},
		{
			Name:    "crlf",
			Purpose: "Disk CRLF is normalized to internal lines before hashing.",

			Initial:    "seed\r\n",
			Final:      "changed\r\n",
			FileFormat: "dos",
			Steps:      []step{edit(0, 1, "changed")},

			States: map[int][]string{
				0: {"seed"},
				1: {"changed"},
			},
			Parents:        map[int]int{1: 0},
			Visit:          []int{0, 1},
			Classification: "base-format-not-initially-supported",
		},
		{
			Name:    "latin1",
			Purpose: "Disk Latin-1 becomes UTF-8 buffer text; the encoding name is not persisted.",

			Initial:  "caf\xe9\n",
			Final:    "th\xe9\n",
			Encoding: "latin1",
			Steps:    []step{edit(0, 1, "thé")},

			States: map[int][]string{
				0: {"café"},
				1: {"thé"},
			},
			Parents:        map[int]int{1: 0},
			Visit:          []int{0, 1},
			Classification: "base-encoding-not-initially-supported",
		},
		{
			Name:    "invalid-utf8",
			Purpose: "Invalid text bytes can survive and must not crash decoding or display.",

			Initial: "\xffseed\n",
			Final:   "\xfechanged\n",
			Steps:   []step{edit(0, 1, "\xfechanged")},

			States: map[int][]string{
				0: {"\xffseed"},
				1: {"\xfechanged"},
			},
			Parents:        map[int]int{1: 0},
			Visit:          []int{0, 1},
			Classification: "invalid-utf8-not-initially-supported",
		},
		{
			Name:    "terminal-controls",
			Purpose: "Recovered control bytes are data, not terminal instructions.",

			Initial: "safe\n",
			Final:   controls + "\n",
			Steps:   []step{edit(0, 1, controls)},

			States: map[int][]string{
				0: {"safe"},
				1: {controls},
			},
			Parents: map[int]int{1: 0},
			Visit:   []int{0, 1},
		},
		{
			Name:    "embedded-nul",
			Purpose: "The API represents NUL literally; memline and undo strings represent it as LF.",

			Initial: "a\x00b\n",
			Final:   "c\x00d\n",
			Steps:   []step{edit(0, 1, "c\x00d")},

			States: map[int][]string{
				0: {"a\x00b"},
				1: {"c\x00d"},
			},
			Parents:        map[int]int{1: 0},
			Visit:          []int{0, 1},
			Classification: "embedded-nul-not-initially-supported",
		},
		{
			Name: "unsaved-wundo",
			Purpose: "An explicit undo file is anchored to unsaved buffer text " +
				"rather than the source on disk.",

			Initial:     "saved\n",
			Final:       "unsaved\n",
			Persistence: "explicit",
			Steps:       []step{edit(0, 1, "unsaved")},

			States: map[int][]string{
				0: {"saved"},
				1: {"unsaved"},
			},
			Parents: map[int]int{1: 0},
			Visit:   []int{0, 1},
		},
		{
			Name:    "move-lines",
			Purpose: "A line move produces native extmark move metadata as well as text entries.",

			Initial: "first\nsecond\nthird\n",
			Final:   "second\nthird\nfirst\n",
			Steps:   []step{{Op: "move"}},

			States: map[int][]string{
				0: {"first", "second", "third"},
				1: {"second", "third", "first"},
			},
			Parents: map[int]int{1: 0},
			Visit:   []int{0, 1, 0},
		},
	}
}
