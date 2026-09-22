package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/nuggocto/xunhen/internal/limits"
	"github.com/nuggocto/xunhen/internal/termtext"
)

const (
	exitSuccess     = 0
	exitFailure     = 1
	exitUsage       = 2
	exitInterrupted = 130
)

const helpText = `xunhen - seek traces in Neovim's saved undo history

Usage:
  xunhen [--help | --version]
  xunhen help [command]
  xunhen version
  xunhen inspect --undo PATH
  xunhen show --undo PATH --base PATH --node ID [--raw --final-newline=include|omit]

Available commands:
  help       Show help
  version    Show version and build information
  inspect    Describe an undo history
  show       Reconstruct a retained state

Planned commands (not available in this build):
  diff       Compare two retained states
  browse     Explore a history in the terminal

This development build provides history inspection and state recovery.
`

const versionHelp = "Usage: xunhen version\nShow version and build information.\n"

// commandHelp answers "xunhen help COMMAND". Each command also accepts --help
// on its own.
var commandHelp = map[string]string{
	"help":    helpText,
	"version": versionHelp,
	"inspect": inspectHelp,
	"show":    showHelp,
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if err := ctx.Err(); err != nil {
		return operationError(stderr, err)
	}
	if len(args) == 0 {
		return writeOutput(stdout, stderr, helpText)
	}

	switch args[0] {
	case "-h", "--help":
		if len(args) != 1 {
			return diagnostic(stderr, exitUsage, "help does not accept extra arguments")
		}
		return writeOutput(stdout, stderr, helpText)

	case "help":
		if len(args) == 1 {
			return writeOutput(stdout, stderr, helpText)
		}
		if len(args) != 2 {
			return diagnostic(stderr, exitUsage, "help accepts at most one command")
		}

		if text, ok := commandHelp[args[1]]; ok {
			return writeOutput(stdout, stderr, text)
		}
		if args[1] == "diff" || args[1] == "browse" {
			return writeOutput(stdout, stderr, args[1]+" is planned and not available in this build.\n")
		}
		return diagnostic(stderr, exitUsage, "unknown command; see 'xunhen --help'")

	case "version", "--version":
		if len(args) != 1 {
			return diagnostic(stderr, exitUsage, "version does not accept extra arguments")
		}
		return writeOutput(stdout, stderr, versionText())

	case "inspect":
		return inspect(ctx, args[1:], stdout, stderr)
	case "show":
		return show(ctx, args[1:], stdout, stderr)

	case "diff", "browse":
		// Future command arguments are deliberately not parsed or opened yet.
		return diagnostic(stderr, exitFailure, args[0]+" is not available in this build")

	default:
		// Unknown arguments can contain terminal controls. Do not echo them.
		return diagnostic(stderr, exitUsage, "unknown command or option; see 'xunhen --help'")
	}
}

func writeOutput(stdout, stderr io.Writer, text string) int {
	// A short write always returns an error, so err alone covers partial output.
	if _, err := io.WriteString(stdout, text); err != nil {
		return diagnostic(stderr, exitFailure, "cannot write output")
	}

	return exitSuccess
}

func diagnostic(stderr io.Writer, status int, message string) int {
	const prefix = "xunhen: "
	safe, err := termtext.Escape(context.Background(), message, limits.Default().OutputBytes-len(prefix+"\n"))
	if err != nil {
		safe = "diagnostic exceeds output byte budget"
		status = exitFailure
	}

	if _, err := io.WriteString(stderr, prefix+safe+"\n"); err != nil {
		return exitFailure
	}

	return status
}

// operationError maps cancellation to the interrupt status and everything else
// to an operational failure.
func operationError(stderr io.Writer, err error) int {
	if errors.Is(err, context.Canceled) {
		return diagnostic(stderr, exitInterrupted, "interrupted")
	}

	return diagnostic(stderr, exitFailure, err.Error())
}

// helpRequested reports whether a command's only argument asks for its help.
func helpRequested(args []string) bool {
	return len(args) == 1 && (args[0] == "--help" || args[0] == "-h")
}

// newFlags returns a command flag set whose errors become diagnostics rather
// than printed usage.
func newFlags(command string) *flag.FlagSet {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	return flags
}

// parseFlags rejects --help mixed with other arguments. helpRequested handles
// help on its own before parsing.
func parseFlags(flags *flag.FlagSet, args []string) error {
	err := flags.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		return fmt.Errorf("%s --help must be used alone", flags.Name())
	}

	return err
}

// once wraps a flag setter so that repeating the flag is a usage error.
func once(name string, set func(string) error) func(string) error {
	seen := false
	return func(value string) error {
		if seen {
			return fmt.Errorf("--%s must be supplied only once", name)
		}
		seen = true
		return set(value)
	}
}
