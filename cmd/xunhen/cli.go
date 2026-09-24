package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/nuggocto/xunhen/internal/history"
	"github.com/nuggocto/xunhen/internal/limits"
	"github.com/nuggocto/xunhen/internal/termtext"
)

// An interrupt keeps its default disposition, so the kernel ends the process
// and the shell reports status 130 without a code path here.
const (
	exitSuccess = 0
	exitFailure = 1
	exitUsage   = 2
)

const helpText = `xunhen - seek traces in Neovim's saved undo history

Usage:
  xunhen [--help | --version]
  xunhen help [command]
  xunhen version
  xunhen inspect --undo PATH
  xunhen show --undo PATH --base PATH --node ID [--raw --final-newline=include|omit]
  xunhen diff --undo PATH --base PATH --from ID --to ID

Available commands:
  help       Show help
  version    Show version and build information
  inspect    Describe an undo history
  show       Reconstruct a retained state
  diff       Compare two retained states

Planned command (not available in this build):
  browse     Explore a history in the terminal

This development build inspects histories, recovers states, and compares them.
`

const versionHelp = "Usage: xunhen version\nShow version and build information.\n"

// commandHelp answers "xunhen help COMMAND". Each command also accepts --help
// on its own.
var commandHelp = map[string]string{
	"help":    helpText,
	"version": versionHelp,
	"inspect": inspectHelp,
	"show":    showHelp,
	"diff":    diffHelp,
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
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
		if args[1] == "browse" {
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
	case "diff":
		return compareStates(ctx, args[1:], stdout, stderr)

	case "browse":
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

// operationError reports a failed input, budget, or output operation.
func operationError(stderr io.Writer, err error) int {
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

// boundedOutput collects a command's whole result before anything is written,
// so a budget or cancellation failure never leaves output that looks complete.
type boundedOutput struct {
	ctx      context.Context
	text     strings.Builder
	maxBytes int
	err      error
}

// line appends formatted text. Callers pass bounded scalars or text that is
// already escaped and bounded.
func (o *boundedOutput) line(format string, args ...any) {
	if o.err != nil {
		return
	}
	if o.err = o.ctx.Err(); o.err != nil {
		return
	}

	line := fmt.Sprintf(format, args...)
	if len(line) > o.maxBytes-o.text.Len() {
		o.err = errors.New("output exceeds its byte budget")
		return
	}

	o.text.WriteString(line)
}

// result returns the collected text, or the first failure.
func (o *boundedOutput) result() (string, error) {
	if o.err != nil {
		return "", o.err
	}
	if err := o.ctx.Err(); err != nil {
		return "", err
	}

	return o.text.String(), nil
}

// pathFlag registers a path flag that may be given once.
func pathFlag(flags *flag.FlagSet, name string, path *string) {
	flags.Func(name, name+" path", once(name, func(value string) error {
		*path = value
		return nil
	}))
}

// nodeFlag registers a node selector that may be given once. It accepts plain
// decimal digits only: no sign, prefix, or separator.
func nodeFlag(flags *flag.FlagSet, name string, id *history.NodeID, set *bool) {
	flags.Func(name, "node ID", once(name, func(value string) error {
		n, err := strconv.ParseUint(value, 10, 31)
		if errors.Is(err, strconv.ErrRange) {
			return fmt.Errorf("--%s exceeds the supported ID range", name)
		}
		if err != nil {
			return fmt.Errorf("--%s requires a non-negative decimal ID", name)
		}

		*id, *set = history.NodeID(n), true
		return nil
	}))
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
