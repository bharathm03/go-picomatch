package compile

// Tests for the compile layer. Same footing as internal/parse's own tests:
// nothing here states an answer that was reasoned out. Every expected `source`
// below was read back from upstream —
//
//	node -e "const pm=require('./tests/original/lib/picomatch.js');console.log(pm.makeRe('foo[/]bar',{}).source)"
//
// — and every input alongside it is the `scannerOutput` testdata/emit records for
// the same pattern. What the table asserts is the step BETWEEN those two, which
// is the only part this package owns.

import "testing"

// TestSourceIsCompileReWrap covers picomatch.js:270-276.
//
// The negated rows are the ones worth having. `^(?!^(?:X)$).*$` keeps the inner
// anchors, which is what distinguishes it from utils.wrapOutput's
// `(?:^(?!X).*$)` — a different wrap, applied at a different site, reachable on
// a different path. Writing either from memory produces the other.
func TestSourceIsCompileReWrap(t *testing.T) {
	for _, tc := range []struct {
		name    string
		output  string
		negated bool
		opts    Options
		want    string
	}{
		{
			// picomatch.makeRe('a/b', {}).source
			name:   "plain",
			output: `a\/b`,
			want:   `^(?:a\/b)$`,
		},
		{
			// picomatch.makeRe('!!!!!!!abc', {}).source, whose scannerOutput is "abc".
			name:    "negated keeps the inner anchors",
			output:  "abc",
			negated: true,
			want:    `^(?!^(?:abc)$).*$`,
		},
		{
			// The same, on an output that is itself full of lookarounds — from
			// "!!!!!!(abc)". Nesting is the whole risk here.
			name:    "negated over an extglob body",
			output:  `(?=.)(?:(?!(?:abc)$))[^/]*?`,
			negated: true,
			want:    `^(?!^(?:(?=.)(?:(?!(?:abc)$))[^/]*?)$).*$`,
		},
		{
			// opts.contains drops both anchors at :270-271. No corpus record sets
			// it, so this row states the transcription, not a recording — it is
			// here to pin which of the two wraps the flag reaches.
			name:   "contains drops the anchors",
			output: "abc",
			opts:   Options{Contains: true},
			want:   "(?:abc)",
		},
		{
			// ...and NOT the negation wrap, which :274 applies unconditionally.
			name:    "contains does not drop the negation wrap",
			output:  "abc",
			negated: true,
			opts:    Options{Contains: true},
			want:    `^(?!(?:abc)).*$`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Source(tc.output, tc.negated, tc.opts); got != tc.want {
				t.Errorf("Source(%q, %v) = %q, want %q", tc.output, tc.negated, got, tc.want)
			}
		})
	}
}

// TestToRegexSerialisesAndFallsBack covers the two things that make a recorded
// `source` differ from the string compileRe built — docs/transcription-traps.md
// #52 and #53. Both are recorded facts; between them they account for 8 of the
// 2,028 compiled records, and an emitter written from picomatch.js alone
// reproduces the other 2,020 and then disagrees on these.
func TestToRegexSerialisesAndFallsBack(t *testing.T) {
	for _, tc := range []struct {
		name      string
		source    string
		opts      Options
		wantSrc   string
		wantFlags string
	}{
		{
			// The wrapped output for "foo[/]bar", whose scannerOutput is
			// `foo(?:\[/\]|[/])bar`. Two slashes, one escaped and one not, and the
			// difference is whether a character class is open. The "\[" before the
			// first is an ESCAPED bracket, so it opens nothing.
			name:    "a slash outside a class is escaped, one inside is not",
			source:  `^(?:foo(?:\[/\]|[/])bar)$`,
			wantSrc: `^(?:foo(?:\[\/\]|[/])bar)$`,
		},
		{
			// Already-escaped slashes survive unchanged, which is what makes the
			// serialisation safe to apply unconditionally rather than only to the
			// 5 records that need it.
			name:    "an escaped slash is not escaped twice",
			source:  `^(?:a\/b)$`,
			wantSrc: `^(?:a\/b)$`,
		},
		{
			// picomatch.js:344-347 swallows the SyntaxError and returns /$^/. The
			// source is the literal "$^" and the flags are EMPTY — the requested
			// flags go with the regex that was never built.
			name:      "an uncompilable source becomes the $^ sentinel",
			source:    `^(?:a\\(b)$`,
			opts:      Options{NoCase: true},
			wantSrc:   "$^",
			wantFlags: "",
		},
		{
			name:      "nocase supplies the flag",
			source:    "^(?:abc)$",
			opts:      Options{NoCase: true},
			wantSrc:   "^(?:abc)$",
			wantFlags: "i",
		},
		{
			// `opts.flags || (opts.nocase ? 'i' : '')` — flags wins when truthy,
			// and an EMPTY flags string is not truthy, so it falls through to
			// nocase exactly as an absent one does.
			name:      "an empty flags string falls through to nocase",
			source:    "^(?:abc)$",
			opts:      Options{Flags: "", NoCase: true},
			wantSrc:   "^(?:abc)$",
			wantFlags: "i",
		},
		{
			name:      "a set flags string wins over nocase",
			source:    "^(?:abc)$",
			opts:      Options{Flags: "g", NoCase: true},
			wantSrc:   "^(?:abc)$",
			wantFlags: "g",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src, flags := ToRegex(tc.source, tc.opts)
			if src != tc.wantSrc {
				t.Errorf("ToRegex(%q) source = %q, want %q", tc.source, src, tc.wantSrc)
			}
			if flags != tc.wantFlags {
				t.Errorf("ToRegex(%q) flags = %q, want %q", tc.source, flags, tc.wantFlags)
			}
		})
	}
}

// TestPathFullScannerAnswersOnlyOneWay pins the predicate's asymmetry, which is
// the thing a caller can most easily get wrong: false does NOT mean "a fast path
// won", it means "a fast path was entered and only running it can say".
//
// The `true` rows are the ones the gate relies on. Every one of the 1,134
// records this returns true for records path "none"; re-derive with the command
// in docs/emit-oracle.md rather than trusting the number here.
func TestPathFullScannerAnswersOnlyOneWay(t *testing.T) {
	for _, tc := range []struct {
		input string
		opts  Options
		want  bool
	}{
		// parse.js:606's seven characters, each on its own: any one of them
		// anywhere closes the inline path.
		{input: "a/b", want: true},
		{input: "a(b", want: true},
		{input: "a)b", want: true},
		{input: "a[b", want: true},
		{input: "a]b", want: true},
		{input: "a{b", want: true},
		{input: "a}b", want: true},
		{input: `a"b`, want: true},

		// A leading "*" or "!" closes the inline path. Only "*" and "." also OPEN
		// the top path at picomatch.js:312, and that difference decides the
		// answer: "!abc" shuts the one door without opening the other, so nothing
		// ran and the path is knowable. Every negated corpus pattern records
		// "none" for exactly this reason.
		{input: "*.js", want: false},
		{input: ".dotfile", want: false},
		{input: "!abc", want: true},

		// A NON-leading "*" is the trap. Reading parse.js:606 as one character
		// set over the whole string would wrongly return true here; the "^" binds
		// to the "[*!]" alternative alone.
		{input: "a*b", want: false},
		{input: "a!b", want: false},

		// Plain text takes the inline path, so the port cannot name it yet.
		{input: "abc", want: false},

		// opts.fastpaths === false turns both guards off, so even an input that
		// would have taken a fast path reaches the full scanner.
		{input: "*.js", opts: Options{NoFastpaths: true}, want: true},
		{input: "abc", opts: Options{NoFastpaths: true}, want: true},

		// Non-ASCII: the byte-level tests must not fire on a continuation byte.
		// "★" is E2 98 85 and none of those bytes is one of the seven.
		{input: "★abc", want: false},
		{input: "★/abc", want: true},
	} {
		t.Run(tc.input, func(t *testing.T) {
			if got := PathFullScanner(tc.input, tc.opts); got != tc.want {
				t.Errorf("PathFullScanner(%q, %+v) = %v, want %v", tc.input, tc.opts, got, tc.want)
			}
		})
	}
}
