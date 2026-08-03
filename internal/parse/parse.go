// Package parse is the port's glob scanner: pattern in, token stream out.
//
// # Status
//
// Complete for the scanner's default-options path. [Parse] handles literal
// text, slashes, dots, escapes, quotes, the leading-negation prologue, the ./
// prefix rules, the star, the globstar, parentheses, the five extglobs,
// character classes including the POSIX [[:name:]] forms, "?", and braces —
// both the "{a,b}" list and the "{a..b}" range. `make tokens` reports 1,491 of
// 1,491 recorded patterns matching (100.00%), with 0 wrong.
//
// No construct is declined any more, so [UnsupportedError] no longer names one.
// What remains unbuilt is the option surface: every opts.X branch in
// tests/original/lib/parse.js that the defaults do not take is marked at its
// site rather than written. Those are the emitter's and the matcher's problem,
// not the scanner's.
//
// The type shapes below are measured rather than designed — every field appears
// in testdata/tokens/summary.json under "tokenFields", recorded from upstream's
// own parser over the 1,491 patterns its test suite uses.
//
// # It refuses rather than guesses
//
// A construct that has not been built returns an error instead of falling back
// to treating the input as literal text. The fallback would produce a token
// stream that is wrong but plausible, and on any pattern where the guess
// happened to coincide it would score as a pass — indistinguishable in the
// gate's percentage from a branch that was actually written. The token gate
// counts the two separately for the same reason: unbuilt patterns are expected
// and shrink as branches land, while a wrong one fails the run outright.
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

import (
	"errors"
	"fmt"
)

// ErrUnsupported reports a construct the scanner has not been built for yet.
//
// It exists so that an unfinished scanner fails loudly instead of guessing. The
// alternative — treating an unhandled character as literal text — produces a
// token stream that is wrong but plausible, and would score as a pass on any
// pattern where the guess happened to coincide.
var ErrUnsupported = errors.New("picomatch/parse: construct not implemented")

// UnsupportedError names the construct and the upstream site that handles it, so
// the token gate can report what the scanner is still missing rather than only
// how many patterns fail.
type UnsupportedError struct {
	Construct string // the syntax encountered, e.g. "*" or "+( extglob"
	Site      string // the upstream branch that implements it, e.g. "parse.js:1128"
	Index     int    // code-unit offset into the post-removePrefix input
}

func (e *UnsupportedError) Error() string {
	return fmt.Sprintf("picomatch/parse: %s not implemented (%s), at index %d", e.Construct, e.Site, e.Index)
}

func (e *UnsupportedError) Unwrap() error { return ErrUnsupported }

// LengthError is upstream's maxLength guard (parse.js:367). Upstream throws a
// SyntaxError with this message; the root package maps it when it needs to
// reproduce the recorded throw.
//
// Length is counted in UTF-16 code units, not bytes or runes. See units.go.
type LengthError struct {
	Length int
	Max    int
}

func (e *LengthError) Error() string {
	return fmt.Sprintf("Input length: %d, exceeds maximum allowed length: %d", e.Length, e.Max)
}

// NonTerminatingError reports input on which upstream's parse() does not return.
//
// It is not a transcription of an upstream throw — there is no throw. Upstream's
// eos() test is `state.index === input.length - 1`, and the backslash-run
// collapse at parse.js:689-699 can step the index past that value, after which
// the loop runs forever. Verified against node: "a" followed by four or more
// backslashes never returns, three or fewer do.
//
// The port detects the overshoot and reports it rather than reproducing the
// hang or inventing a state. Both alternatives are worse in the same way an
// unbuilt construct falling back to literal text would be: there is no recorded
// answer here, no fixture can ever hold one (the extractor would hang on the
// same input), and a plausible state would be indistinguishable from a real one.
// DECISIONS.md §11.
type NonTerminatingError struct {
	Site  string // the upstream branch that steps over the end, "parse.js:689"
	Index int    // code-unit offset the collapse left the index at, one before the step that passes the end
}

func (e *NonTerminatingError) Error() string {
	return fmt.Sprintf("picomatch/parse: upstream parse() does not terminate on this input (%s), at index %d", e.Site, e.Index)
}

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
	//
	// It is set at two sites in upstream — parse.js:1133, a star folded into a
	// token an earlier star already collapsed, and :561, extglobClose's risky
	// rewrite — not at every retroactive rewrite. The globstar arms at :1188-1243
	// rewrite two tokens back and deliberately leave it false; see
	// docs/transcription-traps.md #19.
	Backtrack bool
	// Globstar reports that some "**" reached one of the globstar arms.
	//
	// Upstream sets state.globstar at six sites (parse.js:1134, :1183, :1195,
	// :1212, :1225, :1241) and reads it nowhere — not in parse.js, not in
	// picomatch.js. It is carried here because it is state upstream maintains
	// and the port's scanner has to maintain it anyway to stay a transcription;
	// no caller should read meaning into it that upstream does not.
	Globstar bool
	// NegatedExtglob reports a "!(…)" whose "!" was the first thing in the
	// pattern proper.
	//
	// Upstream sets state.negatedExtglob at one site (parse.js:594, guarded by
	// token.prev.type === "bos") and, unlike Globstar, it is read: lib/scan.js
	// reports the same flag and test/api.picomatch.js:368-378 asserts it on the
	// parse state. It is carried for that reason, not as a promise to callers —
	// DECISIONS.md §6 still applies to this whole type.
	NegatedExtglob bool
	// Tokens is the authoritative representation. See the package doc.
	Tokens []Token
}

// Parse scans pattern into a token stream, running the full scanner.
//
// opts carries only the keys this package answers — see [Options] for why that
// is narrower than the keys upstream reads. The zero value is upstream's
// defaults.
//
// It corresponds to upstream's parse(pattern, {fastpaths: false}) — the form
// picomatch.parse itself always calls (lib/picomatch.js:212). The fast paths are
// a separate normalisation pass and do not belong here; see
// tools/probes/fastpath-diff.js for why they are separate rather than an
// optimisation of this one.
//
// # The state is returned alongside an error
//
// On an [UnsupportedError] the returned state is not nil: it is everything the
// scanner produced before it reached the construct it could not handle. Callers
// must not read it as an answer — it is a prefix, and Consumed and Output stop
// where the scanner did.
//
// It is returned because discarding it hides a whole class of bug. The token
// gate classifies a failure as *unbuilt* or *wrong*, and with no partial state
// it can only ever say "unbuilt" for a pattern that trips on an unbuilt
// construct — including when a branch that does exist got the tokens before it
// wrong. DECISIONS.md §9.
//
// The prefix is now everything the scanner produced. It used to stop at the last
// open "+(" or "*(", because extglobClose may rewrite every token from there
// onwards and what decides that is input the scanner had not read; with braces
// built there is no construct left to decline inside an extglob body, so the
// truncation is unreachable and is gone. DECISIONS.md §14.
//
// [LengthError] and [NonTerminatingError] return a nil state. Neither has a
// meaningful prefix: the first is refused before scanning starts, and the second
// is a point at which upstream stops producing anything.
func Parse(pattern string, opts Options) (*State, error) {
	s, err := newScanner(pattern, opts)
	if err != nil {
		return nil, err
	}
	if err := s.run(); err != nil {
		if _, ok := errors.AsType[*UnsupportedError](err); ok {
			return s.export(), err
		}
		return nil, err
	}
	return s.export(), nil
}
