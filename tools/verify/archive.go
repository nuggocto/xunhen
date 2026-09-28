package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nuggocto/xunhen/tools/internal/layout"
	"github.com/nuggocto/xunhen/tools/internal/tarball"
)

// maxArchiveBytes bounds the archive the verifier reads into memory. A
// release archive is a few megabytes.
const maxArchiveBytes = 128 << 20

// checkArchive validates a binary archive, compares its checksum with
// SHA256SUMS.txt when given, and extracts its executable to a new private
// directory for the remaining checks. The archive must hold exactly the
// documented files with the documented modes, under a top directory named
// for the expected version.
func checkArchive(c config) (string, string, error) {
	data, err := readArchive(c.archive)
	if err != nil {
		return "", "", err
	}
	digest := sha256.Sum256(data)
	name := filepath.Base(c.archive)
	if c.sums != "" {
		if err := checkSum(c.sums, name, hex.EncodeToString(digest[:])); err != nil {
			return "", "", err
		}
	}

	prefix := layout.BinaryArchive(c.version)
	if name != prefix+".tar.gz" {
		return "", "", fmt.Errorf("archive is named %q, want %q for version %s", name, prefix+".tar.gz", c.version)
	}
	a, err := tarball.Read(bytes.NewReader(data))
	if err != nil {
		return "", "", err
	}
	if a.Prefix != prefix {
		return "", "", fmt.Errorf("archive top directory is %q, want %q", a.Prefix, prefix)
	}

	want := append([]string{layout.Executable}, layout.BinaryDocs...)
	var got []string
	for _, f := range a.Files {
		got = append(got, f.Name)
		switch {
		case f.Name == layout.Executable && !f.Executable:
			return "", "", fmt.Errorf("%s is not executable in the archive", f.Name)
		case f.Name != layout.Executable && f.Executable:
			return "", "", fmt.Errorf("%s is executable in the archive", f.Name)
		case len(f.Data) == 0:
			return "", "", fmt.Errorf("%s is empty in the archive", f.Name)
		}
	}
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		return "", "", fmt.Errorf("archive holds %s, want exactly %s", strings.Join(got, ", "), strings.Join(want, ", "))
	}
	for _, dir := range a.Dirs {
		if dir != prefix && dir != prefix+"/docs" {
			return "", "", fmt.Errorf("unexpected directory %s in the archive", dir)
		}
	}

	exe, _ := a.Lookup(layout.Executable)
	dir, err := os.MkdirTemp("", "xunhen-archive-")
	if err != nil {
		return "", "", err
	}
	path := filepath.Join(dir, layout.Executable)
	if err := os.WriteFile(path, exe.Data, 0o755); err != nil {
		return "", "", err
	}
	detail := fmt.Sprintf(" (sha256 %x, %d files, executable sha256 %x)", digest, len(a.Files), sha256.Sum256(exe.Data))
	return path, detail, nil
}

// checkSum finds name in a SHA256SUMS.txt file and compares its hash.
func checkSum(path, name, sum string) error {
	data, err := readAll(path)
	if err != nil {
		return err
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		hash, file, ok := strings.Cut(scanner.Text(), "  ")
		if !ok || file != name {
			continue
		}
		if hash != sum {
			return fmt.Errorf("%s has sha256 %s, but %s lists %s", name, sum, filepath.Base(path), hash)
		}
		return nil
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return fmt.Errorf("%s does not list %s", filepath.Base(path), name)
}

// readArchive reads a regular file of at most maxArchiveBytes. It checks
// the size before reading and reads through a limit, so a larger file, or
// one that grows while it is read, never gets past the limit into memory.
func readArchive(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	if info.Size() > maxArchiveBytes {
		return nil, fmt.Errorf("%s is larger than %d bytes", path, maxArchiveBytes)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxArchiveBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxArchiveBytes {
		return nil, fmt.Errorf("%s grew past %d bytes while it was read", path, maxArchiveBytes)
	}
	return data, nil
}
