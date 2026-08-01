package picomatch

import "errors"

// ErrNotImplemented is returned by every entry point until the matcher lands.
//
// It is deliberately not an [*Error]: it carries no upstream message, because
// upstream has no such failure. The conformance harness scores it as a failure
// rather than matching it against a recorded throw.
var ErrNotImplemented = errors.New("picomatch: not implemented")

// JavaScript error constructor names, as recorded in the fixtures.
const (
	TypeError   = "TypeError"
	SyntaxError = "SyntaxError"
)

// Error is a pattern-compilation failure that upstream picomatch reports by
// throwing.
//
// Name and Message reproduce the thrown JavaScript exception exactly, so the
// conformance harness can compare them against what the upstream suite recorded.
// Without them a port could return any error at all for a recorded throw and
// score behavioural parity for it; the fixtures carry the message, so it is
// checkable, and checking it is the whole point of the exercise.
//
// Error() prefixes the package name for Go's convention that error strings are
// lower-case and self-identifying. Message stays verbatim, capital and all —
// it is evidence, not prose.
type Error struct {
	// Name is the JavaScript constructor name: [TypeError] or [SyntaxError].
	Name string
	// Message is the thrown message, byte-for-byte as upstream produces it.
	Message string
}

func (e *Error) Error() string { return "picomatch: " + e.Message }

// errEmptyPattern reproduces the guard at lib/picomatch.js, which rejects a
// pattern that is not a non-empty string.
//
// The wording differs from makeRe's ("Expected a non-empty string") and the two
// must not be merged: they are distinct observable behaviours, and the fixtures
// record which call site produced which.
func errEmptyPattern() error {
	return &Error{Name: TypeError, Message: "Expected pattern to be a non-empty string"}
}
