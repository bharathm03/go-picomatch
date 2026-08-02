// Package picomatch is a Go port of the JavaScript glob matcher picomatch
// (https://github.com/micromatch/picomatch), v4.0.5, MIT licensed.
//
// It supports the standard and extended Bash glob features picomatch does:
// braces, extglobs, POSIX bracket expressions, globstars and regex passthrough.
//
// # Status
//
// The matching API below is declared but not yet implemented; [New], and so
// [IsMatch] and [Pattern.Match] with it, returns [ErrNotImplemented]. [Scan] is
// the exception: lib/scan.js is ported in full, so it returns a real answer and
// a nil error for every input. The behavioural fixtures the rest is being built
// against are already in place — see testdata/original and the conformance
// harness in conformance_test.go.
//
// # No compiled regexp is exposed
//
// Upstream compiles each pattern to a JavaScript RegExp and exposes it through
// makeRe. This port has no equivalent, by necessity rather than preference: Go's
// regexp is RE2, which has no lookaround, and picomatch's output relies on it in
// every non-trivial pattern — the dot guard `(?!\.)(?=.)` on a leading star and
// `(?!(?:^|\/)\.)` inside a globstar body. Six of seven representative patterns
// fail regexp.Compile outright. Returning a *regexp.Regexp would be a promise
// the matcher can never keep, so matching goes through [Pattern] and nothing
// else. See DECISIONS.md.
//
// # Relationship to upstream
//
// This package contains no JavaScript and shells out to nothing. The upstream
// implementation is vendored under tests/original solely so its own unmodified
// test suite can be recorded offline; it is never consulted at runtime.
package picomatch

import "github.com/bharathm03/go-picomatch/internal/scan"

// Pattern is a compiled glob, safe for concurrent use once built.
type Pattern struct {
	glob string
	opts Options
}

// Result describes a single match attempt in full.
//
// It mirrors the object upstream returns from `matcher(input, true)`, minus the
// internal parser state and the compiled expression, which are JavaScript
// implementation details this port does not reproduce.
type Result struct {
	// Glob is the pattern the matcher was compiled from.
	Glob string
	// Input is the string that was tested.
	Input string
	// Output is Input after any normalisation the options implied.
	Output string
	// IsMatch reports the outcome.
	IsMatch bool
	// Windows reports which path semantics were applied.
	Windows bool
}

// ScanResult is the structural breakdown of a pattern: which leading portion is
// a literal path and which trailing portion is glob syntax.
//
// The field set mirrors what upstream's scan returns; see
// testdata/original/summary.json under "resultShapes".
type ScanResult struct {
	Input  string
	Prefix string
	Start  int
	Base   string
	Glob   string

	IsBrace    bool
	IsBracket  bool
	IsGlob     bool
	IsGlobstar bool
	IsExtglob  bool

	Negated        bool
	NegatedExtglob bool

	// Parts and Slashes are both populated only when Options.Parts is set.
	// Upstream attaches them under `opts.parts` or `opts.tokens` (scan.js:350),
	// and this port does not carry `tokens` — DECISIONS.md §13. Options.ScanToEnd
	// alone does not produce either: it changes how far the scan runs, not what
	// is reported. When the option is set and the pattern holds no separator,
	// both are empty rather than nil, which is upstream's `[]`.
	Parts   []string
	Slashes []int
}

// New compiles pattern into a reusable [Pattern]. A nil opts means defaults.
//
// An empty pattern returns an [*Error] carrying upstream's own message.
func New(pattern string, opts *Options) (*Pattern, error) {
	o := opts.options()
	if pattern == "" {
		return nil, errEmptyPattern()
	}
	// Deliberately unimplemented; see package docs.
	_ = o.extglobDisabled()
	_ = o.basenameOnly()
	return nil, ErrNotImplemented
}

// Match reports whether input matches the compiled pattern.
func (p *Pattern) Match(input string) bool {
	return p.MatchDetail(input).IsMatch
}

// MatchDetail reports the full outcome of matching input.
func (p *Pattern) MatchDetail(input string) Result {
	if p == nil {
		return Result{Input: input}
	}
	return Result{Glob: p.glob, Input: input, Output: input, Windows: p.opts.Windows}
}

// IsMatch reports whether input matches any of patterns.
func IsMatch(input string, patterns []string, opts *Options) (bool, error) {
	for _, pattern := range patterns {
		p, err := New(pattern, opts)
		if err != nil {
			return false, err
		}
		if p.Match(input) {
			return true, nil
		}
	}
	// Matching nothing is an answer, not a failure: returning an error here would
	// make every negative result look like a broken call once New is implemented,
	// and would make an empty pattern list an error today.
	return false, nil
}

// Scan splits pattern into its literal prefix and its glob remainder without
// compiling it.
//
// The error is always nil. Upstream's scan() has no throw path — no length
// guard, no rejected syntax — so every input has an answer; the return signature
// keeps the shape the rest of the API uses rather than promising a failure mode
// that does not exist.
func Scan(pattern string, opts *Options) (ScanResult, error) {
	o := opts.options()
	// NoExt alone, not extglobDisabled(): scan.js reads `opts.noext` at :171 and
	// :288 and never reads `opts.noextglob`, and lib/picomatch.js:242 passes the
	// options object through without normalising the alias.
	r := scan.Scan(pattern, scan.Options{
		NoExt:     o.NoExt,
		NoNegate:  o.NoNegate,
		NoParen:   o.NoParen,
		Unescape:  o.Unescape,
		ScanToEnd: o.ScanToEnd,
		Parts:     o.Parts,
	})
	return ScanResult{
		Input:          r.Input,
		Prefix:         r.Prefix,
		Start:          r.Start,
		Base:           r.Base,
		Glob:           r.Glob,
		IsBrace:        r.IsBrace,
		IsBracket:      r.IsBracket,
		IsGlob:         r.IsGlob,
		IsGlobstar:     r.IsGlobstar,
		IsExtglob:      r.IsExtglob,
		Negated:        r.Negated,
		NegatedExtglob: r.NegatedExtglob,
		Parts:          r.Parts,
		Slashes:        r.Slashes,
	}, nil
}
