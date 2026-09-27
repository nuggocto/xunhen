// Command workload generates synthetic undo histories for resource
// measurements and runs the built command against them. It is a development
// tool: its histories exercise limits and costs, not producer compatibility,
// which the Neovim corpus in testdata/undo establishes.
//
//	go run ./tools/workload list
//	go run ./tools/workload generate -out DIR [RECIPE...]
//	go run ./tools/workload run -bin PATH -dir DIR/RECIPE -samples N -out FILE
//	go run ./tools/workload summarize FILE...
//
// generate writes each recipe to DIR/RECIPE: history.undo, base.txt, and
// manifest.json with the recipe version, seed, counts, content hashes, and
// the digest of every probed state. run checks the command's output against
// those digests before it times anything, then appends one JSON line per
// measurement to the output file.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: workload list | generate -out DIR [RECIPE...] | run -bin PATH -dir DIR -samples N -out FILE | summarize FILE...")
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "list":
		for _, r := range recipes {
			fmt.Printf("%-14s v%d seed %-3d %s\n", r.Name, r.Version, r.Seed, r.Purpose)
		}
	case "generate":
		err = generate(os.Args[2:])
	case "run":
		err = run(os.Args[2:])
	case "summarize":
		err = summarize(os.Args[2:])
	default:
		err = fmt.Errorf("unknown subcommand %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "workload:", err)
		os.Exit(1)
	}
}

// manifest describes one generated workload.
type manifest struct {
	Recipe    string `json:"recipe"`
	Version   int    `json:"version"`
	Seed      uint64 `json:"seed"`
	Purpose   string `json:"purpose"`
	Generator string `json:"generator"`

	Nodes          int   `json:"nodes"`
	Entries        int   `json:"entries"`
	StoredLines    int   `json:"stored_lines"`
	LongestLine    int   `json:"longest_line"`
	ReferenceLines int   `json:"reference_lines"`
	ReferenceBytes int   `json:"reference_bytes"`
	UndoBytes      int64 `json:"undo_bytes"`

	UndoSHA256 string `json:"undo_sha256"`
	BaseSHA256 string `json:"base_sha256"`

	Probes  []probe  `json:"probes"`
	Compare [2]int32 `json:"compare"`
}

func generate(args []string) error {
	flags := flag.NewFlagSet("generate", flag.ContinueOnError)
	out := flags.String("out", "", "directory to write workloads into")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return errors.New("generate needs -out DIR")
	}

	names := flags.Args()
	if len(names) == 0 {
		for _, r := range recipes {
			names = append(names, r.Name)
		}
	}
	for _, name := range names {
		r, ok := findRecipe(name)
		if !ok {
			return fmt.Errorf("unknown recipe %q", name)
		}
		if err := generateOne(r, filepath.Join(*out, r.Name)); err != nil {
			return fmt.Errorf("%s: %w", r.Name, err)
		}
		fmt.Println("generated", r.Name)
	}
	return nil
}

func generateOne(r recipe, dir string) error {
	w, err := r.build(r.Seed)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	m := manifest{
		Recipe: r.Name, Version: r.Version, Seed: r.Seed, Purpose: r.Purpose,
		Generator: runtime.Version(),
		Nodes:     len(w.file.Nodes) + 1, // the retained root is a node too
		Probes:    w.probes, Compare: w.compare,
	}
	for _, n := range w.file.Nodes {
		m.Entries += len(n.Entries)
		for _, e := range n.Entries {
			m.StoredLines += len(e.Lines)
			for _, line := range e.Lines {
				m.LongestLine = max(m.LongestLine, len(line))
			}
		}
	}
	for _, line := range w.file.Reference {
		m.ReferenceBytes += len(line) + 1
		m.LongestLine = max(m.LongestLine, len(line))
	}
	m.ReferenceLines = len(w.file.Reference)

	undoHash, undoBytes, err := writeHashed(filepath.Join(dir, "history.undo"), w.file.WriteTo)
	if err != nil {
		return err
	}
	baseHash, _, err := writeHashed(filepath.Join(dir, "base.txt"), func(out io.Writer) (int64, error) {
		var n int64
		for _, line := range w.file.Reference {
			k, err := io.WriteString(out, line+"\n")
			n += int64(k)
			if err != nil {
				return n, err
			}
		}
		return n, nil
	})
	if err != nil {
		return err
	}
	m.UndoSHA256, m.UndoBytes, m.BaseSHA256 = undoHash, undoBytes, baseHash

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "manifest.json"), append(data, '\n'), 0o644)
}

// writeHashed writes a file through a hash and returns its SHA-256 and size.
func writeHashed(path string, write func(io.Writer) (int64, error)) (string, int64, error) {
	f, err := os.Create(path)
	if err != nil {
		return "", 0, err
	}
	h := sha256.New()
	n, err := write(io.MultiWriter(f, h))
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return hex.EncodeToString(h.Sum(nil)), n, err
}

func readManifest(dir string) (manifest, error) {
	var m manifest
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, err
	}
	r, ok := findRecipe(m.Recipe)
	switch {
	case !ok:
		return m, fmt.Errorf("unknown recipe %q", m.Recipe)
	case r.Version != m.Version:
		return m, fmt.Errorf("%s was generated by recipe version %d; this tool has %d", m.Recipe, m.Version, r.Version)
	}
	return m, nil
}
