package parse

// Tests for the brace branch. Same footing as scanner_test.go: nothing here
// states picomatch's answer for a pattern — testdata/tokens holds those.
//
// The ECMAScript acceptance predicate expandRange consults used to be tested
// here too. It moved to internal/ecmaregexp with the code, on the day toRegex
// became its second caller.

import "testing"

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
	st, err := Parse(p, Options{})
	if err != nil {
		t.Fatalf("Parse(%q): %v", p, err)
	}
	return st
}
