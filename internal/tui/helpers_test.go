package tui

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nuggocto/xunhen/internal/history"
	"github.com/nuggocto/xunhen/internal/limits"
	"github.com/nuggocto/xunhen/internal/synth"
	"github.com/nuggocto/xunhen/internal/undofile"
)

// rootOnly returns an undo file with no changes whose reference state is
// lines: the retained root is the only node.
func rootOnly(lines []string) []byte {
	undo, _ := chain([][]string{lines})
	return undo
}

// chain returns a linear history through states, whose last state is the
// reference text the returned base must match.
func chain(states [][]string) (undo []byte, base []string) {
	f := synth.Chain(states)
	undo, err := f.Bytes()
	if err != nil {
		panic(err)
	}
	return undo, f.Reference
}

// loaderOf returns a loader that decodes and validates the inputs afresh on
// every call, as the command's loader does.
func loaderOf(undo []byte, base []string) Loader {
	return func(ctx context.Context) (*Loaded, error) {
		lim := limits.Default()
		file, err := undofile.Decode(ctx, "history.undo", bytes.NewReader(undo), lim)
		if err != nil {
			return nil, err
		}
		h, err := history.New(ctx, file, lim)
		if err != nil {
			return nil, err
		}
		verified, err := undofile.VerifyBase(ctx, file, "base", base, lim)
		if err != nil {
			return nil, err
		}
		r, err := history.Bind(h, verified, lim)
		if err != nil {
			return nil, err
		}

		labels := Labels{Undo: "history.undo", Base: "retry.go", Inputs: []string{"--undo", "history.undo", "--base", "retry.go"}}
		return &Loaded{History: h, Reconstructor: r, Labels: labels}, nil
	}
}

// fixture is a checked-in Neovim history with its independently recorded
// states, which come from Neovim rather than from xunhen.
type fixture struct {
	undo   []byte
	base   []string
	states map[history.NodeID][]string
}

func readFixture(t *testing.T, name string) fixture {
	t.Helper()

	dir := filepath.Join("..", "..", "testdata", "undo", name)
	undo, err := os.ReadFile(filepath.Join(dir, "history.undo"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "oracle.json"))
	if err != nil {
		t.Fatal(err)
	}

	type state struct {
		Seq      history.NodeID `json:"seq"`
		LinesHex []string       `json:"lines_hex"`
	}
	var oracle struct {
		Loaded struct {
			Anchor state   `json:"anchor"`
			States []state `json:"states"`
		} `json:"loaded"`
	}
	if err := json.Unmarshal(data, &oracle); err != nil {
		t.Fatal(err)
	}

	decode := func(encoded []string) []string {
		lines := make([]string, len(encoded))
		for i, h := range encoded {
			raw, err := hex.DecodeString(h)
			if err != nil {
				t.Fatal(err)
			}
			lines[i] = string(raw)
		}
		return lines
	}

	f := fixture{undo: undo, base: decode(oracle.Loaded.Anchor.LinesHex), states: map[history.NodeID][]string{}}
	for _, s := range oracle.Loaded.States {
		f.states[s.Seq] = decode(s.LinesHex)
	}

	return f
}

func (f fixture) loader() Loader {
	return loaderOf(f.undo, f.base)
}

// load runs a loader and builds its session the way the worker does.
func load(t *testing.T, loader Loader) *session {
	t.Helper()

	e := &engine{load: loader, limits: limits.Default(), cache: newCache(cacheBudget)}
	s, err := e.loadSession(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	return s
}

// ref returns the reference to a node ID in a session.
func ref(t *testing.T, s *session, id history.NodeID) history.NodeRef {
	t.Helper()

	r, err := s.history.Lookup(id)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
