package parse

// Tests for the brace branch and the ECMAScript acceptance predicate under it.
// Same footing as scanner_test.go: nothing here states picomatch's answer for a
// pattern — testdata/tokens holds those — and the one table that does state a
// foreign semantic states JavaScript's, not picomatch's.

import "testing"

// TestECMARegExpValidIsTheJavaScriptGrammar pins ecmaRegExpValid against the
// RegExp constructor, on the same footing as dropLast, escapeLast, trim and
// hasAngleGroupIntro in scanner_test.go: what is asserted is a JavaScript
// semantic, not picomatch's answer for any pattern.
//
// It is here rather than left to the differential because the differential needs
// Node and this suite must run without it — and because the predicate is the one
// place in the port whose upstream is an *engine* rather than a grammar
// (DECISIONS.md §15). Every row was produced by running
// `try { new RegExp(src) } catch (e) { ... }` under node, not reasoned out.
//
// The rows are grouped by the failure the constructor reports, because that is
// what makes the set finite: for a non-unicode pattern with no flags there are
// twelve, and Annex B lets everything else through.
func TestECMARegExpValidIsTheJavaScriptGrammar(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want bool
	}{
		// Range out of order, which is the failure expandRange is really asking
		// about. The endpoint *values* are what make it: an escape is compared
		// by the character it denotes, and a class escape has no single value at
		// all, so a "-" beside one is a literal member.
		{"[a-b]", true}, {"[b-a]", false}, {"[ac-b]", false},
		{"[a-b-c]", true}, {"[-a]", true}, {"[a-]", true},
		{"[--a]", true}, {"[a--]", false}, {"[---]", true},
		{`[\d-a]`, true}, {`[a-\d]`, true}, {`[\w-\d]`, true},
		{`[\x41-\x42]`, true}, {`[\x41-B]`, true}, {`[B-\x41]`, false},
		{`[A-B]`, true}, {`[B-A]`, false},
		{`[\7-\10]`, true}, {`[\10-\7]`, false}, {`[\377-\377]`, true},
		{`[\8-\9]`, true}, {`[\9-\8]`, false}, {`[\08-\09]`, false},
		{`[\n-\r]`, true}, {`[\r-\n]`, false}, {`[\b]`, true}, {`[a-\b]`, false},
		{`[\cA-\cB]`, true}, {`[\cB-\cA]`, false}, {`[\c1]`, true},
		// "\c" with nothing controllable after it is a bare backslash and the
		// "c" is the next atom, so this is 'a' against U+005C, not against 'c'.
		{`[a-\c]`, false}, {`[\c-a]`, false}, {`[\c]`, true},
		// Non-unicode counts code units, so an astral range is two units each
		// way and the halves are what get compared.
		{"[\U0001F600-\U0001F601]", false},

		// Unterminated character class, and the rule that makes "[]" empty
		// rather than the "]" being its first member — the opposite of
		// picomatch's own bracket branch (docs/transcription-traps.md #31).
		{"[", false}, {"[a", false}, {`[a\]`, false}, {`[\c\]`, false},
		{"[]", true}, {"[^]", true}, {"[]]", true}, {"[^]]", true}, {"[]-a]", true},

		// "\" at end of pattern.
		{`\`, false}, {`a\`, false}, {`[a]\`, false}, {`\c`, true},

		// Nothing to repeat, and the lazy marker that is not a second
		// quantifier.
		{"*", false}, {"a*", true}, {"a**", false}, {"a*?", true},
		{"a*??", false}, {"a??", true}, {"^*", false}, {`\b*`, false},
		{"|*", false}, {"(*)", false}, {"a{1,2}{3}", false},
		// Annex B: a lookahead is quantifiable, a lookbehind is not.
		{"(?=a)*", true}, {"(?!a)?", true}, {"(?<=a)*", false}, {"(?<!a)+", false},

		// A "{" is a quantifier only when it parses as one; otherwise Annex B
		// makes it a literal, which is why five of these are legal.
		{"a{1}", true}, {"a{1,}", true}, {"a{1,2}", true}, {"a{2,1}", false},
		{"a{01,1}", true}, {"a{1,01}", true}, {"a{99999999999,1}", false},
		{"a{", true}, {"a{1", true}, {"a{,2}", true}, {"a{}", true},
		{"a{1,2,3}", true}, {"{1}", false}, {"{", true}, {"}", true}, {"]", true},

		// Groups.
		{"(a)", true}, {"(", false}, {"(a", false}, {")", false}, {"a)", false},
		{"(?", false}, {"(?a)", false}, {"(?:a)", true}, {"(?=a)", true},
		{"(?<=a)", true}, {"(?<!a)", true}, {"(?<a>)", true}, {"(?<>)", false},
		{"(?<1a>x)", false}, {"(?<$>x)", true}, {"(?<a b>x)", false},
		{"(?<a>x)(?<a>y)", false}, {"(?<a>x)(?<b>y)", true},
		// Group names are code points, and may be spelled with "\u" escapes even
		// though the pattern is not in unicode mode.
		{`(?<a>x)`, true}, {`(?<\u{61}>x)`, true}, {"(?<é>x)", true},
		{"(?<\U0001F600>x)", false}, {"(?<𝐀>x)", true},

		// "\k" is a named backreference exactly when the pattern declares a
		// GroupName somewhere — before it or after it — and an identity escape
		// otherwise. That is the whole reason the predicate needs a pre-scan.
		{`\k`, true}, {`\k<a>`, true}, {`\k<`, true}, {`[\k]`, true},
		{`(?<a>x)\k`, false}, {`(?<a>x)\k<a>`, true}, {`(?<a>x)\k<b>`, false},
		{`(?<a>x)\k<`, false}, {`(?<a>x)[\k]`, false},
		{`\k<a>(?<a>x)`, true}, {`\k<b>(?<a>x)`, false},

		// Annex B leniency the rest of the way: over-large backreferences,
		// legacy octal, malformed hex and unicode escapes, and "\p" without the
		// property syntax the /u flag would demand.
		{`\1`, true}, {`\9`, true}, {`(a)\2`, true}, {`\0`, true}, {`\00`, true},
		{`\x`, true}, {`\xZZ`, true}, {`\u`, true}, {`\uZZZZ`, true},
		{`\u{1}`, true}, {`\p{L}`, true}, {`\p`, true}, {`\q`, true},

		// Shapes expandRange actually produces, from the enumerated corpus.
		{"[a-c]", true}, {"[0-9]", true}, {`[.-\+]`, false}, {`[,-\$]`, false},
		{`[(?:\[a\]|[a])]`, false}, {`[[^a/]-a]`, true}, {`[[\]]-a]`, true},
	} {
		if got := ecmaRegExpValid(encode(tc.src)); got != tc.want {
			t.Errorf("ecmaRegExpValid(%q) = %v, want %v (JavaScript new RegExp(%q))",
				tc.src, got, tc.want, tc.src)
		}
	}

	// A lone trailing surrogate against a lone leading one. Go string literals
	// cannot spell it — an unpaired surrogate is not a valid code point — so it
	// is built as units, which is the representation the predicate works in and
	// the reason it can answer this at all. V8: new RegExp("[\uDE00-\uD83D]")
	// throws "Range out of order in character class".
	if ecmaRegExpValid(units{'[', 0xDE00, '-', 0xD83D, ']'}) {
		t.Error(`ecmaRegExpValid("[\uDE00-\uD83D]") = true, want false — 0xDE00 > 0xD83D as code units`)
	}
	if !ecmaRegExpValid(units{'[', 0xD83D, '-', 0xDE00, ']'}) {
		t.Error(`ecmaRegExpValid("[\uD83D-\uDE00]") = false, want true`)
	}
}

// TestBraceRangeUnwindsToTheBrace covers the pop loop at parse.js:911-919, which
// the token gate barely reaches: 3 of the 1,491 corpus patterns contain a
// "{a..b}" at all.
//
// What is asserted is the loop's own defining property rather than an answer:
// the pop runs *before* the type test, so the brace token goes with the tokens
// it opened, and a closed range leaves neither a "brace" nor a "dots" token
// behind. The "dots" type exists only between the second dot and the closing
// "}", which is why no recorded token in testdata/tokens carries it.
//
// The unclosed case at the end is the control. Without it the property would
// hold for free if the branch never produced a "dots" token at all.
func TestBraceRangeUnwindsToTheBrace(t *testing.T) {
	for _, p := range []string{"{a..b}", "{1..2}", "a/{a..c}", "{a..b..c}", "x{a..b}y", "{a...b}"} {
		st := mustParse(t, p)
		for i, tk := range st.Tokens {
			if tk.Type == "dots" {
				t.Errorf("Parse(%q): token %d is still a %q; the pop loop should have removed it", p, i, tk.Type)
			}
			if tk.Type == "brace" && tk.Value == "{" {
				t.Errorf("Parse(%q): token %d is the opening brace; the pop at :912 runs before the break at :914", p, i)
			}
		}
	}

	var sawDots bool
	for _, tk := range mustParse(t, "{a..b").Tokens {
		if tk.Type == "dots" {
			sawDots = true
		}
	}
	if !sawDots {
		t.Error(`Parse("{a..b"): no "dots" token survived an unclosed brace, so the loop above proves nothing`)
	}
}

// TestCommaInsideBracesNeedsTheStackTop covers parse.js:961-962, the only reader
// of `stack` in the whole file.
//
// Two tests, not one: a brace has to be open *and* be the innermost open
// construct. state.braces alone would also count a brace two levels out, and the
// two disagree whenever something else is open inside it.
func TestCommaInsideBracesNeedsTheStackTop(t *testing.T) {
	comma := func(p string) string {
		t.Helper()
		for _, tk := range mustParse(t, p).Tokens {
			if tk.Type == "comma" {
				if tk.Output == nil {
					t.Fatalf("Parse(%q): comma token with no output", p)
				}
				return *tk.Output
			}
		}
		t.Fatalf("Parse(%q): no comma token", p)
		return ""
	}
	if got := comma("{a,b}"); got != "|" {
		t.Errorf(`Parse("{a,b}"): comma output %q, want "|" — braces are on top of the stack`, got)
	}
	if got := comma("{a(b,c)}"); got != "," {
		t.Errorf(`Parse("{a(b,c)}"): comma output %q, want "," — parens are on top of the stack`, got)
	}
	if got := comma("a,b"); got != "," {
		t.Errorf(`Parse("a,b"): comma output %q, want "," — nothing is open`, got)
	}
}

// TestSortJSIsTheDefaultComparator pins Array.prototype.sort with no comparator:
// elements are compared as strings, which for UTF-16 code units is plain
// lexicographic order over those units — not by length, not by code point, and
// not by any locale rule.
//
// It matters because expandRange sorts before it builds the range, which is what
// stops "{z..a}" being an out-of-order class and sends it to "[a-z]" instead.
func TestSortJSIsTheDefaultComparator(t *testing.T) {
	for _, tc := range []struct{ in, want []string }{
		{[]string{"z", "a"}, []string{"a", "z"}},
		{[]string{"b", "a"}, []string{"a", "b"}},
		{[]string{"ab", "a"}, []string{"a", "ab"}},
		{[]string{"B", "a"}, []string{"B", "a"}},
		{[]string{"10", "9"}, []string{"10", "9"}},
		{[]string{"\U0001F600", "�"}, []string{"\U0001F600", "�"}},
		{[]string{"", "a"}, []string{"", "a"}},
	} {
		args := make([]units, len(tc.in))
		for i, s := range tc.in {
			args[i] = encode(s)
		}
		sortJS(args)
		for i := range args {
			if got := args[i].String(); got != tc.want[i] {
				t.Errorf("sortJS(%q)[%d] = %q, want %q", tc.in, i, got, tc.want[i])
			}
		}
	}
}

func mustParse(t *testing.T, p string) *State {
	t.Helper()
	st, err := Parse(p)
	if err != nil {
		t.Fatalf("Parse(%q): %v", p, err)
	}
	return st
}
