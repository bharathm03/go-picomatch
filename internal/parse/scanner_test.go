// These tests assert structure, not behaviour.
//
// Nothing here states what picomatch's answer is for any pattern — testdata/tokens
// holds the recorded answers and `make tokens` is what checks them. What is
// asserted is that [Parse] returns, returns the same thing twice, keeps to its
// own error contract, and does not hand out tokens that share memory. Those are
// properties of this implementation, checkable without a fixture, and false for
// bugs a percentage cannot see.
//
// It is untagged because the token gate is not. `go test ./...` is what `make
// check` and both CI Go jobs run, and until this file existed that suite touched
// no scanner code at all: Parse, run, push, emit, negate, advance, peek and
// export were all at 0% statement coverage, so a panic or a non-terminating loop
// in a built branch left every everyday signal green.
package parse

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// builtPatterns exercises one path through each branch the scanner implements.
// It is a coverage list, not an expectation list — the assertion is only that
// these parse at all.
var builtPatterns = []string{
	"",
	"a",
	"abc",
	"a/b",
	"a/b/c",
	"/",
	"./a",
	"./a/b",
	"!a",
	"!!a",
	"!!!a",
	"!./a",
	"a!b",
	".a",
	"a.b",
	"./.a",
	"a,b",
	"a}b",
	"}",
	"a]b",
	"]",
	"a|b",
	"a+",
	"+",
	"a@b",
	"@",
	"a$b",
	"a^b",
	`a\b`,
	`a\`,
	`a\\b`,
	`a\.b`,
	`a\/b`,
	`a\;b`,
	`"a"`,
	`"a.b"`,
	`a"b"c`,
	`"a$b"`,
	strings.Repeat("a", 4096),
	strings.Repeat("a.", 512),
	"フォルダ/x",

	// Stars (parse.js:1128-1283). The three prefix positions at :1263 — index
	// equal to state.start, after a slash, after a dot — plus the two ways
	// state.start moves off zero, which is what makes that test different from
	// `index == 0`.
	"*",
	"a*",
	"*a",
	"a/*",
	"*/a",
	"a/*/b",
	".*",
	"a.*",
	"*.*",
	"!*",
	"!!*",
	"!./*",
	"@*",
	"*$",
	// REPLACEMENTS rewrites "***" to "*" before the loop starts
	// (parse.js:361), so what the scanner sees here is a single star.
	"***",

	// Globstars (parse.js:1145-1244). One path through each arm: the whole
	// pattern (:1178), a trailing "/**" (:1188), a "/**/" in the middle
	// (:1201), a leading "**/" (:1220), the fallthrough (:1231), and the
	// not-a-start star that stays a plain star with an empty output (:1161).
	// Then the two mechanisms the branch switches on: push()'s rewrite back to
	// a star (:494) and the fold that sets state.backtrack (:1128).
	"**",
	"/**",
	"a/**",
	"a/b/**",
	"**/a",
	"**/",
	"a/**/b",
	"a/**/",
	"x/**/y/**/z",
	"**/*.js",
	"a**",
	"a**b",
	"**a",
	"**c",
	"!**",
	"a/**//b",
	"a/**/**/b",
	"a/***",
	// REPLACEMENTS rewrites both of these to "**" before the loop starts.
	"**/**",
	"**/**/**",

	// Parens (parse.js:788-808) and extglobs (:523-600, :1021-1103, :1140).
	// One path through each of the five openers, the two closing arms, and the
	// unclosed-paren loop at :1292.
	"(a)",
	"(a|b)",
	"a(b)c",
	")",
	"))",
	"))a.b",
	"(",
	"a(",
	"a+(",
	"a*(",
	"@(a)",
	"a@(b)c",
	"!(a)",
	"+(a)",
	"*(a)",
	"?(a)",
	"!(a|b)",
	"a/!(b)/c",
	// The negate arm's three shapes at :571-596: the globstar body when the
	// inner holds a "/", the ")$))" close at end-of-input or before a run of
	// ")", and the recursive parse of a trailing ".ts"-style suffix at :588.
	"!(a/b)",
	"!(*)",
	"!(*).ts",
	"!(*.d).ts",
	"(!(a))",
	"**/!(a).js",
	// The ReDoS analysis at :287-347 and the risky rewrite at :544. Not risky:
	// a single branch, or branches that are plain literals. Risky: a "*" or "?"
	// branch beside another, a repeated-character prefix overlap, and a nested
	// "*(" sequence — the last of which also produces a safeOutput.
	"+(*)",
	"+(a|b)",
	"+(ab|cd)",
	"+(*|a)",
	"+(a|aa)",
	"+(*(a))",
	"+(*(a)|*(b))",
	"x+(*(a)|*(b))",
	"*(*(a)|*(b))",
	"+(+(+(a)))",
	// Extglobs beside the constructs that already exist: the globstar
	// lookbehind at :496 and :1162, and the "+" arms at :1077 and :1082.
	"**!(a)",
	"!(a)**",
	"**/+(a)",
	"(+)",
	"(a)+",
	"!(+)",
	"+(+)",

	// Question marks (parse.js:1021-1047) and brackets (:707-758, :814-875),
	// including the POSIX classes and the two arms that used to decline inside
	// an open "+(".
	"?", "a?", "[a]", "+(a[b])", "+(*|?)",
	"[[:alpha:]]", "[^a]", "[]]", `[\d]`,

	// Braces (parse.js:881-940), one path through each arm. The list form and
	// the range form; a "}" with nothing open (:900); a brace with neither a
	// comma nor a range, which rewrites both delimiters and replays the output
	// from brace.outputIndex (:925); a range whose class V8 rejects, which
	// takes expandRange's catch (:33); the "stack" test that keeps a comma
	// inside a nested paren a comma (:962); the unclosed-brace loop (:1298);
	// and the four readers of state.braces that could not fire before.
	"{a,b}",
	"{a,b,c}",
	"{a..c}",
	"{1..2}",
	"a/{a..c}",
	"{a}",
	"{}",
	"{a}b",
	"{ac..b}",
	"{a(b,c)}",
	"{a[b,c]}",
	"{a",
	"{a..b",
	"a/{b",
	"{a...b}",
	"{a..b..c}",
	"{{a,b},c}",
	"{a,{b,c}}",
	"**{a,b}",
	"**{a..b}",
	"{**}",
	"{a/**/b}",
	"{.a,b}",
	"{*,a}",
	"*({a})",
	"!(*).{ts,tsx}",
	"a{b,c}/{d..f}",
	"{,}",
	"{..}",
}

// unbuiltPatterns are constructs the scanner declines, and there are none left:
// the brace branch was the last, so every branch of upstream's loop is written
// and no default-options input reaches an [UnsupportedError].
//
// The list and the test below are kept rather than deleted, because what they
// assert is the *contract* — a declined construct names itself and still hands
// back the tokens it managed — and that contract is what the token gate's
// unbuilt/wrong split rests on (DECISIONS.md §9). It becomes load-bearing again
// the moment an option path is written that this package cannot yet answer.
var unbuiltPatterns []string

func TestParseReturnsForEveryBuiltBranch(t *testing.T) {
	for _, p := range builtPatterns {
		st, err := Parse(p)
		if err != nil {
			t.Errorf("Parse(%q): unexpected error %v", p, err)
			continue
		}
		if st == nil {
			t.Errorf("Parse(%q): no state and no error", p)
			continue
		}
		if len(st.Tokens) == 0 || st.Tokens[0].Type != "bos" {
			t.Errorf("Parse(%q): first token is not bos: %+v", p, st.Tokens)
		}
	}
}

// TestParseIsDeterministic guards the in-place appends in push(). A token whose
// value shares a backing array with something the scanner still holds produces
// output that depends on what was allocated where, which shows up as the same
// pattern parsing differently in a different order.
func TestParseIsDeterministic(t *testing.T) {
	for _, p := range builtPatterns {
		first, err := Parse(p)
		if err != nil {
			t.Fatalf("Parse(%q): %v", p, err)
		}
		for i := 0; i < 3; i++ {
			again, err := Parse(p)
			if err != nil {
				t.Fatalf("Parse(%q) run %d: %v", p, i, err)
			}
			if again.Consumed != first.Consumed || again.Output != first.Output {
				t.Fatalf("Parse(%q) run %d: consumed/output differ: %q/%q vs %q/%q",
					p, i, again.Consumed, again.Output, first.Consumed, first.Output)
			}
			if len(again.Tokens) != len(first.Tokens) {
				t.Fatalf("Parse(%q) run %d: %d tokens, first run gave %d", p, i, len(again.Tokens), len(first.Tokens))
			}
			for j := range first.Tokens {
				if again.Tokens[j] != first.Tokens[j] && !tokensEqual(again.Tokens[j], first.Tokens[j]) {
					t.Fatalf("Parse(%q) run %d: token %d differs: %+v vs %+v", p, i, j, again.Tokens[j], first.Tokens[j])
				}
			}
		}
	}
}

func tokensEqual(a, b Token) bool {
	if a.Type != b.Type || a.Value != b.Value {
		return false
	}
	if (a.Output == nil) != (b.Output == nil) {
		return false
	}
	if a.Output != nil && *a.Output != *b.Output {
		return false
	}
	return a.Extglob == b.Extglob && a.Posix == b.Posix && a.Comma == b.Comma && a.Star == b.Star
}

// TestTokensDoNotShareMemory is the direct check on the invariant clone() exists
// for. push() grows token values and outputs with append, which writes into
// spare capacity, so two fields backed by one array corrupt each other the
// moment either grows — silently, and only for patterns long enough to reach the
// capacity boundary.
//
// It reaches into the scanner rather than going through Parse because the
// exported strings are copies: by the time a Token is built the aliasing has
// already been laundered away.
func TestTokensDoNotShareMemory(t *testing.T) {
	// Merge-heavy: every one of these pushes a text token onto a preceding text
	// token, which is the path that appends to both value and output. The
	// star-bearing ones cover the other in-place grower — starGuard at
	// parse.js:1263-1281 appends the same guard to state.output and to
	// prev.output, which is the port's first retroactive edit to an already
	// emitted token.
	//
	// The globstar patterns cover the deeper rewrites: :1182 and :1224 assign
	// state.output from prev.output (which must copy, or the two grow into one
	// array), :1190 and :1205 rebuild prior.output two tokens back, and :499
	// truncates state.output and replaces the token's output outright.
	for _, pattern := range []string{
		"a.b.c.d", `a\.b\.c`, "a.b}c|d,e", `"ab"cd"ef"`, strings.Repeat("a.", 64),
		"a/*/b.c", ".*.*", "*.*.*", "!./*/a.b", strings.Repeat("a/*", 64),
		"**", "**/a.b", "a/**", "a/**/b.c", "**a.b", "a/***", "a**b",
		strings.Repeat("a/**/", 32) + "z", "x/**/y/**/z",
		// The extglob rewrites: extglobClose's risky path (parse.js:544-566)
		// replaces one token's value and output outright and blanks every token
		// after it, and rebuilds state.output from a snapshot taken at open —
		// four slices that must not be sharing an array with anything.
		"+(*|a)", "+(*(a)|*(b))", "x+(a|aa).b", "!(*).ts", "!(a.b|c)d.e",
		strings.Repeat("+(a|b)", 32), "a." + strings.Repeat("!(x)", 32),
	} {
		s, err := newScanner(pattern)
		if err != nil {
			t.Fatalf("newScanner(%q): %v", pattern, err)
		}
		if err := s.run(); err != nil {
			t.Fatalf("run(%q): %v", pattern, err)
		}

		// Every unit slice the scanner is holding, named for the failure message.
		type field struct {
			name string
			get  func() units
		}
		fields := []field{{name: "state.output", get: func() units { return s.output }}}
		for i, tok := range s.tokens {
			i, tok := i, tok
			fields = append(fields, field{name: itoa(i) + ".value", get: func() units { return tok.value }})
			if tok.output != nil {
				fields = append(fields, field{name: itoa(i) + ".output", get: func() units { return *tok.output }})
			}
		}

		for i := range fields {
			before := make([]string, len(fields))
			for k, f := range fields {
				before[k] = f.get().String()
			}
			// Append past the end of one field. If any other field shares the
			// array, this lands inside it.
			_ = append(fields[i].get(), 'X', 'X', 'X', 'X') //nolint:staticcheck // the write is the test
			for k, f := range fields {
				if k == i {
					continue
				}
				if got := f.get().String(); got != before[k] {
					t.Fatalf("%q: growing token %s rewrote token %s: %q -> %q",
						pattern, fields[i].name, f.name, before[k], got)
				}
			}
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// TestParseTerminates is the regression test for an unbounded loop, which is the
// one failure mode no fixture can carry: the recorder would hang on the same
// input, so the pattern could never reach testdata/.
//
// Upstream does hang here. `a` followed by four or more backslashes drives
// parse.js's index past the end, and its eos() is an equality test, so it spins
// forever — verified against node, three backslashes return and four do not.
// DECISIONS.md §11 records why this port reports that instead of reproducing it.
func TestParseTerminates(t *testing.T) {
	bs := `\`
	var patterns []string
	for n := 1; n <= 12; n++ {
		patterns = append(patterns,
			bs+strings.Repeat(bs, n-1),
			"a"+strings.Repeat(bs, n),
			"a"+strings.Repeat(bs, n)+"b",
			"a/"+strings.Repeat(bs, n),
			`"`+strings.Repeat(bs, n),
		)
	}
	patterns = append(patterns, builtPatterns...)
	patterns = append(patterns, unbuiltPatterns...)

	for _, p := range patterns {
		p := p
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, _ = Parse(p)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("Parse(%q) did not return within 5s", p)
		}
	}
}

// TestNonTerminatingInputIsReported pins the shape of that report. The state is
// nil on purpose: there is no prefix worth handing back when upstream produces
// nothing at all for the input.
func TestNonTerminatingInputIsReported(t *testing.T) {
	// Three trailing backslashes still terminate upstream; four do not.
	if _, err := Parse(`a\\\`); err != nil {
		t.Fatalf(`Parse("a\\\"): %v — three backslashes terminate upstream`, err)
	}

	st, err := Parse(`a\\\\`)
	var nonTerm *NonTerminatingError
	if !errors.As(err, &nonTerm) {
		t.Fatalf(`Parse("a\\\\"): got %v, want a NonTerminatingError`, err)
	}
	if st != nil {
		t.Fatalf("a NonTerminatingError came with a state: %+v", st)
	}
	if nonTerm.Site == "" {
		t.Error("NonTerminatingError names no upstream site")
	}
}

// TestPosixClassAtEndOfInputIsReported covers the second site at which upstream's
// index passes the end — the bare advance() at parse.js:732, which steps over the
// "]" of ":]" without checking one is there.
//
// It needs an open character class, so the "]" that satisfied the guard at :815
// has to be one the class body already swallowed: "[][:alpha:" hangs node and
// "[[:alpha:" does not, because without the leading "[]" the class never opens.
// Both halves are asserted here, and neither can ever be a fixture — the
// extractor hangs on the same input. DECISIONS.md §11.
func TestPosixClassAtEndOfInputIsReported(t *testing.T) {
	hangs := []string{
		"[][:alpha:",
		"[]a[:digit:",
		"[^][:word:",
		"a[][:upper:",
		"[]-[:alpha:",
		"[][[:alpha:",
		"[[:alnum:][:alnum:",
	}
	for _, p := range hangs {
		st, err := Parse(p)
		var nonTerm *NonTerminatingError
		if !errors.As(err, &nonTerm) {
			t.Errorf("Parse(%q): got %v, want a NonTerminatingError", p, err)
			continue
		}
		if st != nil {
			t.Errorf("Parse(%q): a NonTerminatingError came with a state: %+v", p, st)
		}
		if nonTerm.Site != "parse.js:732" {
			t.Errorf("Parse(%q): site %q, want parse.js:732", p, nonTerm.Site)
		}
	}

	// The near misses. Each differs from a hang above by one thing: no class is
	// open, the name does not resolve, or the ":" is not the last unit.
	returns := []string{
		"[[:alpha:",    // never opened a class: no "]" ahead of the "["
		"[][:foo:",     // "foo" is not a POSIX class, so :730 never runs
		"[]][:alpha:",  // the second "]" closed the class before the "["
		"[][:alpha::",  // the ":" that resolves is not the last unit
		"[]x[:alpha:x", // ditto
		"[[:alpha:]",   // resolves, but the "]" it steps over exists
		"[[:alpha:]]",  // the whole construct
	}
	for _, p := range returns {
		if _, err := Parse(p); err != nil {
			var nonTerm *NonTerminatingError
			if errors.As(err, &nonTerm) {
				t.Errorf("Parse(%q): reported non-terminating, but upstream returns", p)
			}
		}
	}
}

// TestUnbuiltConstructsDeclineWithAPartialState covers the contract the token
// gate's unbuilt/wrong split depends on: an unbuilt construct is an error that
// names itself, and it comes with the tokens the scanner did manage.
func TestUnbuiltConstructsDeclineWithAPartialState(t *testing.T) {
	for _, p := range unbuiltPatterns {
		st, err := Parse(p)
		var unsupported *UnsupportedError
		if !errors.As(err, &unsupported) {
			// The branch landed. That is progress, not a failure.
			if err == nil {
				continue
			}
			t.Errorf("Parse(%q): got %v, want an UnsupportedError", p, err)
			continue
		}
		if !errors.Is(err, ErrUnsupported) {
			t.Errorf("Parse(%q): UnsupportedError does not unwrap to ErrUnsupported", p)
		}
		if unsupported.Construct == "" || unsupported.Site == "" {
			t.Errorf("Parse(%q): declined without naming the construct and site: %+v", p, unsupported)
		}
		if st == nil {
			t.Errorf("Parse(%q): declined with no partial state; the gate needs it to score the branches that did run", p)
			continue
		}
		if len(st.Tokens) == 0 || st.Tokens[0].Type != "bos" {
			t.Errorf("Parse(%q): partial state has no bos token: %+v", p, st.Tokens)
		}
	}
}

// TestBacktrackRebuildsOutputFromTokens covers parse.js:1309-1319, the post-loop
// rebuild, and it exists because the token gate barely reaches it: state.backtrack
// is set at one site (parse.js:1133) and only 2 of the 1,491 corpus patterns get
// there. Two inputs is not coverage of a branch that discards and reconstructs
// the entire output.
//
// What is asserted is the rebuild's own defining property, read off :1312-1317:
// once it has run, the output is exactly the concatenation of each token's output
// (or its value where there is none). That is a statement about this loop, not
// about picomatch's answer for any pattern — testdata/tokens holds the answers.
//
// The last check is what stops it being vacuous. If the rebuild never changed
// anything the property would hold everywhere for free, so at least one pattern
// must reach the end with an output the concatenation does *not* reproduce —
// which is exactly what parse.js:1182 and :1224 create by assigning state.output
// instead of appending to it.
func TestBacktrackRebuildsOutputFromTokens(t *testing.T) {
	concat := func(st *State) string {
		var b strings.Builder
		for _, tok := range st.Tokens {
			if tok.Output != nil {
				b.WriteString(*tok.Output)
			} else {
				b.WriteString(tok.Value)
			}
		}
		return b.String()
	}

	rebuilt, unrebuilt := 0, 0
	for _, p := range builtPatterns {
		st, err := Parse(p)
		if err != nil {
			t.Fatalf("Parse(%q): %v", p, err)
		}
		switch {
		case st.Backtrack:
			rebuilt++
			if got := concat(st); st.Output != got {
				t.Errorf("Parse(%q): Backtrack is set but Output was not rebuilt from the tokens:\n  output %q\n  tokens %q", p, st.Output, got)
			}
		case st.Output != concat(st):
			unrebuilt++
		}
	}

	if rebuilt == 0 {
		t.Error("no pattern set Backtrack, so the rebuild at parse.js:1309 ran for nothing here")
	}
	if unrebuilt == 0 {
		t.Error("every pattern's output already equals its token concatenation, so the rebuild assertion above holds vacuously")
	}
}

// TestGlobstarIsRewrittenBackToAStar covers push()'s lookbehind at
// parse.js:494-505 — the first retroactive rewrite in the port that *shrinks* a
// token — and the one guard that stops it, the `tok.type !== 'slash'` test at
// :498.
//
// Like the test above it asserts structure rather than an answer. The three
// properties come from reading the branch: the globstar stops being a globstar,
// its value goes back to one unit while state.consumed keeps both (trap #12),
// and state.output is truncated by the globstar body's length before the star
// output replaces it, so the body must be gone from the output entirely
// (trap #7 is the way to get that last one wrong: the truncation drops nothing
// and the body stays).
func TestGlobstarIsRewrittenBackToAStar(t *testing.T) {
	// Each of these puts a token that is not a slash, paren, brace or extglob
	// immediately after a "**": text, a dot, a "}" and a comma.
	for _, p := range []string{"**a", "**c", "**.b", "**}", "**,", "a/**b"} {
		st, err := Parse(p)
		if err != nil {
			t.Errorf("Parse(%q): %v", p, err)
			continue
		}
		for i, tok := range st.Tokens {
			if tok.Type == "globstar" {
				t.Errorf("Parse(%q): token %d is still a globstar; push() at parse.js:498 should have rewritten it", p, i)
			}
			if tok.Value == "**" {
				t.Errorf("Parse(%q): token %d kept the value %q; parse.js:501 assigns \"*\", it does not append", p, i, tok.Value)
			}
		}
		if !strings.Contains(st.Consumed, "**") {
			t.Errorf("Parse(%q): consumed %q, but both stars were read; the value shrinks and the consumed text does not", p, st.Consumed)
		}
		if strings.Contains(st.Output, globstarBody) {
			t.Errorf("Parse(%q): output %q still contains the globstar body; parse.js:499 truncates it away before appending the star", p, st.Output)
		}
	}

	// The control, and the reason :498 tests the type at all: a slash after a
	// globstar leaves it alone. Without this the test above would also pass
	// against a push() that rewrote unconditionally.
	for _, p := range []string{"**/a", "a/**/b", "**/"} {
		st, err := Parse(p)
		if err != nil {
			t.Errorf("Parse(%q): %v", p, err)
			continue
		}
		found := false
		for _, tok := range st.Tokens {
			if tok.Type == "globstar" {
				found = true
			}
		}
		if !found {
			t.Errorf("Parse(%q): no globstar survived, but the token after it is a slash — parse.js:498 exempts slashes", p)
		}
	}
}

// TestDropLastIsJavaScriptSlice pins the one helper in this package whose
// correct behaviour no pattern can demonstrate.
//
// dropLast stands for `u.slice(0, -n)` at four built sites — parse.js:499, :1189,
// :1204 and :1232 — and each of them is a truncate-then-rebuild, so getting it
// wrong retains a whole regex fragment upstream threw away. The reading a
// reviewer would ask for, `u[:len(u)-n]`, agrees everywhere except at n == 0,
// where JavaScript's -0 makes the call slice(0, 0) and discards the *entire*
// output while the Go reading leaves it untouched. docs/transcription-traps.md #7.
//
// It is asserted directly rather than through Parse because n == 0 is not
// reachable from any pattern the scanner accepts today. Two independent
// measurements say so: instrumenting a *copy* of tests/original/lib/parse.js at
// all four sites and enumerating 147,448 patterns over three alphabets
// (`*/a.!,b}` to length 5, `*/a.` to length 8, `*/.!a@+}]|$,` to length 4) hits
// the sites 6,666 / 1,098 / 1,099 / 10,451 times and records a truncation length
// of 0 on none of them; and replacing dropLast's degenerate arm with the plain Go
// reading changes nothing across the same corpus or the token gate. So this test
// is the only thing in the repo that would notice, and the assertions below are
// about JavaScript's slice, not about picomatch's answer for anything.
func TestDropLastIsJavaScriptSlice(t *testing.T) {
	for _, tc := range []struct {
		in   string
		n    int
		want string
	}{
		// The degenerate case, and the only row the plain Go reading fails.
		// JavaScript: "abc".slice(0, -0) === "" — -0 is 0, so the end index is 0.
		{"abc", 0, ""},
		{"", 0, ""},

		// The ordinary case the sites are written for.
		{"abc", 1, "ab"},
		{"abc", 2, "a"},

		// n at and past the length: JavaScript clamps the computed end index up
		// to 0 rather than wrapping, so both empty.
		{"abc", 3, ""},
		{"abc", 4, ""},
		{"", 1, ""},
	} {
		if got := dropLast(encode(tc.in), tc.n).String(); got != tc.want {
			t.Errorf("dropLast(%q, %d) = %q, want %q (JavaScript %q.slice(0, -%d))",
				tc.in, tc.n, got, tc.want, tc.in, tc.n)
		}
	}

	// The truncation is by a count of code units, not bytes or runes: every one
	// of the four sites measures it as len(token.output), and an output can carry
	// an astral character through the quoted-string path. "a\U0001F600b" is three
	// characters, four code units and six bytes.
	//
	// The n == 2 row is the discriminating one. It lands *inside* the surrogate
	// pair, which a rune-counting or byte-counting dropLast cannot do — both would
	// leave "a" — and which the boundary conversion then reports as U+FFFD,
	// exactly as DECISIONS.md §10 records.
	astral := "a\U0001F600b"
	for _, tc := range []struct {
		n    int
		want string
	}{
		{1, "a\U0001F600"},
		{2, "a�"},
		{3, "a"},
	} {
		if got := dropLast(encode(astral), tc.n).String(); got != tc.want {
			t.Errorf("dropLast(%q, %d) = %q, want %q — the count is in UTF-16 units", astral, tc.n, got, tc.want)
		}
	}
}

// TestEscapeLastIsJavaScriptEscapeLast pins the second helper in this package
// whose correctness the fixtures cannot speak for.
//
// escapeLast stands for utils.escapeLast (utils.js:36-41), called from the three
// unclosed-delimiter loops at parse.js:1286-1302; only the paren loop is built.
// Two things about it survive translation badly, and neither has an observable a
// fixture could carry, because both differ only on inputs no corpus pattern
// produces:
//
//   - it recurses past an occurrence that is *already* escaped, restarting the
//     backwards search from one before it, so a run of "\(" is skipped over
//     rather than double-escaped;
//   - the `input[idx - 1] === '\\'` test reads index -1 when the occurrence is at
//     the start of the string, and JavaScript yields undefined there rather than
//     a character, so a leading occurrence *is* escaped. A Go transcription that
//     indexes without guarding panics; one that guards by treating the missing
//     character as "escaped" silently skips it.
//
// As with dropLast, what is asserted is JavaScript's function, not picomatch's
// answer for any pattern.
func TestEscapeLastIsJavaScriptEscapeLast(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		// The ordinary case: the last unescaped occurrence gets a backslash.
		{`(`, `\(`},
		{`a(b`, `a\(b`},
		{`(a(b`, `(a\(b`},
		{`((`, `(\(`},

		// Already escaped: the search restarts to the left of it. The first two
		// have no unescaped occurrence at all and come back unchanged; the third
		// finds the earlier one.
		{`a\(b`, `a\(b`},
		{`\(`, `\(`},
		{`(a\(b`, `\(a\(b`},

		// The "already escaped" test is one character deep and does not count
		// backslash parity, so a paren behind *two* backslashes — which is an
		// escaped backslash followed by a live paren — is skipped as well.
		{`a\\(b`, `a\\(b`},

		// No occurrence at all, and the empty string.
		{`abc`, `abc`},
		{``, ``},
	} {
		if got := escapeLast(encode(tc.in), '(', len(encode(tc.in))).String(); got != tc.want {
			t.Errorf("escapeLast(%q, '(') = %q, want %q", tc.in, got, tc.want)
		}
	}

	// A run of escaped occurrences recurses once per element and must terminate
	// at the left edge rather than at index 0's phantom character.
	if got := escapeLast(encode(`\(\(\(`), '(', 6).String(); got != `\(\(\(` {
		t.Errorf("escapeLast over a run of escaped parens = %q, want it unchanged", got)
	}

	// The rows above were read off node rather than off utils.js — the recursion
	// at utils.js:39 reads more like "escape the last one that is not already
	// escaped" than it behaves, and two of them came out the other way:
	//
	//	node -e "const u=require('./tests/original/lib/utils.js');
	//	         console.log(u.escapeLast('a\\\\(b','('))"   // a\(b, unchanged
}

// TestTrimIsJavaScriptTrim pins the whitespace set, which is where Go's obvious
// helper is a different set from JavaScript's.
//
// String.prototype.trim is used at parse.js:123, :252, :275, :280 and :297 — the
// ReDoS analysis trims every extglob branch before deciding whether it is a
// single character. strings.TrimSpace would be the idiomatic translation and it
// disagrees in both directions: Go strips U+0085 (NEL), which JavaScript does
// not, and JavaScript strips U+FEFF (ZWNBSP), which Go does not.
//
// A branch that trims differently is a branch that is or is not "a single
// character", which is what decides whether an extglob body is rewritten.
func TestTrimIsJavaScriptTrim(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"  a  ", "a"},
		{"\t\n\v\f\r a \r\f\v\n\t", "a"},
		{"\u00a0a\u00a0", "a"}, // NBSP: Zs, trimmed by both
		{"\u3000a\u3000", "a"}, // ideographic space: Zs
		{"\u2028a\u2029", "a"}, // LS and PS: LineTerminator
		{"\u1680a\u205f", "a"}, // OGHAM SPACE MARK, MEDIUM MATHEMATICAL SPACE
		// U+FEFF is WhiteSpace in JavaScript and not in Go. It cannot be written
		// literally here: Go rejects a byte order mark anywhere but the first
		// character of a file, string literals included.
		{"\ufeffa\ufeff", "a"},
		{"\u0085a\u0085", "\u0085a\u0085"}, // NEL: Go trims, JavaScript does not
		{"", ""},
		{"   ", ""},
		{"a b", "a b"},
	} {
		if got := encode(tc.in).trim().String(); got != tc.want {
			t.Errorf("trim(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	// The two rows above that separate the sets, stated as the claim they make.
	if strings.TrimSpace("\ufeffa") == "a" {
		t.Error("strings.TrimSpace now strips U+FEFF; the sets no longer differ there")
	}
	if strings.TrimSpace("\u0085a") != "a" {
		t.Error("strings.TrimSpace no longer strips U+0085; the sets no longer differ there")
	}
}

// TestExtglobInnerIsNotTheBody covers parse.js:507-509, the accumulation rule
// whose result extglobClose reads at :574 and :582.
//
// `inner` is built from the *values of the tokens pushed while this extglob is
// the innermost open one*. `body` — which extglobClose computes two lines
// earlier, at :541 — is a slice of the input between the same two positions.
// They look interchangeable and are not: paren tokens are excluded from `inner`
// (:507), and a nested extglob's contents accumulate into the nested entry, so
// they never reach the outer one at all.
//
// The token gate cannot see the difference: substituting `body` for `inner` at
// both readers leaves it at 0 wrong. The measurement that does see it is the
// enumerated differential — 103 of 159,424 patterns — and this test is what
// keeps the property asserted without one.
func TestExtglobInnerIsNotTheBody(t *testing.T) {
	// For each: the outer extglob's body, and whether its inner holds the same
	// characters. The nested extglob owns the "*" and the "/" in the first two.
	differs := 0
	for _, tc := range []struct {
		pattern   string
		innerHas  string
		bodyHas   string
		sameChars bool
	}{
		{"!(+(*))", "", "*", false},
		{"!(+(a/b))", "", "/", false},
		{"!(*)", "*", "*", true},
		{"!(a/b)", "/", "/", true},
	} {
		s, err := newScanner(tc.pattern)
		if err != nil {
			t.Fatalf("newScanner(%q): %v", tc.pattern, err)
		}
		// Stop at the closing paren of the outer extglob so the stack entry is
		// still there to inspect: run() pops it.
		s.input = s.input[:len(s.input)-1]
		if err := s.run(); err != nil {
			t.Fatalf("run(%q): %v", tc.pattern, err)
		}
		if len(s.extglobs) == 0 {
			t.Fatalf("%q: no extglob left open to inspect", tc.pattern)
		}
		inner := s.extglobs[0].inner.String()
		body := tc.pattern[2 : len(tc.pattern)-1]

		for _, c := range tc.bodyHas {
			if !strings.ContainsRune(body, c) {
				t.Fatalf("%q: the test's own body %q does not contain %q", tc.pattern, body, c)
			}
		}
		if tc.sameChars {
			for _, c := range tc.innerHas {
				if !strings.ContainsRune(inner, c) {
					t.Errorf("%q: inner %q should carry %q, as the body does", tc.pattern, inner, c)
				}
			}
			continue
		}
		differs++
		for _, c := range tc.bodyHas {
			if strings.ContainsRune(inner, c) {
				t.Errorf("%q: inner %q carries %q, but %q belongs to the nested extglob — parse.js:507 accumulates into the innermost entry only",
					tc.pattern, inner, c, c)
			}
		}
	}
	if differs == 0 {
		t.Error("no case separated inner from body, so this test asserts nothing")
	}
}

// TestUnmatchedCloseParenTakesTheCounterNegative covers the unguarded decrement
// at parse.js:806.
//
// The ")" branch decrements state.parens whether or not anything opened one, so
// an unmatched ")" leaves the counter at -1. That is not cosmetic. Two live
// tests read it as a JavaScript truthiness rather than as a count — the output
// choice at :805 (`state.parens ? ')' : '\\)'`) and the dot branch at :1008
// (`(state.braces + state.parens) === 0`) — and both answer differently at -1
// than at 0. A Go transcription that clamps the counter at zero, which is what a
// negative depth invites, changes both.
//
// The token gate does not see it: clamping leaves it at 0 wrong. The enumerated
// differential does, on 29,640 of 159,424 patterns.
func TestUnmatchedCloseParenTakesTheCounterNegative(t *testing.T) {
	for _, tc := range []struct {
		pattern string
		want    int
	}{
		{")", -1},
		{"))", -2},
		{"a)b)c", -2},
		{"(a))", -1},
	} {
		s, err := newScanner(tc.pattern)
		if err != nil {
			t.Fatalf("newScanner(%q): %v", tc.pattern, err)
		}
		if err := s.run(); err != nil {
			t.Fatalf("run(%q): %v", tc.pattern, err)
		}
		if s.parens != tc.want {
			t.Errorf("Parse(%q): state.parens = %d, want %d — parse.js:806 decrements unguarded", tc.pattern, s.parens, tc.want)
		}
	}
}

// TestOversizeInputIsRejectedBeforeConversion checks that the maxLength guard
// runs on a count rather than on an allocation. Upstream reads input.length and
// throws without touching the string; converting first would allocate the very
// thing the guard exists to refuse.
func TestOversizeInputIsRejectedBeforeConversion(t *testing.T) {
	over := strings.Repeat("a", maxLength+1)
	allocs := testing.AllocsPerRun(10, func() {
		if _, err := newScanner(over); err == nil {
			t.Fatal("oversize input was accepted")
		}
	})
	// One allocation for the LengthError itself; the []rune and []uint16 the
	// conversion would need are both proportional to the input.
	if allocs > 4 {
		t.Errorf("rejecting oversize input took %v allocations, want the count to run before the conversion", allocs)
	}
}

// TestAngleGroupIntroIsTheJavaScriptRegexp pins the third helper in this package
// whose correctness the token gate cannot speak for.
//
// hasAngleGroupIntro stands for /<([!=]|\w+>)/ at parse.js:1032, the wider half
// of the pair that decides whether a "?" straight after a paren is emitted bare
// or escaped. Two things about it survive translation badly, and the corpus sees
// neither: applying each misreading to the scanner leaves the gate at 0 wrong.
//
//   - it is a *search*, not an anchored test. Its caller has already established
//     that remaining() starts with "<", so "is this a lookbehind or a named
//     group" is the sentence it looks like — but a "<...>" anywhere further along
//     satisfies it just as well.
//   - "\w" in a JavaScript regexp without the /u flag is exactly [A-Za-z0-9_].
//     It is not Unicode-aware, so "(?<é>" escapes its "?" where "(?<e>" does not.
//
// As with dropLast and escapeLast, what is asserted is the JavaScript regexp,
// not picomatch's answer for any pattern.
func TestAngleGroupIntroIsTheJavaScriptRegexp(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		// The two alternatives, matched at the start.
		{"<!", true},
		{"<=", true},
		{"<a>", true},
		{"<ab>", true},
		{"<_>", true},
		{"<9>", true},
		{"<a9_>", true},

		// No match: an empty name, a non-word character in it, or no ">".
		{"<>", false},
		{"<a", false},
		{"< >", false},
		{"<a b>", false},
		{"<a-b>", false},
		{"<", false},
		{"", false},
		{"abc", false},

		// Unanchored. The caller only reaches this helper when the input starts
		// with "<", so these are the rows an anchored reading gets wrong.
		{"<<!", true},
		{"<<=", true},
		{"<(<!", true},
		{"<a b<c>", true},
		{"<a b<c", false},

		// Backtracking cannot rescue a run that is not followed by ">": a
		// shorter \w+ would have to be followed by a word character.
		{"<abc!", false},
		{"<abc!>", false},
		{"<abc>!", true},

		// \w is ASCII. Every one of these is a letter to Go's unicode package
		// and not a word character to a non-/u JavaScript regexp.
		{"<é>", false},
		{"<aé>", false},
		{"<éa>", false},
		{"<ダ>", false},
		{"<\U0001F600>", false},
		{"<µ>", false},
		{"<é><a>", true},
	} {
		if got := hasAngleGroupIntro(encode(tc.in)); got != tc.want {
			t.Errorf(`hasAngleGroupIntro(%q) = %v, want %v (JavaScript /<([!=]|\w+>)/.test(%q))`,
				tc.in, got, tc.want, tc.in)
		}
	}
}

// TestLookaroundIntroOnAbsentCharacterIsFalse pins the other half of that pair,
// and the reason it is a (uint16, bool) rather than a uint16.
//
// Both halves of parse.js:1032 are regexp tests on a value that can be
// undefined, and JavaScript coerces before testing: /[!=<:]/.test(undefined)
// tests the string "undefined", which contains none of "!=<:". So the test is
// false, the guard around it is true, and a "?" that ends the input straight
// after a "(" is escaped — "(?" compiles to "\(\?".
//
// The same coercion runs at parse.js:1055 on peek(3), where the "!" branch has
// used it since before this helper had a second caller.
func TestLookaroundIntroOnAbsentCharacterIsFalse(t *testing.T) {
	if isLookaroundIntro(0, false) {
		t.Error("isLookaroundIntro(absent) = true, want false — /[!=<:]/.test(undefined) tests the string \"undefined\"")
	}
	// None of the characters in the string "undefined" is in the class either,
	// which is what makes the coercion invisible rather than merely harmless.
	for _, c := range "undefined" {
		if isLookaroundIntro(uint16(c), true) {
			t.Errorf("isLookaroundIntro(%q) = true — /[!=<:]/ must not match a character of \"undefined\"", c)
		}
	}
	for _, c := range "!=<:" {
		if !isLookaroundIntro(uint16(c), true) {
			t.Errorf("isLookaroundIntro(%q) = false, want true", c)
		}
	}
}
