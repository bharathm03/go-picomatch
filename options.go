package picomatch

// Options controls pattern compilation and matching.
//
// The field set is reconciled between two independent sources of evidence, not
// transcribed from upstream documentation:
//
//   - what the upstream Mocha suite passes in — `tools/extract` reports the 30
//     keys it observed in testdata/original/summary.json under "optionSurface";
//   - what upstream actually reads — every `opts.X` / `options.X` in
//     tests/original/lib and the two entry points.
//
// The two sets differ, and the difference is the point. A key the suite passes
// but upstream never reads is inert and gets no field here, however often it
// appears: `relaxSlashes` is passed once, in test/slashes-posix.js, and read
// nowhere — `makeRe("*")` and `makeRe("*", {relaxSlashes: true})` compile to
// identical sources. A key upstream reads but the suite never passes is real and
// does get a field, because the fixtures cannot vouch for behaviour they never
// trigger: Contains, NoFastpaths, LiteralBrackets and Prepend are all in that
// position, and are marked below.
//
// The zero value is the default configuration, matching picomatch's own
// `options || {}`. Options is passed by pointer throughout; a nil *Options means
// defaults.
type Options struct {
	// Windows selects Windows path semantics, where both / and \ separate
	// segments. Upstream defaults this from the host OS at the package entry
	// point; the Go port makes it explicit. See DECISIONS.md.
	Windows bool

	// Bash makes globstar and star behaviour follow Bash rather than
	// picomatch's default.
	Bash bool

	// Dot allows patterns to match paths whose segments begin with a dot.
	Dot bool

	// StrictSlashes disables the default leniency about trailing slashes.
	StrictSlashes bool

	// Posix enables POSIX character classes such as [:alpha:].
	Posix bool

	// Regex treats the pattern as containing regular-expression syntax that
	// should be preserved rather than escaped.
	Regex bool

	// Ignore holds patterns that veto a match even when the main pattern hits.
	Ignore []string

	// Basename matches the pattern against the final path segment only.
	Basename bool

	// MatchBase is an alias for Basename, accepted for API compatibility.
	MatchBase bool

	// NoBrace disables brace expansion, {a,b}.
	NoBrace bool

	// NoBracket disables POSIX bracket expressions.
	NoBracket bool

	// StrictBrackets makes an unclosed bracket an error rather than a literal.
	StrictBrackets bool

	// NoExtglob disables extended globs, !(a|b) and friends. NoExt is the
	// alias upstream also accepts.
	NoExtglob bool
	NoExt     bool

	// NoGlobstar disables ** crossing path separators.
	NoGlobstar bool

	// NoNegate disables leading-! negation.
	NoNegate bool

	// NoParen disables parenthesised groups.
	NoParen bool

	// NoCase makes matching case-insensitive.
	NoCase bool

	// Capture keeps capture groups in the compiled expression.
	Capture bool

	// Contains matches anywhere in the input instead of anchoring.
	//
	// Read by upstream (lib/picomatch.js, lib/parse.js) but never exercised by
	// the suite: no fixture constrains it.
	Contains bool

	// Unescape strips backslash escapes from the pattern before compiling.
	Unescape bool

	// KeepQuotes preserves quote characters instead of treating them as
	// grouping.
	KeepQuotes bool

	// NoFastpaths disables the inline fast paths upstream uses for patterns that
	// begin with `.` or `*` and contain no other glob syntax.
	//
	// Upstream spells this `fastpaths`, defaulting to on (`opts.fastpaths !==
	// false`). Inverting it here keeps the Go zero value equal to upstream's
	// default; a `Fastpaths bool` would silently disable them instead.
	//
	// It is not cosmetic. The fast paths change the compiled output, not just the
	// route to it — upstream's own parser disables them mid-parse at
	// lib/parse.js:588 to work around a `**/!(*.d).ts` misparse. Read by upstream
	// but never exercised by the suite.
	NoFastpaths bool

	// LiteralBrackets forces `[` and `]` to be treated as literals (true) or as a
	// bracket expression (false).
	//
	// Upstream tests this three ways — `=== false` at lib/parse.js:856 and
	// `=== true` at :865 — so unset is a third state distinct from either, and a
	// plain bool could not express it. Read by upstream but never exercised by
	// the suite.
	LiteralBrackets *bool

	// Prepend is emitted ahead of the compiled output (lib/parse.js:371).
	//
	// Read by upstream but never exercised by the suite.
	Prepend string

	// Flags are extra regular-expression flags applied to the compiled pattern.
	Flags string

	// MaxLength caps the accepted pattern length; zero means the default cap.
	MaxLength int

	// MaxExtglobRecursion caps nested extglob expansion depth. Nil selects
	// picomatch's built-in cap; a non-nil pointer sets an explicit one, so a cap
	// of zero — forbid nested extglobs outright — is expressible and stays
	// distinct from "no cap at all".
	//
	// Upstream additionally accepts `maxExtglobRecursion: false`, meaning
	// unlimited. That is UnlimitedExtglobRecursion here rather than a sentinel
	// integer, because the depth cap is a denial-of-service guard (see
	// tests/original/test/malicious.js) and silently turning a requested cap of
	// zero into "unlimited" is the one mistake this field must not permit.
	MaxExtglobRecursion       *int
	UnlimitedExtglobRecursion bool

	// ScanToEnd makes Scan consume the whole input rather than stopping at the
	// first glob character.
	ScanToEnd bool

	// Parts makes Scan return the individual path segments.
	Parts bool
}

// options returns o, or a zero-value Options when o is nil, so callers can treat
// a nil *Options as defaults without a nil check at every use.
func (o *Options) options() Options {
	if o == nil {
		return Options{}
	}
	return *o
}

// extglobDisabled reports whether extended globs are off under either spelling.
func (o Options) extglobDisabled() bool { return o.NoExtglob || o.NoExt }

// basenameOnly reports whether matching is restricted to the final path segment.
func (o Options) basenameOnly() bool { return o.Basename || o.MatchBase }
