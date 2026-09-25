package tui

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nuggocto/xunhen/internal/history"
	"github.com/nuggocto/xunhen/internal/limits"
	"github.com/nuggocto/xunhen/internal/undofile"
)

// rootOnly returns an undo file with no changes whose reference state is
// lines: the retained root is the only node.
func rootOnly(lines []string) []byte {
	undo, _ := chain([][]string{lines})
	return undo
}

// chain returns a format 3 undo file, laid out as docs/undo-format.md
// describes, for a linear history through states: the root holds states[0],
// node k holds states[k], and the last node is the reference, whose text is
// the returned base. Each node's entry faces undo and swaps the whole buffer
// for the previous state. Lines must not contain NUL, which the reference
// hash would store as LF.
func chain(states [][]string) (undo []byte, base []string) {
	base = states[len(states)-1]
	hash := sha256.New()
	for _, line := range base {
		hash.Write([]byte(line))
		hash.Write([]byte{0})
	}

	last := uint32(len(states) - 1)
	oldest := min(last, 1)
	b := append([]byte("Vim\x9fUnDo\xe5\x00\x03"), hash.Sum(nil)...)
	for _, value := range []uint32{uint32(len(base)), 0, 0, 0, oldest, last, 0, last, last, last} {
		b = binary.BigEndian.AppendUint32(b, value)
	}
	b = binary.BigEndian.AppendUint64(b, 0)
	b = append(b, 0) // absent optional file metadata

	for k := uint32(1); k <= last; k++ {
		child := k + 1
		if k == last {
			child = 0
		}

		b = binary.BigEndian.AppendUint16(b, 0x5fd0)
		for _, value := range []uint32{k - 1, child, 0, 0, k} {
			b = binary.BigEndian.AppendUint32(b, value)
		}
		b = append(b, make([]byte, 12)...)
		b = binary.BigEndian.AppendUint32(b, ^uint32(0)) // cursor virtual column -1
		b = append(b, make([]byte, 2+26*12+32)...)
		b = binary.BigEndian.AppendUint64(b, uint64(1_700_000_000+k))
		b = append(b, 0) // absent optional change metadata

		// Top 0 and bottom 0 span the whole buffer.
		previous := states[k-1]
		b = binary.BigEndian.AppendUint16(b, 0xf518)
		for _, value := range []uint32{0, 0, 0, uint32(len(previous))} {
			b = binary.BigEndian.AppendUint32(b, value)
		}
		for _, line := range previous {
			b = binary.BigEndian.AppendUint32(b, uint32(len(line)))
			b = append(b, line...)
		}
		b = append(b, 0x35, 0x81, 0x35, 0x81) // ends of the text and extmark lists
	}

	return append(b, 0xe7, 0xaa), base
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
