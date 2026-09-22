// Package limits defines finite budgets for loading and inspecting one history.
package limits

import "fmt"

// Limits may be lowered for a caller, but never raised above Default's ceilings.
// Byte limits count payload, not Go object or allocator overhead.
type Limits struct {
	InputBytes    int64
	Nodes         int
	Entries       int
	StoredLines   int
	StateLines    int
	LineBytes     int
	OptionalBytes int
	TextBytes     int
	OutputBytes   int
}

// Default returns the inspection budgets documented in docs/undo-format.md.
func Default() Limits {
	return Limits{
		InputBytes:    64 << 20,
		Nodes:         100_000,
		Entries:       250_000,
		StoredLines:   1_000_000,
		StateLines:    1_000_000,
		LineBytes:     1 << 20,
		OptionalBytes: 1 << 20,
		TextBytes:     128 << 20,
		OutputBytes:   16 << 20,
	}
}

// Validate rejects unset, negative, and excessive budgets. Zero is not unlimited.
func (l Limits) Validate() error {
	max := Default()
	bounds := []struct {
		name string
		got  int64
		max  int64
	}{
		{"undo input bytes", l.InputBytes, max.InputBytes},
		{"history nodes", int64(l.Nodes), int64(max.Nodes)},
		{"entries", int64(l.Entries), int64(max.Entries)},
		{"stored lines", int64(l.StoredLines), int64(max.StoredLines)},
		{"state lines", int64(l.StateLines), int64(max.StateLines)},
		{"line bytes", int64(l.LineBytes), int64(max.LineBytes)},
		{"optional-field bytes", int64(l.OptionalBytes), int64(max.OptionalBytes)},
		{"decoded text bytes", int64(l.TextBytes), int64(max.TextBytes)},
		{"output bytes", int64(l.OutputBytes), int64(max.OutputBytes)},
	}

	for _, bound := range bounds {
		if bound.got <= 0 || bound.got > bound.max {
			return fmt.Errorf("%s budget must be between 1 and %d", bound.name, bound.max)
		}
	}

	return nil
}
