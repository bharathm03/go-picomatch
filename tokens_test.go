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
	"strings"
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

	// Unbuilt and Wrong split the failures into the two kinds that mean
	// completely different things while the scanner is being written.
	//
	// Unbuilt is a pattern the scanner refused, having reached a construct it
	// does not implement. It is a failure — ErrUnsupported is scored exactly
	// like ErrNotImplemented, never as a skip — but it is an expected one, and
	// it shrinks as branches land.
	//
	// Wrong is a pattern the scanner answered and got different tokens for.
	// That is a bug in a branch that already exists, and unlike Unbuilt it is
	// not supposed to be non-zero at any point. Watch this number, not the
	// percentage: the percentage climbs on its own as constructs are added,
	// while this one only moves when something already built breaks.
	Unbuilt int `json:"unbuilt"`
	Wrong   int `json:"wrong"`

	// ByConstruct counts unbuilt patterns against the construct that stopped
	// them, so the report says what to build next rather than only how far
	// there is to go.
	ByConstruct map[string]int `json:"unbuiltByConstruct"`

	// ByTokenType counts failures against the token types involved, so a
	// systematic gap shows up as one type rather than as a list of patterns.
	ByTokenType map[string]int `json:"failuresByTokenType"`

	FirstFailures []string `json:"firstFailures"`

	// WrongPatterns lists every pattern in Wrong, uncapped. FirstFailures is
	// capped because nearly everything fails early on; this list is the one
	// that must stay empty, so truncating it would hide the thing it exists to
	// surface.
	WrongPatterns []string `json:"wrongPatterns"`
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

	rep := tokenReport{
		Fixture:     tokensPath,
		Cases:       len(cases),
		ByTokenType: map[string]int{},
		ByConstruct: map[string]int{},
	}

	for i := range cases {
		c := &cases[i]
		independent := c.FastpathIndependent()
		if independent {
			rep.FastpathIndependent++
		}

		detail, unbuilt := compareTokens(c)
		if detail == "" {
			rep.Passed++
			if independent {
				rep.FastpathIndependentOK++
			}
			continue
		}

		rep.Failed++
		if unbuilt != nil {
			rep.Unbuilt++
			rep.ByConstruct[fmt.Sprintf("%s (%s)", unbuilt.Construct, unbuilt.Site)]++
		} else {
			rep.Wrong++
			rep.WrongPatterns = append(rep.WrongPatterns, fmt.Sprintf("%q: %s", c.Pattern, detail))
		}
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
	t.Logf("  of %d failures: %d unbuilt (a construct the scanner refused), %d wrong (a built branch disagreed)",
		rep.Failed, rep.Unbuilt, rep.Wrong)
	logTopConstructs(t, &rep)
	logTopTokenTypes(t, &rep)
	t.Logf("report written to %s", tokensReportPath)

	// A wrong answer is not progress-shaped: it means a branch that exists
	// disagrees with the recording, and no amount of further building fixes it.
	// Unlike the percentage floor this is not opt-in, because there is no stage
	// of the port at which a nonzero value here is acceptable.
	if rep.Wrong > 0 {
		for _, p := range rep.WrongPatterns {
			t.Errorf("  %s", p)
		}
		t.Fatalf("%d pattern(s) parsed to the wrong tokens — see wrongPatterns in %s", rep.Wrong, tokensReportPath)
	}

	if min := floorFor(t, "PICOMATCH_TOKENS_MIN"); rep.Percent < min {
		t.Fatalf("token parity %.2f%% is below the required %.2f%%", rep.Percent, min)
	}
}

// compareTokens replays one case and returns "" on a match, or the first
// difference found. The second result is non-nil when the failure was the
// scanner declining a construct it has not been built for, which the caller
// scores separately from a disagreement.
//
// The first difference, not all of them: while the scanner is being written
// almost every case fails, and a report that listed every field of every token
// would be unreadable at exactly the moment it needs to be read.
func compareTokens(c *tokencase.Case) (string, *parse.UnsupportedError) {
	// Default options, unconditionally. testdata/tokens has no options axis:
	// tools/tokens/generate.js:63 records under `{fastpaths: false}` and nothing
	// else, and that is not a configurable key here — it is what parse.Parse
	// *is*, the form picomatch.parse itself always calls (picomatch.js:212), so
	// the Go zero value already means it. There is nothing on the case to thread
	// through, and inventing one would score the port against a configuration the
	// recording was not made under. The option surface is testdata/emit's axis,
	// and emit_test.go is where it is scored.
	got, err := parse.Parse(c.Pattern, parse.Options{})
	if err != nil {
		// Not-implemented is a failure, never a skip — the same rule
		// compareError applies in the conformance harness. Scoring it any other
		// way would let an absent scanner shrink the denominator instead of
		// counting against it.
		var unbuilt *parse.UnsupportedError
		if errors.As(err, &unbuilt) {
			if d := comparePrefix(c, got); d != "" {
				return d, nil
			}
			return "parse: " + err.Error(), unbuilt
		}
		return "parse: " + err.Error(), nil
	}
	if got == nil {
		return "parse returned no state and no error", nil
	}

	if got.Consumed != c.Consumed {
		return fmt.Sprintf("consumed: want %q, got %q", c.Consumed, got.Consumed), nil
	}
	if got.Output != c.Output {
		return fmt.Sprintf("output: want %q, got %q", c.Output, got.Output), nil
	}
	if got.Negated != c.Negated {
		return fmt.Sprintf("negated: want %v, got %v", c.Negated, got.Negated), nil
	}
	if got.Backtrack != c.Backtrack {
		return fmt.Sprintf("backtrack: want %v, got %v", c.Backtrack, got.Backtrack), nil
	}
	if len(got.Tokens) != len(c.Tokens) {
		return fmt.Sprintf("token count: want %d %v, got %d %v",
			len(c.Tokens), tokenTypes(c.Tokens), len(got.Tokens), portTokenTypes(got.Tokens)), nil
	}

	for i := range c.Tokens {
		if d := compareToken(c.Tokens[i], got.Tokens[i]); d != "" {
			return fmt.Sprintf("token %d (%s): %s", i, c.Tokens[i].Type, d), nil
		}
	}
	return "", nil
}

// comparePrefix scores the tokens a declined parse did produce against the
// recording's leading tokens, so that a wrong answer in a branch that exists is
// reported as Wrong rather than filed under the unbuilt construct that stopped
// the scanner further along.
//
// Without it the split is much weaker than it looks. [parse.Parse] gives up at
// the first construct it cannot handle, so any pattern containing one is scored
// from the error alone — and with 1,315 of 1,491 patterns in that state, "0
// wrong" would be a claim about the 176 that parse end to end. Some branches
// could never be scored at all: the "@" branch at scanner.go is only entered
// when the next character is "(", which is unsupported, so nothing it emits
// would ever be compared.
//
// State fields are deliberately not compared. Consumed and Output stop where the
// scanner did and the recording holds the whole pattern, so a difference there
// says nothing.
//
// # Why the last token is excluded
//
// A pushed token is not final. Upstream rewrites the one before the token being
// pushed — every retroactive assignment in parse.js is to `prev.<field>`, at
// :500-502, :722, :730, :867, :872, :999-1001, :1129-1132 and :1179-1236 — so
// the token adjacent to the construct that stopped the scanner is exactly the
// one the construct would have edited. Comparing it reports 592 of the 1,491
// corpus patterns as Wrong against a scanner that is right: "js/*.js" records
// the slash before the star with output "\\/(?!\\.)(?=.)", which the star branch
// writes, and there is no point at which a correct scanner has produced that
// without having built the star.
//
// Everything earlier is settled, because `prev` is always the last pushed token.
// The cost is real and worth naming: the token immediately before an unbuilt
// construct is still unscored, which is the case the "@" branch falls in.
func comparePrefix(c *tokencase.Case, got *parse.State) string {
	if got == nil {
		return ""
	}
	if len(got.Tokens) > len(c.Tokens) {
		return fmt.Sprintf("token count: declined after %d tokens %v, but only %d %v were recorded for the whole pattern",
			len(got.Tokens), portTokenTypes(got.Tokens), len(c.Tokens), tokenTypes(c.Tokens))
	}
	for i := 0; i < len(got.Tokens)-1; i++ {
		if d := compareToken(c.Tokens[i], got.Tokens[i]); d != "" {
			return fmt.Sprintf("token %d (%s), before the unbuilt construct: %s", i, c.Tokens[i].Type, d)
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

// logTopConstructs prints the constructs blocking the most patterns. It is the
// build order, measured: the top line is the branch worth writing next.
func logTopConstructs(t *testing.T, rep *tokenReport) {
	t.Helper()
	if len(rep.ByConstruct) == 0 {
		return
	}
	keys := make([]string, 0, len(rep.ByConstruct))
	for k := range rep.ByConstruct {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if rep.ByConstruct[keys[i]] != rep.ByConstruct[keys[j]] {
			return rep.ByConstruct[keys[i]] > rep.ByConstruct[keys[j]]
		}
		return keys[i] < keys[j]
	})
	if len(keys) > 8 {
		keys = keys[:8]
	}
	for _, k := range keys {
		t.Logf("  blocked on %-34s %d", k, rep.ByConstruct[k])
	}
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
// runs before any token is looked at, and the unbuilt/wrong split the report
// depends on.
//
// The expectation is built from the scanner's own output and then perturbed one
// field at a time. That is circular as a claim about picomatch and is not made
// as one — the recorded fixtures are the claim about picomatch. What it asserts
// is that the harness notices a difference at all, which is the failure mode
// that would otherwise read as a rising score.
func TestCompareTokensChecksStateAndLength(t *testing.T) {
	const pattern = "a/b"

	st, err := parse.Parse(pattern, parse.Options{})
	if err != nil {
		t.Fatalf("Parse(%q): %v", pattern, err)
	}

	base := func() tokencase.Case { return recordOf(pattern, st) }

	if d, unbuilt := compareTokens(ptr(base())); d != "" || unbuilt != nil {
		t.Fatalf("the scanner's own output did not compare equal: %s", d)
	}

	perturbations := map[string]func(*tokencase.Case){
		"consumed":    func(c *tokencase.Case) { c.Consumed += "x" },
		"output":      func(c *tokencase.Case) { c.Output += "x" },
		"negated":     func(c *tokencase.Case) { c.Negated = !c.Negated },
		"backtrack":   func(c *tokencase.Case) { c.Backtrack = !c.Backtrack },
		"token count": func(c *tokencase.Case) { c.Tokens = c.Tokens[:len(c.Tokens)-1] },
	}
	for name, perturb := range perturbations {
		t.Run(name, func(t *testing.T) {
			c := base()
			perturb(&c)
			d, unbuilt := compareTokens(&c)
			if d == "" {
				t.Fatalf("a %s difference compared equal", name)
			}
			if unbuilt != nil {
				t.Fatalf("a %s difference was reported as an unbuilt construct: %v", name, unbuilt)
			}
		})
	}

	// An error must never compare equal. This is the line TestTokenParity's
	// scoring rests on — it counts an empty detail as a pass, so any error
	// routed to "" would score every pattern the scanner refuses as a match and
	// print 100.00% over a scanner that implements nothing.
	//
	// The input is over maxLength, which cannot stop being an error: upstream
	// throws on it too, so unlike a choice of unbuilt construct this does not
	// expire as the scanner grows.
	overlong := strings.Repeat("a", 64*1024+1)
	d, unbuilt := compareTokens(&tokencase.Case{Pattern: overlong})
	if d == "" {
		t.Fatal("a parse error compared equal, which TestTokenParity scores as a pass")
	}
	if unbuilt != nil {
		t.Fatalf("a length error was classified as an unbuilt construct: %v", unbuilt)
	}

	// A construct the scanner declines must be reported as unbuilt, not as a
	// disagreement — the report's two columns mean different things and the
	// Wrong column is the one that fails the run.
	//
	// The construct is found rather than named. Naming one expires: "a*" heads
	// the measured build order, so the day the star branch lands a hardcoded
	// assertion fails on a correct change and accuses the classification instead
	// of itself.
	declined, ok := firstDeclinedConstruct()
	if !ok {
		t.Log("every candidate construct is built; nothing left here to classify as unbuilt")
		return
	}
	partial, err := parse.Parse(declined, parse.Options{})
	if !errors.Is(err, parse.ErrUnsupported) {
		t.Fatalf("Parse(%q): want ErrUnsupported, got %v", declined, err)
	}
	if _, unbuilt := compareTokens(ptr(recordOf(declined, partial))); unbuilt == nil {
		t.Fatalf("%q was declined by the scanner but not reported as unbuilt", declined)
	}
}

// firstDeclinedConstruct returns a pattern the scanner still refuses, and false
// once every candidate is built.
func firstDeclinedConstruct() (string, bool) {
	for _, p := range []string{"a*", "a?", "a[b]", "a{b,c}", "a(b)", "a@(b)", "a+(b)", "a!(b)"} {
		if _, err := parse.Parse(p, parse.Options{}); errors.Is(err, parse.ErrUnsupported) {
			return p, true
		}
	}
	return "", false
}

// recordOf re-encodes a scanner result in the fixture's shape, so a test can
// perturb one field of an otherwise-matching case.
//
// It carries every field compareToken looks at. Dropping one would not fail
// here — it would fail later, as a false Wrong against a correct scanner: leave
// out OutputIndex and the first brace token upstream records (parse.js:881-894
// sets outputIndex and tokensIndex on every one) reports "want absent, got 0",
// blaming the scanner for what the conversion lost.
func recordOf(pattern string, st *parse.State) tokencase.Case {
	c := tokencase.Case{
		Pattern:   pattern,
		Consumed:  st.Consumed,
		Output:    st.Output,
		Negated:   st.Negated,
		Backtrack: st.Backtrack,
	}
	for _, tok := range st.Tokens {
		rec := tokencase.Token{
			Type: tok.Type, Value: tok.Value,
			Extglob: tok.Extglob, Posix: tok.Posix, Comma: tok.Comma, Star: tok.Star,
		}
		if tok.Output != nil {
			rec.Output = ptr(*tok.Output)
		}
		if tok.OutputIndex != nil {
			rec.OutputIndex = ptr(*tok.OutputIndex)
		}
		if tok.TokensIndex != nil {
			rec.TokensIndex = ptr(*tok.TokensIndex)
		}
		c.Tokens = append(c.Tokens, rec)
	}
	return c
}

func ptr[T any](v T) *T { return &v }

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
