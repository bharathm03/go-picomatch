package picomatch

// Options controls pattern compilation and matching.
//
// The field set is derived from the extracted fixtures rather than transcribed
// from upstream documentation: every option below is one the upstream Mocha suite
// actually exercises. `tools/extract` reports the surface it observed in
// testdata/original/summary.json under "optionSurface", so this struct can be
// re-checked against the evidence rather than against memory.
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

	// RelaxSlashes relaxes slash matching at segment boundaries.
	RelaxSlashes bool

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
	Contains bool

	// Unescape strips backslash escapes from the pattern before compiling.
	Unescape bool

	// KeepQuotes preserves quote characters instead of treating them as
	// grouping.
	KeepQuotes bool

	// Literal treats the whole pattern as a literal string.
	Literal bool

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
