// Package parse is the port's glob scanner: pattern in, token stream out.
//
// # Status
//
// [Parse] is declared but not implemented. What exists is the shape, and the
// shape is measured rather than designed — every field below appears in
// testdata/tokens/summary.json under "tokenFields", recorded from upstream's own
// parser over the 1,491 patterns its test suite uses.
//
// # Why the port has a token stream at all
//
// Upstream's parse() maintains two representations of the same pattern at once:
// an incrementally appended regex string and a token array. When a decision
// invalidates output it has already appended, it sets state.backtrack, discards
// the string, and rebuilds it from the tokens (lib/parse.js:1309, set at :561,
// :731, :922, :1133 — 77 of the corpus patterns reach it). That is the tell for
// which representation is authoritative: when the two disagree, picomatch trusts
// the tokens. The string is a cache.
//
// So this package builds tokens and never maintains a parallel serialised form.
// It is not a stylistic preference, and it is not only forced by RE2 — it is
// what the reference implementation itself falls back to under pressure.
//
// # This is internal on purpose
//
// DECISIONS.md §6 excludes upstream's parser state from the public API and from
// the parity number: it is an implementation detail of a regex-backed JavaScript
// parser, and reproducing it as a promise would be a promise about internals.
// Using it as an internal oracle is a different thing from exposing it. Nothing
// here is reachable by an importer of the root package.
package parse

import "errors"

// ErrNotImplemented is returned by [Parse] until the scanner lands.
//
// Declared here rather than reused from the root package because the dependency
// runs the other way: the root package will import this one.
var ErrNotImplemented = errors.New("picomatch/parse: not implemented")

// Token is one unit of a parsed pattern.
//
// # Optional fields
//
// Output, OutputIndex and TokensIndex are pointers because upstream distinguishes
// absent from zero and the recording proves it does, not because pointers are
// tidy: across 10,558 recorded tokens, `output` is absent 2,366 times and present
// as the empty string 1,883 times, and `outputIndex` is absent 10,443 times and
// present as 0 on 18 tokens. A plain string and int would collapse both
// distinctions, and the token gate would then pass a parser that never sets them.
//
// The bool fields are plain bools for the same evidentiary reason inverted:
// Extglob, Posix, Comma and Star are never recorded as false. Absent and false
// are the same state, so there is nothing for a pointer to carry.
type Token struct {
	// Type is the token kind: one of the 15 in testdata/tokens/summary.json —
	// text, slash, bos, star, paren, maybe_slash, globstar, negate, qmark,
	// bracket, brace, dot, comma, plus, at.
	Type string
	// Value is the source text this token consumed.
	Value string
	// Output is the regex fragment it emits, when that differs from Value.
	Output *string

	// Extglob marks a token belonging to an extglob construct: !(…) *(…) +(…)
	// ?(…) @(…). Posix marks a bracket token that came from a POSIX class.
	// Comma marks a brace token containing a comma, which is what separates a
	// brace list from a brace range. Star marks a token a star was folded into.
	Extglob bool
	Posix   bool
	Comma   bool
	Star    bool

	// OutputIndex and TokensIndex record where this token sits in the output
	// string and the token array. They exist so a later decision can rewrite an
	// earlier token — see the package doc on state.backtrack.
	OutputIndex *int
	TokensIndex *int
}

// State is the result of parsing one pattern.
type State struct {
	// Consumed is the input the scanner accounted for. It is not always the
	// input it was given: parsing "a/**/*.js" consumes "a/**//*.js" — the
	// scanner invents a slash. Anything reproducing that has to do so
	// deliberately.
	Consumed string
	// Output is the assembled regex source, before compileRe anchors it.
	Output string
	// Negated reports a leading "!" that inverts the match.
	Negated bool
	// Backtrack reports that some token was rewritten after being emitted, so
	// Output was discarded and rebuilt from Tokens.
	Backtrack bool
	// Tokens is the authoritative representation. See the package doc.
	Tokens []Token
}

// Parse scans pattern into a token stream, running the full scanner.
//
// It corresponds to upstream's parse(pattern, {fastpaths: false}) — the form
// picomatch.parse itself always calls (lib/picomatch.js:212). The fast paths are
// a separate normalisation pass and do not belong here; see
// tools/probes/fastpath-diff.js for why they are separate rather than an
// optimisation of this one.
func Parse(pattern string) (*State, error) {
	return nil, ErrNotImplemented
}
