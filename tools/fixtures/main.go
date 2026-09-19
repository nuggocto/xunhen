// Command fixtures creates reference histories with the pinned Neovim build.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	os.Exit(run())
}

func run() int {
	const usage = "usage: go run ./tools/fixtures -nvim /path/to/nvim -out /new/directory\n"

	flags := flag.NewFlagSet("fixtures", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	nvim := flags.String("nvim", "", "absolute path to the pinned Neovim executable")
	out := flags.String("out", "", "new output directory")

	if err := flags.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			if _, err := io.WriteString(os.Stdout, usage); err != nil {
				return 1
			}
			return 0
		}

		fmt.Fprintf(os.Stderr, "fixtures: %q\n", err.Error())
		return 2
	}

	if *nvim == "" || *out == "" || flags.NArg() != 0 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	if err := generate(ctx, *nvim, *out); err != nil {
		fmt.Fprintf(os.Stderr, "fixtures: %q\n", err.Error())
		return 1
	}

	_, err := fmt.Fprintf(os.Stdout, "generated %d synthetic histories in %q\n", len(corpusCases()), *out)
	if err != nil {
		return 1
	}
	return 0
}
