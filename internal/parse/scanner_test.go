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
}

// unbuiltPatterns are constructs the scanner declines. The list shrinks as
// branches land; a pattern that starts parsing simply stops being checked here.
//
// "***" and "**/**" are here rather than above because REPLACEMENTS rewrites
// them to "*" and "**" before the loop starts (parse.js:361), so what the
// scanner declines is not what the caller passed.
var unbuiltPatterns = []string{
	"*", "a*", "?", "a?", "[a]", "{a}", "(a)", "@(a)", "+(a)", "!(a)",
	"***", "**/**", "**/**/**",
}

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
	// token, which is the path that appends to both value and output.
	for _, pattern := range []string{"a.b.c.d", `a\.b\.c`, "a.b}c|d,e", `"ab"cd"ef"`, strings.Repeat("a.", 64)} {
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
		var fields []field
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
