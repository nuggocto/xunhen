package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nuggocto/xunhen/internal/pty"
	"github.com/nuggocto/xunhen/internal/synth"
)

// hostileLines are recovered lines that would control a terminal if drawn
// raw: a clipboard write, a hyperlink, a window title, a screen clear, a
// carriage return and a backspace that overwrite text, invalid UTF-8, a
// bidirectional override, and a C1 control introducer. Each payload is
// tied to a word, so a raw match in the transcript cannot be the browser's
// own drawing.
var hostileLines = []string{
	"HOSTILE clip\x1b]52;c;UFdORUQ=\a",
	"link \x1b]8;;https://example.invalid\x1b\\text\x1b]8;;\x1b\\",
	"title \x1b]0;PWNED-TITLE\a",
	"clear \x1b[2JCLEARED",
	"before\rOVERWRITTEN",
	"keep\bERASED",
	"bytes \xff\xfe INVALID",
	"reversed \u202eDESREVER",
	"c1 \u009b31mRED",
}

// rawPayloads must never reach the terminal as bytes. None is anything the
// browser draws for itself.
var rawPayloads = []string{
	"\x1b]52", "\x1b]8;", "\x1b]0;", "\x1b]2;", "\x1b[2JCLEARED",
	"before\rOVERWRITTEN", "keep\bERASED", "\xff", "\u202e", "\u009b",
}

// TestHostileTextReachesTheTerminalEscaped drives the built browser through
// every view that draws untrusted text: the title with the file names, the
// preview, the comparison, the export instructions with the quoted paths,
// and the help. The raw terminal stream, read before anything is stripped,
// must hold none of the payloads.
func TestHostileTextReachesTheTerminalEscaped(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	binary := buildExecutable(t, t.TempDir())

	f := synth.Chain([][]string{hostileLines, {"package calm"}})
	undo, err := f.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	undoName := "evil\x1b]0;PWNED-TITLE\a.undo"
	baseName := "base\x1b[2JCLEARED\xff.go"
	if err := os.WriteFile(filepath.Join(dir, undoName), undo, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, baseName), []byte("package calm\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	term := openTerminal(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "browse", "--undo", undoName, "--base", baseName)
	command.Dir = dir
	command.Env = []string{"PATH=", "HOME=" + dir, "TERM=xterm-256color", "LC_ALL=C"}
	command.Stdin, command.Stdout, command.Stderr = term.slave, term.slave, term.slave
	command.SysProcAttr = pty.Attach()
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}

	term.waitText(t, 0, "calm")
	steps := []struct{ keys, shows string }{
		{keys: "k", shows: "HOSTILE"}, // preview of the hostile state
		{keys: "d", shows: "hunk"},    // comparison with the reference
		{keys: "e", shows: "recovered.go"},
		{keys: "?", shows: "Keys"},
	}
	for _, step := range steps {
		mark := term.mark()
		term.write(t, step.keys)
		term.waitText(t, mark, step.shows)
	}
	term.write(t, "q")

	err = command.Wait()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		t.Fatal(err)
	}
	if status := command.ProcessState.ExitCode(); status != 0 {
		t.Fatalf("exit status %d", status)
	}

	output := term.drain()
	for _, payload := range rawPayloads {
		if strings.Contains(output, payload) {
			t.Errorf("the terminal received %q raw", payload)
		}
	}
}
