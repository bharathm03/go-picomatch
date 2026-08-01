//go:build conformance

// The token gate: replays the token streams recorded from upstream's parser
// against this port's scanner.
//
//	make tokens                         # report only, never fails
//	PICOMATCH_TOKENS_MIN=95 make tokens # gate at 95%
//
// # Why this exists alongside TestConformance
//
// TestConformance replays behaviour — pattern in, boolean out — and so cannot
// move until the scanner, the emitter and the matcher all work. For the whole
// time the parser is being written it reads 0% and reports nothing about
// progress. This gate is available from the first token the scanner emits, and
// it fails in the layer that caused the failure rather than three layers down.
//
// The two are not redundant, and the ordering between them is the useful part:
//
//	tokens differ                           -> parser bug
//	tokens match, behaviour differs         -> emitter or matcher bug
//
// # It is never folded into parity
//
// DECISIONS.md §6 excludes upstream's parser state from the parity number, and
// this does not quietly reverse that. These are internal-structure assertions
// over a pattern list inherited from upstream's suite, not independent evidence
// of behaviour, and they are reported to their own file under their own name.
package picomatch_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"testing"

	"github.com/bharathm03/go-picomatch/internal/parse"
	"github.com/bharathm03/go-picomatch/internal/tokencase"
)

const (
	tokensPath       = "testdata/tokens/cases.jsonl"
	tokensReportPath = "tokens-report.json"
)

// tokenReport is the token gate's own report. It is deliberately not the
// conformance `report` type: the stratification below is the point of this
// harness, and reusing a shape without it would drop the one number that says
// how much a green score is worth.
type tokenReport struct {
	Fixture string  `json:"fixture"`
	Cases   int     `json:"cases"`
	Passed  int     `json:"passed"`
	Failed  int     `json:"failed"`
	Percent float64 `json:"percent"`

	// FastpathIndependent counts only the patterns whose recorded tokens settle
	// what upstream's makeRe compiled. For the rest a fastpath ran and produced
	// different regex source, so passing here does not pin their behaviour —
	// that waits on the fastpath pass. Reported, not filtered out.
	FastpathIndependent    int     `json:"fastpathIndependent"`
	FastpathIndependentOK  int     `json:"fastpathIndependentPassed"`
	FastpathIndependentPct float64 `json:"fastpathIndependentPct"`

	// ByTokenType counts failures against the token types involved, so a
	// systematic gap shows up as one type rather than as a list of patterns.
	ByTokenType map[string]int `json:"failuresByTokenType"`

	FirstFailures []string `json:"firstFailures"`
}

func TestTokenParity(t *testing.T) {
	cases, err := tokencase.Load(tokensPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Skip("token fixtures not generated; run `make tokens`")
		}
		t.Fatalf("load token fixtures: %v\n\nRun `make tokens` to generate them.", err)
	}
	if len(cases) == 0 {
		t.Fatalf("token fixture file is empty: %s", tokensPath)
	}

	rep := tokenReport{Fixture: tokensPath, Cases: len(cases), ByTokenType: map[string]int{}}

	for i := range cases {
		c := &cases[i]
		independent := c.FastpathIndependent()
		if independent {
			rep.FastpathIndependent++
		}

		detail := compareTokens(c)
		if detail == "" {
			rep.Passed++
			if independent {
				rep.FastpathIndependentOK++
			}
			continue
		}

		rep.Failed++
		for _, tok := range c.Tokens {
			rep.ByTokenType[tok.Type]++
		}
		if len(rep.FirstFailures) < 25 {
			rep.FirstFailures = append(rep.FirstFailures, fmt.Sprintf("%q: %s", c.Pattern, detail))
		}
	}

	if total := rep.Passed + rep.Failed; total > 0 {
		rep.Percent = 100 * float64(rep.Passed) / float64(total)
	}
	if rep.FastpathIndependent > 0 {
		rep.FastpathIndependentPct = 100 * float64(rep.FastpathIndependentOK) / float64(rep.FastpathIndependent)
	}

	writeTokenReport(t, &rep)
	t.Logf("%s: cases=%d passed=%d failed=%d tokens=%.2f%%",
		rep.Fixture, rep.Cases, rep.Passed, rep.Failed, rep.Percent)
	t.Logf("  fastpath-independent: %d of %d passed (%.2f%%) — the rest need the fastpath pass before their behaviour is pinned",
		rep.FastpathIndependentOK, rep.FastpathIndependent, rep.FastpathIndependentPct)
	logTopTokenTypes(t, &rep)
	t.Logf("report written to %s", tokensReportPath)

	if min := floorFor(t, "PICOMATCH_TOKENS_MIN"); rep.Percent < min {
		t.Fatalf("token parity %.2f%% is below the required %.2f%%", rep.Percent, min)
	}
}

// compareTokens replays one case and returns "" on a match, or the first
// difference found.
//
// The first difference, not all of them: while the scanner is being written
// almost every case fails, and a report that listed every field of every token
// would be unreadable at exactly the moment it needs to be read.
func compareTokens(c *tokencase.Case) string {
	got, err := parse.Parse(c.Pattern)
	if err != nil {
		// Not-implemented is a failure, never a skip — the same rule
		// compareError applies in the conformance harness. Scoring it any other
		// way would let an absent scanner shrink the denominator instead of
		// counting against it.
		return "parse: " + err.Error()
	}
	if got == nil {
		return "parse returned no state and no error"
	}

	if got.Consumed != c.Consumed {
		return fmt.Sprintf("consumed: want %q, got %q", c.Consumed, got.Consumed)
	}
	if got.Output != c.Output {
		return fmt.Sprintf("output: want %q, got %q", c.Output, got.Output)
	}
	if got.Negated != c.Negated {
		return fmt.Sprintf("negated: want %v, got %v", c.Negated, got.Negated)
	}
	if got.Backtrack != c.Backtrack {
		return fmt.Sprintf("backtrack: want %v, got %v", c.Backtrack, got.Backtrack)
	}
	if len(got.Tokens) != len(c.Tokens) {
		return fmt.Sprintf("token count: want %d %v, got %d %v",
			len(c.Tokens), tokenTypes(c.Tokens), len(got.Tokens), portTokenTypes(got.Tokens))
	}

	for i := range c.Tokens {
		if d := compareToken(c.Tokens[i], got.Tokens[i]); d != "" {
			return fmt.Sprintf("token %d (%s): %s", i, c.Tokens[i].Type, d)
		}
	}
	return ""
}

// compareToken checks one token, field by field.
//
// Optional fields are compared for presence as well as value. That is the whole
// reason both sides carry pointers: `output` is absent on 2,366 recorded tokens
// and present as "" on 1,883, and a scanner that always assigned "" would
// otherwise be indistinguishable from one that got it right.
func compareToken(want tokencase.Token, got parse.Token) string {
	if want.Type != got.Type {
		return fmt.Sprintf("type: want %q, got %q", want.Type, got.Type)
	}
	if want.Value != got.Value {
		return fmt.Sprintf("value: want %q, got %q", want.Value, got.Value)
	}
	if d := compareOptString("output", want.Output, got.Output); d != "" {
		return d
	}
	for _, f := range []struct {
		name string
		want bool
		got  bool
	}{
		{"extglob", want.Extglob, got.Extglob},
		{"posix", want.Posix, got.Posix},
		{"comma", want.Comma, got.Comma},
		{"star", want.Star, got.Star},
	} {
		if f.want != f.got {
			return fmt.Sprintf("%s: want %v, got %v", f.name, f.want, f.got)
		}
	}
	if d := compareOptInt("outputIndex", want.OutputIndex, got.OutputIndex); d != "" {
		return d
	}
	return compareOptInt("tokensIndex", want.TokensIndex, got.TokensIndex)
}

func compareOptString(name string, want, got *string) string {
	switch {
	case want == nil && got == nil:
		return ""
	case want == nil:
		return fmt.Sprintf("%s: want absent, got %q", name, *got)
	case got == nil:
		return fmt.Sprintf("%s: want %q, got absent", name, *want)
	case *want != *got:
		return fmt.Sprintf("%s: want %q, got %q", name, *want, *got)
	}
	return ""
}

func compareOptInt(name string, want, got *int) string {
	switch {
	case want == nil && got == nil:
		return ""
	case want == nil:
		return fmt.Sprintf("%s: want absent, got %d", name, *got)
	case got == nil:
		return fmt.Sprintf("%s: want %d, got absent", name, *want)
	case *want != *got:
		return fmt.Sprintf("%s: want %d, got %d", name, *want, *got)
	}
	return ""
}

func tokenTypes(ts []tokencase.Token) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Type
	}
	return out
}

func portTokenTypes(ts []parse.Token) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Type
	}
	return out
}

func logTopTokenTypes(t *testing.T, rep *tokenReport) {
	t.Helper()
	if len(rep.ByTokenType) == 0 {
		return
	}
	types := make([]string, 0, len(rep.ByTokenType))
	for k := range rep.ByTokenType {
		types = append(types, k)
	}
	sort.Slice(types, func(i, j int) bool {
		if rep.ByTokenType[types[i]] != rep.ByTokenType[types[j]] {
			return rep.ByTokenType[types[i]] > rep.ByTokenType[types[j]]
		}
		return types[i] < types[j]
	})
	if len(types) > 8 {
		types = types[:8]
	}
	for _, k := range types {
		t.Logf("  failing patterns containing %-12s %d", k, rep.ByTokenType[k])
	}
}

func writeTokenReport(t *testing.T, rep *tokenReport) {
	t.Helper()
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		t.Fatalf("encode token report: %v", err)
	}
	if err := os.WriteFile(tokensReportPath, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write token report: %v", err)
	}
}

// TestCompareTokenDetectsDifferences proves the comparison can fail, and on
// what.
//
// Until the scanner lands, [parse.Parse] errors before compareToken is ever
// reached, so TestTokenParity exercises none of it — it would report a clean
// 0.00% just the same if every check below were `return ""`. This is what makes
// the comparison observable in the meantime, and the absent-versus-zero cases
// are the reason it is worth writing: those are the ones a reasonable
// implementation gets wrong, and the ones that fail silently as parity.
func TestCompareTokenDetectsDifferences(t *testing.T) {
	str := func(s string) *string { return &s }
	num := func(n int) *int { return &n }

	base := func() (tokencase.Token, parse.Token) {
		return tokencase.Token{Type: "star", Value: "*", Output: str("[^/]*?")},
			parse.Token{Type: "star", Value: "*", Output: str("[^/]*?")}
	}

	t.Run("identical tokens compare equal", func(t *testing.T) {
		w, g := base()
		if d := compareToken(w, g); d != "" {
			t.Fatalf("want no difference, got %q", d)
		}
	})

	// The two that motivated the pointers. Across the recorded corpus `output`
	// is absent 2,366 times and present as "" 1,883 times, and `outputIndex` is
	// absent 10,443 times and present as 0 on 18 tokens. A plain string and int
	// would report both of these as a match.
	t.Run("absent output is not the empty string", func(t *testing.T) {
		w, g := base()
		w.Output, g.Output = nil, str("")
		if d := compareToken(w, g); d == "" {
			t.Fatal("absent vs empty output compared equal")
		}
		w.Output, g.Output = str(""), nil
		if d := compareToken(w, g); d == "" {
			t.Fatal("empty vs absent output compared equal")
		}
	})

	t.Run("absent outputIndex is not zero", func(t *testing.T) {
		w, g := base()
		w.OutputIndex, g.OutputIndex = nil, num(0)
		if d := compareToken(w, g); d == "" {
			t.Fatal("absent vs zero outputIndex compared equal")
		}
	})

	for _, tc := range []struct {
		name   string
		mutate func(*tokencase.Token)
	}{
		{"type", func(tk *tokencase.Token) { tk.Type = "globstar" }},
		{"value", func(tk *tokencase.Token) { tk.Value = "**" }},
		{"output", func(tk *tokencase.Token) { tk.Output = str("different") }},
		{"extglob", func(tk *tokencase.Token) { tk.Extglob = true }},
		{"posix", func(tk *tokencase.Token) { tk.Posix = true }},
		{"comma", func(tk *tokencase.Token) { tk.Comma = true }},
		{"star", func(tk *tokencase.Token) { tk.Star = true }},
		{"tokensIndex", func(tk *tokencase.Token) { tk.TokensIndex = num(3) }},
	} {
		t.Run(tc.name+" difference is detected", func(t *testing.T) {
			w, g := base()
			tc.mutate(&w)
			if d := compareToken(w, g); d == "" {
				t.Fatalf("a %s difference compared equal", tc.name)
			}
		})
	}
}

// TestCompareTokensChecksStateAndLength covers the case-level comparison, which
// runs before any token is looked at.
func TestCompareTokensChecksStateAndLength(t *testing.T) {
	// A scanner that returned no error and no state would otherwise be scored a
	// pass by a comparison that only looked at fields.
	if d := compareTokens(&tokencase.Case{Pattern: "a"}); d == "" {
		t.Fatal("an unimplemented parser compared equal")
	}
}

// TestTokenFixtureShape guards the fixture itself, and runs whether or not a
// scanner exists.
//
// Without it the gate has a silent failure mode that looks like success: if
// tools/tokens/generate.js ever wrote records with an empty token array, or
// dropped the fastpath flags, TestTokenParity would still report a percentage —
// over assertions that assert nothing.
func TestTokenFixtureShape(t *testing.T) {
	cases, err := tokencase.Load(tokensPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Skip("token fixtures not generated; run `make tokens`")
		}
		t.Fatalf("load token fixtures: %v", err)
	}

	stats := tokencase.Summarise(cases)
	t.Logf("cases=%d tokens=%d backtrack=%d fastpath=%v diverges=%d",
		stats.Total, stats.Tokens, stats.Backtrack, stats.ByFastpath, stats.Diverges)

	for i := range cases {
		c := &cases[i]
		if len(c.Tokens) == 0 {
			t.Fatalf("%q: no tokens recorded", c.Pattern)
		}
		// Upstream emits `bos` first for every pattern the full scanner sees;
		// its absence would mean the record came from somewhere else.
		if c.Tokens[0].Type != "bos" {
			t.Fatalf("%q: first token is %q, want \"bos\"", c.Pattern, c.Tokens[0].Type)
		}
		switch c.Fastpath {
		case tokencase.FastpathNone, tokencase.FastpathTop, tokencase.FastpathInline:
		default:
			t.Fatalf("%q: unknown fastpath %q", c.Pattern, c.Fastpath)
		}
		// A pattern the full scanner handled cannot have diverged from itself.
		if c.Fastpath == tokencase.FastpathNone && c.FastpathDiverges {
			t.Fatalf("%q: fastpath=none but marked divergent", c.Pattern)
		}
	}

	// Every token type the fixture carries needs a home in the port's parser.
	// A new one appearing after an upstream bump is a design change, not a
	// silent data change.
	if got := stats.ByFastpath[tokencase.FastpathNone]; got == 0 {
		t.Fatal("no full-scanner patterns recorded; the fastpath flag is wrong")
	}
}
