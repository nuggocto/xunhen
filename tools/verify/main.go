// Command verify checks a built xunhen executable, or a release binary
// archive, as a user would receive it. It never compiles anything: it runs
// the exact file it is given, so the same checks apply to the release
// archive, the Nix package, and the Arch package.
//
// Usage:
//
//	go run ./tools/verify -binary PATH -version V -commit SHA -go GOVERSION
//	go run ./tools/verify -archive xunhen_V_linux_amd64.tar.gz [-sums SHA256SUMS.txt] -version V -commit SHA -go GOVERSION
//
// Expected recovered text comes from the Neovim oracle files under -corpus,
// never from xunhen's own code. Every command runs with an empty PATH and a
// private HOME, on copies of the fixtures, and the copies must be unchanged
// afterwards. Run it from the repository root, or pass -corpus and -gosum.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	var c config
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&c.binary, "binary", "", "executable to verify")
	flags.StringVar(&c.archive, "archive", "", "release binary archive to verify, instead of -binary")
	flags.StringVar(&c.sums, "sums", "", "SHA256SUMS.txt that must list the archive")
	flags.StringVar(&c.corpus, "corpus", "testdata/undo", "directory of Neovim oracle fixtures")
	flags.StringVar(&c.gosum, "gosum", "go.sum", "go.sum the executable's modules must appear in")
	flags.StringVar(&c.version, "version", "", "version the executable must report, such as v1.0.0")
	flags.StringVar(&c.commit, "commit", "", "commit the executable must report")
	flags.StringVar(&c.goVersion, "go", "", "Go toolchain the executable must be built with, such as go1.27.1")
	flags.StringVar(&c.buildmode, "buildmode", "exe", "build mode the executable must record: exe or pie")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	err := c.validate()
	if err == nil && flags.NArg() != 0 {
		err = fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}
	if err != nil {
		fmt.Fprintf(stderr, "verify: %v\n", err)
		flags.Usage()
		return 2
	}

	results := verify(ctx, c)
	failed := 0
	for _, r := range results {
		if r.err != nil {
			failed++
			fmt.Fprintf(stdout, "FAIL  %s: %v\n", r.name, r.err)
		} else {
			fmt.Fprintf(stdout, "ok    %s%s\n", r.name, r.detail)
		}
	}
	if failed != 0 {
		fmt.Fprintf(stdout, "%d of %d checks failed\n", failed, len(results))
		return 1
	}
	fmt.Fprintf(stdout, "all %d checks passed\n", len(results))
	return 0
}

// config names what to verify and what it must be.
type config struct {
	binary, archive, sums string
	corpus, gosum         string
	version, commit       string
	goVersion, buildmode  string
}

func (c config) validate() error {
	switch {
	case (c.binary == "") == (c.archive == ""):
		return fmt.Errorf("give exactly one of -binary and -archive")
	case c.sums != "" && c.archive == "":
		return fmt.Errorf("-sums applies to -archive")
	case c.version == "" || c.commit == "" || c.goVersion == "":
		return fmt.Errorf("-version, -commit, and -go are required")
	case c.buildmode != "exe" && c.buildmode != "pie":
		return fmt.Errorf("-buildmode must be exe or pie")
	}
	return nil
}

// result is the outcome of one named check.
type result struct {
	name   string
	detail string
	err    error
}
