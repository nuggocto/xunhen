package discover

import (
	"slices"
	"testing"
)

func reports(outcomes ...Outcome) []Report {
	out := make([]Report, len(outcomes))
	for i, o := range outcomes {
		out[i] = Report{Outcome: o}
	}

	return out
}

func TestChoose(t *testing.T) {
	t.Parallel()

	searched := []Scope{{Status: Searched}}
	tests := []struct {
		name            string
		reports         []Report
		scope           []Scope
		allowUnverified bool
		noBase          bool
		index           int
		problem         Problem
	}{
		{name: "nothing found", scope: searched, index: -1, problem: NotFound},
		{name: "one verified", reports: reports(Verified), scope: searched, index: 0},
		{name: "verified among rejected", reports: reports(Rejected, Verified), scope: searched, index: 1},
		{name: "verified with a duplicate", reports: reports(Verified, Duplicate), scope: searched, index: 0},
		{name: "two verified", reports: reports(Verified, Verified), scope: searched, index: -1, problem: Ambiguous},
		{name: "verified before an unexamined candidate", reports: reports(Verified, Unexamined), scope: searched, index: -1, problem: Incomplete},
		{name: "verified with an unavailable directory", reports: reports(Verified), scope: []Scope{{Status: Searched}, {Status: Unavailable}}, index: -1, problem: Incomplete},
		{name: "only a mismatch", reports: reports(Unverified), scope: searched, index: -1, problem: Mismatch},
		{name: "only unverified without a base", reports: reports(Unverified), scope: searched, noBase: true, index: -1, problem: NoBase},
		{name: "inspect a lone unverified history", reports: reports(Unverified, Rejected), scope: searched, allowUnverified: true, index: 0},
		{name: "inspect two unverified histories", reports: reports(Unverified, Unverified), scope: searched, allowUnverified: true, index: -1, problem: Ambiguous},
		{name: "inspection prefers the verified history", reports: reports(Unverified, Verified), scope: searched, allowUnverified: true, index: 1},
		{name: "only rejected", reports: reports(Rejected, Rejected), scope: searched, index: -1, problem: Invalid},
		{name: "repeated directory", reports: reports(Verified), scope: []Scope{{Status: Searched}, {Status: Repeated}}, index: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			index, problem := choose(tt.reports, tt.scope, tt.allowUnverified, tt.noBase)
			if index != tt.index || problem != tt.problem {
				t.Fatalf("choose = %d, %d; want %d, %d", index, problem, tt.index, tt.problem)
			}
		})
	}
}

// FuzzChoose checks the selection invariants over arbitrary outcomes: a
// choice needs a complete search, picks a qualifying report, is the only
// qualifying report of its kind, and does not depend on report order.
func FuzzChoose(f *testing.F) {
	f.Add([]byte{1, 3, 5}, byte(0), false, false)
	f.Add([]byte{2, 2}, byte(0), true, false)
	f.Add([]byte{1, 4}, byte(2), false, true)

	f.Fuzz(func(t *testing.T, raw []byte, dirStatus byte, allowUnverified, noBase bool) {
		if len(raw) > 64 {
			t.Skip()
		}

		outcomes := make([]Outcome, len(raw))
		for i, b := range raw {
			outcomes[i] = Outcome(1 + b%5)
		}
		scope := []Scope{{Status: Searched}, {Status: DirStatus(1 + dirStatus%3)}}
		list := reports(outcomes...)

		index, problem := choose(list, scope, allowUnverified, noBase)
		if (index >= 0) == (problem != 0) {
			t.Fatalf("choose = %d, %d: exactly one of a choice or a problem is required", index, problem)
		}

		incomplete := slices.Contains(outcomes, Unexamined) || scope[1].Status == Unavailable
		if incomplete && problem != Incomplete {
			t.Fatalf("incomplete search gave %d, %d", index, problem)
		}
		if index < 0 {
			return
		}

		chosen := outcomes[index]
		verified := countOf(outcomes, Verified)
		switch {
		case chosen == Verified && verified == 1:
		case chosen == Unverified && allowUnverified && verified == 0 && countOf(outcomes, Unverified) == 1:
		default:
			t.Fatalf("chose %v at %d from %v", chosen, index, outcomes)
		}

		// Reversing the reports must choose the same report.
		reversed := slices.Clone(list)
		slices.Reverse(reversed)
		again, _ := choose(reversed, scope, allowUnverified, noBase)
		if again != len(list)-1-index {
			t.Fatalf("choice depends on report order: %d, then %d reversed", index, again)
		}
	})
}

func countOf(outcomes []Outcome, want Outcome) int {
	n := 0
	for _, o := range outcomes {
		if o == want {
			n++
		}
	}

	return n
}
