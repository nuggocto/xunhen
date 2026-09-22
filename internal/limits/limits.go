// Package limits defines finite budgets for loading, recovering, and comparing
// states of one history.
package limits

import "fmt"

// Limits may be lowered for a caller, but never raised above Default's ceilings.
// Byte limits count payload, not Go object or allocator overhead.
type Limits struct {
	InputBytes    int64
	BaseBytes     int64
	Nodes         int
	Entries       int
	StoredLines   int
	StateLines    int
	StateBytes    int
	LineBytes     int
	OptionalBytes int
	TextBytes     int
	OutputBytes   int
	ReplayHeaders int
	ReplayEntries int
	ReplayMoves   int

	// Diff budgets count the units defined in docs/diff.md: workspace bytes for
	// identifiers, lookup-table entries, and stored search rounds; steps for
	// diagonals visited and matched lines followed; compared bytes for line
	// bytes read while trimming and identifying.
	DiffWorkspaceBytes int
	DiffSteps          int
	DiffCompareBytes   int
}

// Default returns the command budgets documented in docs/undo-format.md.
func Default() Limits {
	return Limits{
		InputBytes:    64 << 20,
		BaseBytes:     8 << 20,
		Nodes:         100_000,
		Entries:       250_000,
		StoredLines:   1_000_000,
		StateLines:    1_000_000,
		StateBytes:    16 << 20,
		LineBytes:     1 << 20,
		OptionalBytes: 1 << 20,
		TextBytes:     128 << 20,
		OutputBytes:   16 << 20,
		ReplayHeaders: 200_000,
		ReplayEntries: 1_000_000,
		ReplayMoves:   8_000_000,

		DiffWorkspaceBytes: 32 << 20,
		DiffSteps:          10_000_000,
		DiffCompareBytes:   256 << 20,
	}
}

// Validate rejects unset, negative, and excessive budgets. Zero is not unlimited.
func (l Limits) Validate() error {
	ceiling := Default()
	bounds := []struct {
		name    string
		got     int64
		ceiling int64
	}{
		{"undo input bytes", l.InputBytes, ceiling.InputBytes},
		{"base input bytes", l.BaseBytes, ceiling.BaseBytes},
		{"history nodes", int64(l.Nodes), int64(ceiling.Nodes)},
		{"entries", int64(l.Entries), int64(ceiling.Entries)},
		{"stored lines", int64(l.StoredLines), int64(ceiling.StoredLines)},
		{"state lines", int64(l.StateLines), int64(ceiling.StateLines)},
		{"state bytes", int64(l.StateBytes), int64(ceiling.StateBytes)},
		{"line bytes", int64(l.LineBytes), int64(ceiling.LineBytes)},
		{"optional-field bytes", int64(l.OptionalBytes), int64(ceiling.OptionalBytes)},
		{"decoded text bytes", int64(l.TextBytes), int64(ceiling.TextBytes)},
		{"output bytes", int64(l.OutputBytes), int64(ceiling.OutputBytes)},
		{"replay headers", int64(l.ReplayHeaders), int64(ceiling.ReplayHeaders)},
		{"replay entries", int64(l.ReplayEntries), int64(ceiling.ReplayEntries)},
		{"replay line moves", int64(l.ReplayMoves), int64(ceiling.ReplayMoves)},
		{"diff workspace bytes", int64(l.DiffWorkspaceBytes), int64(ceiling.DiffWorkspaceBytes)},
		{"diff steps", int64(l.DiffSteps), int64(ceiling.DiffSteps)},
		{"diff compared bytes", int64(l.DiffCompareBytes), int64(ceiling.DiffCompareBytes)},
	}

	for _, bound := range bounds {
		if bound.got <= 0 || bound.got > bound.ceiling {
			return fmt.Errorf("%s budget must be between 1 and %d", bound.name, bound.ceiling)
		}
	}

	return nil
}
