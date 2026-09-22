package undofile

import "fmt"

// ErrorKind distinguishes input failures without matching diagnostic text.
type ErrorKind string

// Input error kinds. Each value doubles as the text shown in diagnostics.
const (
	Truncated   ErrorKind = "truncated input"
	Invalid     ErrorKind = "invalid input"
	Unsupported ErrorKind = "unsupported input"
	Limit       ErrorKind = "limit exceeded"
	ReadFailure ErrorKind = "read failure"
	Mismatch    ErrorKind = "base mismatch"
)

// InputError identifies an input failure. Offset is zero-based, or -1 when
// unavailable. Source and Detail are untrusted text; escape them for a terminal.
type InputError struct {
	Kind     ErrorKind
	Source   string
	Offset   int64
	Sequence Sequence
	Field    string
	Detail   string
	Cause    error
}

func (e *InputError) Error() string {
	where := e.Source
	if e.Offset >= 0 {
		where += fmt.Sprintf(": byte %d", e.Offset)
	}
	if e.Sequence != 0 {
		where += fmt.Sprintf(": sequence %d", e.Sequence)
	}

	message := fmt.Sprintf("%s: %s: %s: %s", where, e.Kind, e.Field, e.Detail)
	if e.Cause != nil {
		message += ": " + e.Cause.Error()
	}

	return message
}

func (e *InputError) Unwrap() error { return e.Cause }
