// Command release builds xunhen's release artifacts from one commit: the
// Linux/amd64 binary archive, the source archive, their checksums, and a
// provenance record. The archives are reproducible: the same commit and
// toolchain give the same bytes anywhere. Run it through tools/release.sh,
// which fixes the go command's environment first.
//
// Usage:
//
//	tools/release.sh build [-tag vX.Y.Z] [-out DIR]
//	tools/release.sh notices [-check]
//
// Without -tag, build makes a snapshot whose version names its commit and
// can never be mistaken for a release. notices regenerates
// THIRD_PARTY_NOTICES.txt from the modules linked into the executable; with
// -check it only reports whether the committed file is current.
package main

import (
	"bytes"
	"context"
	"debug/buildinfo"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

const usage = `usage:
  tools/release.sh build [-tag vX.Y.Z] [-out DIR]
  tools/release.sh notices [-check]
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "release: %v\n", err)
		return 1
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		fmt.Fprintln(stderr, "release: run from the repository root")
		return 2
	}

	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	switch args[0] {
	case "build":
		tag := flags.String("tag", "", "release tag to build; it must name HEAD and match VERSION")
		out := flags.String("out", "", "new directory for the artifacts (default dist/VERSION)")
		if flags.Parse(args[1:]) != nil || flags.NArg() != 0 {
			fmt.Fprint(stderr, usage)
			return 2
		}
		b, err := build(ctx, options{root: root, tag: *tag, out: *out, getenv: os.Getenv, log: stderr})
		if err != nil {
			fmt.Fprintf(stderr, "release: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "Built %s in %s\n%s", b.version, b.out, b.sums)
		return 0

	case "notices":
		check := flags.Bool("check", false, "report whether the committed file is current instead of writing it")
		if flags.Parse(args[1:]) != nil || flags.NArg() != 0 {
			fmt.Fprint(stderr, usage)
			return 2
		}
		if err := notices(ctx, root, os.Getenv, *check); err != nil {
			fmt.Fprintf(stderr, "release: %v\n", err)
			return 1
		}
		return 0

	default:
		fmt.Fprint(stderr, usage)
		return 2
	}
}

// notices builds the executable from the working tree and writes, or with
// check compares, the notices for the modules linked into it.
func notices(ctx context.Context, root string, getenv func(string) string, check bool) error {
	if err := checkEnvironment(getenv); err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "xunhen-notices-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	env := buildEnv(getenv)
	executable := filepath.Join(dir, "xunhen")
	if _, err := goCommand(ctx, root, env, "build", "-trimpath", "-buildvcs=false", "-o", executable, "./cmd/xunhen"); err != nil {
		return err
	}
	info, err := buildinfo.ReadFile(executable)
	if err != nil {
		return err
	}
	generated, err := generateNotices(ctx, root, env, info)
	if err != nil {
		return err
	}

	path := filepath.Join(root, noticesFile)
	if !check {
		return os.WriteFile(path, generated, 0o644)
	}
	committed, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if !bytes.Equal(committed, generated) {
		return fmt.Errorf("%s is out of date; run tools/release.sh notices and commit the result", noticesFile)
	}
	return nil
}
