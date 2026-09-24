package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"syscall"
	"unicode/utf8"
	"unsafe"

	"github.com/nuggocto/xunhen/internal/history"
	"github.com/nuggocto/xunhen/internal/limits"
	"github.com/nuggocto/xunhen/internal/termtext"
)

const showHelp = `Usage: xunhen show --undo PATH --base PATH --node ID [--raw --final-newline=include|omit]

Reconstruct a retained buffer state using a matching base file.
  --undo PATH             Undo-file path (required)
  --base PATH             Matching source text (required; read-only)
  --node ID               Retained sequence number; 0 is the root (required)
  --raw                   Write UTF-8/LF bytes to redirected stdout
  --final-newline POLICY  Required with --raw: include or omit
  -h, --help              Show this help

Base files must be UTF-8/LF text without a BOM, NUL, or CRLF. Raw export also
rejects a selected state with invalid UTF-8 or NUL, which retained edits can
hold. A lone CR is literal line content. Historical encoding and final-newline
settings are unknown; the chosen policy does not restore original file bytes.
An empty buffer is one empty line, so include writes a single LF and omit
writes nothing.

Default output shows tabs, quotes, backslashes, and printable Unicode as they
are, and escapes terminal controls and invalid bytes. Use --raw for exact text.
Raw output is refused when stdout is a terminal. Inputs are never modified.
Exit status: 0 success, 1 input/output failure, 2 invalid arguments, 130 interrupt.
`

type showOptions struct {
	undo, base   string
	node         history.NodeID
	hasNode      bool
	raw          bool
	finalNewline string
}

func show(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if helpRequested(args) {
		return writeOutput(stdout, stderr, showHelp)
	}

	options, err := parseShowArgs(args)
	if err != nil {
		return diagnostic(stderr, exitUsage, err.Error())
	}

	if options.raw {
		terminal, err := outputIsTerminal(stdout)
		if err != nil {
			return operationError(stderr, err)
		}
		if terminal {
			return diagnostic(stderr, exitFailure, "raw output requires redirected stdout")
		}
	}

	lim := limits.Default()
	states, err := recoverStates(ctx, options.undo, options.base, []history.NodeID{options.node}, lim)
	if err != nil {
		return operationError(stderr, err)
	}
	snapshot := states[0]

	text, err := showText(ctx, snapshot, options, lim.OutputBytes)
	if err != nil {
		return operationError(stderr, err)
	}

	return writeOutput(stdout, stderr, text)
}

func parseShowArgs(args []string) (showOptions, error) {
	var options showOptions

	flags := newFlags("show")
	flags.BoolVar(&options.raw, "raw", false, "raw export")

	pathFlag(flags, "undo", &options.undo)
	pathFlag(flags, "base", &options.base)
	nodeFlag(flags, "node", &options.node, &options.hasNode)
	flags.Func("final-newline", "include or omit", once("final-newline", func(value string) error {
		if value != "include" && value != "omit" {
			return errors.New("--final-newline must be include or omit")
		}
		options.finalNewline = value
		return nil
	}))

	if err := parseFlags(flags, args); err != nil {
		return showOptions{}, err
	}

	switch {
	case options.undo == "" || options.base == "" || !options.hasNode || flags.NArg() != 0:
		return showOptions{}, errors.New("expected show --undo PATH --base PATH --node ID; see 'xunhen show --help'")
	case options.raw && options.finalNewline == "":
		return showOptions{}, errors.New("--raw requires --final-newline=include|omit")
	case !options.raw && options.finalNewline != "":
		return showOptions{}, errors.New("--final-newline requires --raw")
	}

	return options, nil
}

// TCGETS checks the descriptor, so /dev/null is not mistaken for a terminal.
func outputIsTerminal(output io.Writer) (bool, error) {
	file, ok := output.(interface{ Fd() uintptr })
	if !ok {
		return false, nil
	}

	var attrs syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&attrs)))
	switch errno {
	case 0:
		return true, nil
	case syscall.ENOTTY:
		return false, nil
	default:
		return false, fmt.Errorf("check output terminal: %w", errno)
	}
}

// showText renders the whole state before anything is written, so a failure
// never leaves a partial result that looks complete.
func showText(ctx context.Context, snapshot *history.Snapshot, options showOptions, maxBytes int) (string, error) {
	lines := snapshot.Lines()
	var out strings.Builder

	for i, line := range lines {
		if err := ctx.Err(); err != nil {
			return "", err
		}

		if options.raw {
			if err := checkRawLine(line); err != nil {
				return "", err
			}
		} else {
			escaped, err := termtext.EscapeDisplay(ctx, line, maxBytes)
			if err != nil {
				return "", err
			}
			line = escaped
		}

		last := i == len(lines)-1
		newline := !last || !options.raw || options.finalNewline == "include"

		size := len(line)
		if newline {
			size++
		}
		if size > maxBytes-out.Len() {
			return "", errors.New("show output exceeds output byte budget")
		}

		out.WriteString(line)
		if newline {
			out.WriteByte('\n')
		}
	}

	return out.String(), nil
}

// checkRawLine rejects text that UTF-8/LF export cannot represent faithfully.
// Lines never hold LF: the decoder stores a serialized LF as NUL, and base
// verification rejects LF.
func checkRawLine(line string) error {
	switch {
	case !utf8.ValidString(line):
		return errors.New("selected state contains invalid UTF-8; raw export unsupported")
	case strings.Contains(line, "\x00"):
		return errors.New("selected state contains NUL; raw export unsupported")
	}

	return nil
}
