// Package limits defines the finite ceilings for loading, recovering, and
// comparing states of one history.
package limits

import "fmt"

// Limits may be lowered for a caller, but never raised above Default's
// ceilings. Each ceiling is a safety limit sized so that ordinary files never
// reach it; work that these sizes already bound, such as replay and diff
// search, has no limit of its own. Byte limits count payload, not Go object
// or allocator overhead.
type Limits struct {
	InputBytes  int64
	BaseBytes   int64
	Nodes       int
	Entries     int
	StoredLines int
	StateLines  int
	StateBytes  int
	LineBytes   int
}

// Default returns the ceilings documented in docs/undo-format.md. At these
// sizes one command stays under about 1 GiB of memory on the worst inputs
// measured there.
func Default() Limits {
	return Limits{
		InputBytes:  256 << 20,
		BaseBytes:   64 << 20,
		Nodes:       1_000_000,
		Entries:     1_000_000,
		StoredLines: 4_000_000,
		StateLines:  4_000_000,
		StateBytes:  64 << 20,
		LineBytes:   16 << 20,
	}
}

// Validate rejects unset, negative, and excessive limits. Zero is not unlimited.
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
	}

	for _, bound := range bounds {
		if bound.got <= 0 || bound.got > bound.ceiling {
			return fmt.Errorf("%s limit must be between 1 and %d", bound.name, bound.ceiling)
		}
	}

	return nil
}
