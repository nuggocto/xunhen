package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
)

func validateOracle(fixture fixtureCase, result oracle, undo []byte) error {
	if result.Name != fixture.Name {
		return errors.New("oracle name does not match the fixture")
	}
	if result.Purpose != fixture.Purpose {
		return errors.New("oracle purpose does not match the fixture")
	}
	if result.Classification != fixture.classification() {
		return errors.New("oracle classification does not match the fixture")
	}
	if !reflect.DeepEqual(result.Recipe, fixtureRequest(fixture)) {
		return errors.New("oracle recipe does not match the fixture")
	}
	if !result.Loaded.MismatchRejected {
		return errors.New("base-mismatch check failed")
	}
	if !reflect.DeepEqual(result.Created.Tree, result.Loaded.Tree) {
		return errors.New("persisted undo tree changed across process restart")
	}

	before := result.Created.Anchor
	after := result.Loaded.Anchor
	if before.Seq != after.Seq || !slices.Equal(before.LinesHex, after.LinesHex) {
		return errors.New("base state changed across process restart")
	}
	if len(result.Created.Observations) != len(fixture.Steps)+1 {
		return errors.New("creation observations are incomplete")
	}

	parents, err := treeParents(result.Loaded.Tree.Entries)
	if err != nil {
		return err
	}
	if !maps.Equal(parents, fixture.Parents) {
		return fmt.Errorf("observed parents = %v, want %v", parents, fixture.Parents)
	}

	states := result.Loaded.States
	if len(states) != len(fixture.Visit) {
		return errors.New("replay observations are incomplete")
	}

	seen := make(map[int]bool)
	for i, state := range states {
		if state.Seq != fixture.Visit[i] {
			return errors.New("oracle visited states in the wrong order")
		}
		if err := checkState(fixture, state); err != nil {
			return err
		}
		seen[state.Seq] = true
	}
	if len(seen) != len(fixture.States) {
		return errors.New("not every expected state was visited")
	}
	if err := checkState(fixture, after); err != nil {
		return err
	}
	if err := checkBase(after, undo, result.Recipe.Persistence); err != nil {
		return err
	}

	if fixture.BaseOnlyText != "" {
		if bytes.Contains(undo, []byte(fixture.BaseOnlyText)) {
			return errors.New("missing-base example unexpectedly stores the unchanged anchor text")
		}
	}
	return nil
}

func checkState(fixture fixtureCase, state snapshot) error {
	want, found := fixture.States[state.Seq]
	if !found || len(state.LinesHex) != len(want) {
		return fmt.Errorf("unexpected state or line count at sequence %d", state.Seq)
	}

	for i, line := range want {
		if state.LinesHex[i] != hex.EncodeToString([]byte(line)) {
			return fmt.Errorf("sequence %d line %d = %s, want %x", state.Seq, i, state.LinesHex[i], line)
		}
	}
	return nil
}

// Check the file envelope and reference hash, not the undo records themselves.
func checkBase(anchor snapshot, undo []byte, persistence string) error {
	if len(undo) < 47 {
		return errors.New("truncated format envelope")
	}
	if !bytes.Equal(undo[:9], []byte("Vim\x9fUnDo\xe5")) {
		return errors.New("unexpected format envelope magic")
	}
	if binary.BigEndian.Uint16(undo[9:11]) != 3 {
		return errors.New("unsupported format envelope version")
	}
	if binary.BigEndian.Uint32(undo[43:47]) != uint32(len(anchor.LinesHex)) {
		return errors.New("stored base line count does not match the oracle")
	}

	storedHash := undo[11:43]
	if persistence == "automatic" && len(anchor.LinesHex) == 1 && anchor.LinesHex[0] == "" {
		// The automatic empty-buffer fixture omits the dummy line from its hash.
		emptyHash := sha256.Sum256(nil)
		if !bytes.Equal(emptyHash[:], storedHash) {
			return errors.New("stored base hash does not match the automatic empty buffer")
		}
		return nil
	}

	hash := sha256.New()
	for _, encoded := range anchor.LinesHex {
		line, err := hex.DecodeString(encoded)
		if err != nil {
			return err
		}

		// Internal lines use LF for NUL and end with a NUL terminator.
		hash.Write(bytes.ReplaceAll(line, []byte{0}, []byte{'\n'}))
		hash.Write([]byte{0})
	}
	if !bytes.Equal(hash.Sum(nil), storedHash) {
		return errors.New("stored base hash does not match Neovim's normalized lines")
	}
	return nil
}

// An undotree() entry's alt list shares its parent; the main list follows children.
func treeParents(entries []treeEntry) (map[int]int, error) {
	type branch struct {
		entries []treeEntry
		parent  int
	}

	stack := []branch{{entries: entries}}
	parents := make(map[int]int)

	for len(stack) > 0 {
		last := len(stack) - 1
		current := stack[last]
		stack = stack[:last]

		for _, entry := range current.entries {
			if _, duplicate := parents[entry.Seq]; duplicate {
				return nil, errors.New("duplicate sequence in fixture tree")
			}
			if entry.Seq <= current.parent {
				return nil, errors.New("invalid ancestry in fixture tree")
			}
			if len(parents) >= 64 {
				return nil, errors.New("fixture tree exceeds 64 nodes")
			}

			parents[entry.Seq] = current.parent
			if len(entry.Alt) > 0 {
				stack = append(stack, branch{
					entries: entry.Alt,
					parent:  current.parent,
				})
			}
			current.parent = entry.Seq
		}
	}
	return parents, nil
}
