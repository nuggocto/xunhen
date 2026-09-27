package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
)

// noticeHeader opens the notices file. The file is generated, so its text
// lives here.
const noticeHeader = `Third-party notices for xunhen

xunhen is licensed under the Apache License, Version 2.0; see LICENSE.
The xunhen executable also contains the Go runtime and standard library
and the Go modules listed below. Each section reproduces the license
files that the module distributes.

This file is generated from the modules linked into cmd/xunhen for
linux/amd64. Regenerate it with tools/release.sh notices; do not edit it.
`

// licenseFile reports whether a module's top-level file is license
// material worth reproducing, and whether it is a license proper.
func licenseFile(name string) (notice, license bool) {
	upper := strings.ToUpper(name)
	for _, prefix := range []string{"LICENSE", "LICENCE", "COPYING"} {
		if strings.HasPrefix(upper, prefix) {
			return true, true
		}
	}
	for _, prefix := range []string{"NOTICE", "PATENTS"} {
		if strings.HasPrefix(upper, prefix) {
			return true, false
		}
	}
	return false, false
}

// generateNotices reproduces the license files of the Go distribution and
// of every module linked into the executable that info describes. The
// module list comes from the executable itself, so a module that only a
// test or a development tool imports never appears, and a new runtime
// dependency cannot be missed.
func generateNotices(ctx context.Context, dir string, env []string, info *debug.BuildInfo) ([]byte, error) {
	goroot, err := goCommand(ctx, dir, env, "env", "GOROOT")
	if err != nil {
		return nil, err
	}

	var out bytes.Buffer
	out.WriteString(noticeHeader)
	if err := writeNotice(&out, "The Go runtime and standard library (https://go.dev)", strings.TrimSpace(string(goroot))); err != nil {
		return nil, err
	}

	deps := slices.Clone(info.Deps)
	slices.SortFunc(deps, func(a, b *debug.Module) int { return strings.Compare(a.Path, b.Path) })
	if len(deps) == 0 {
		return out.Bytes(), nil
	}

	args := []string{"mod", "download", "-json"}
	for _, dep := range deps {
		if dep.Replace != nil {
			return nil, fmt.Errorf("dependency %s was replaced", dep.Path)
		}
		args = append(args, dep.Path+"@"+dep.Version)
	}
	listing, err := goCommand(ctx, dir, env, args...)
	if err != nil {
		return nil, err
	}

	dirs := map[string]string{}
	decoder := json.NewDecoder(bytes.NewReader(listing))
	for {
		var m struct{ Path, Version, Dir, Error string }
		if err := decoder.Decode(&m); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("read module download listing: %w", err)
		}
		if m.Error != "" || m.Dir == "" {
			return nil, fmt.Errorf("download %s@%s: %s", m.Path, m.Version, m.Error)
		}
		dirs[m.Path+"@"+m.Version] = m.Dir
	}

	for _, dep := range deps {
		key := dep.Path + "@" + dep.Version
		moduleDir, ok := dirs[key]
		if !ok {
			return nil, fmt.Errorf("no download for %s", key)
		}
		if err := writeNotice(&out, key, moduleDir); err != nil {
			return nil, err
		}
	}
	return out.Bytes(), nil
}

// writeNotice appends one section with every license file at the top of
// dir. A module with no license file fails the build, since nothing would
// show that it may be redistributed.
func writeNotice(out *bytes.Buffer, title, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	rule := strings.Repeat("=", 72)
	fmt.Fprintf(out, "\n%s\n%s\n%s\n", rule, title, rule)
	licensed := false
	for _, entry := range entries {
		notice, license := licenseFile(entry.Name())
		if !notice || !entry.Type().IsRegular() {
			continue
		}
		licensed = licensed || license
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "\n--- %s\n\n", entry.Name())
		out.Write(bytes.TrimRight(data, "\n"))
		out.WriteByte('\n')
	}
	if !licensed {
		return fmt.Errorf("%s has no license file in %s", title, dir)
	}
	return nil
}
