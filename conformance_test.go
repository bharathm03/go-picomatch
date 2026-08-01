//go:build conformance

// Conformance harness: replays the behaviour recorded from upstream picomatch's
// own unmodified test suite against this port.
//
// It is behind a build tag on purpose. `go test ./...` covers the port's own
// tests and must stay green; `make conformance` runs this and reports the real
// parity number, which starts at 0% and is expected to climb. Keeping the two
// separate means the everyday signal never gets diluted, and the parity figure is
// never quietly rounded up.
//
//	make conformance                        # report only, never fails
//	PICOMATCH_PARITY_MIN=95 make conformance # gate at 95%
package picomatch_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"

	picomatch "github.com/bharathm03/go-picomatch"
	"github.com/bharathm03/go-picomatch/internal/testcase"
)

const fixturePath = "testdata/original/cases.jsonl"

// charAxisPath holds the supplementary fixtures covering the character domain.
//
// It is kept separate from testdata/original, and reported separately, on
// purpose. testdata/original is recorded from picomatch's own unmodified suite
// and nobody chose its contents, which is exactly what makes the parity number
// worth quoting. These cases were chosen — by `tools/mutate/run.js` showing five
// plausible port mistakes that no upstream fixture detects — so folding them
// into the same percentage would quietly mix a measurement with a target.
const charAxisPath = "testdata/charaxis/cases.jsonl"

// reportPath is written on every run so progress is reviewable over time.
const reportPath = "conformance-report.json"
const charAxisReportPath = "charaxis-report.json"

// apiStat tracks one API's replay outcome.
type apiStat struct {
	Total       int `json:"total"`
	Passed      int `json:"passed"`
	Failed      int `json:"failed"`
	Unsupported int `json:"unsupported"`
}

type report struct {
	Fixture     string              `json:"fixture"`
	Cases       int                 `json:"cases"`
	Replayable  int                 `json:"replayable"`
	Passed      int                 `json:"passed"`
	Failed      int                 `json:"failed"`
	Unsupported int                 `json:"unsupported"`
	ParityPct   float64             `json:"parityPct"`
	ByAPI       map[string]*apiStat `json:"byApi"`
	// FirstFailures keeps a bounded sample so the report stays readable.
	FirstFailures []string `json:"firstFailures"`
}

func TestConformance(t *testing.T) {
	rep := replaySet(t, fixturePath, "Run `make extract` to generate them.")
	writeReport(t, &rep, reportPath)
	logReport(t, &rep, reportPath)

	if min := parityFloor(t); rep.ParityPct < min {
		t.Fatalf("parity %.2f%% is below the required %.2f%%", rep.ParityPct, min)
	}
}

// TestCharacterAxis replays the supplementary character-domain fixtures.
//
// Reported separately from TestConformance so the headline parity figure stays
// derived purely from upstream's own tests. Each case here exists because
// tools/mutate/run.js proved the upstream suite cannot detect a specific
// mistake; see testdata/charaxis/summary.json for which mutation each axis kills.
func TestCharacterAxis(t *testing.T) {
	if _, err := os.Stat(charAxisPath); errors.Is(err, os.ErrNotExist) {
		t.Skip("character-axis fixtures not generated; run `make charaxis`")
	}

	rep := replaySet(t, charAxisPath, "Run `make charaxis` to generate them.")
	writeReport(t, &rep, charAxisReportPath)
	logReport(t, &rep, charAxisReportPath)

	if min := floorFor(t, "PICOMATCH_CHARAXIS_MIN"); rep.ParityPct < min {
		t.Fatalf("character-axis parity %.2f%% is below the required %.2f%%", rep.ParityPct, min)
	}
}

// replaySet replays one fixture file and returns its report.
func replaySet(t *testing.T, path, hint string) report {
	t.Helper()

	cases, err := testcase.Load(path)
	if err != nil {
		t.Fatalf("load fixtures: %v\n\n%s", err, hint)
	}
	if len(cases) == 0 {
		t.Fatalf("fixture file is empty: %s", path)
	}

	rep := report{Fixture: path, Cases: len(cases), ByAPI: map[string]*apiStat{}}

	for i := range cases {
		c := &cases[i]
		if !c.Replayable() {
			continue
		}
		rep.Replayable++

		key := c.Module + "." + c.API
		stat := rep.ByAPI[key]
		if stat == nil {
			stat = &apiStat{}
			rep.ByAPI[key] = stat
		}
		stat.Total++

		status, detail := replay(c)
		switch status {
		case statusPassed:
			stat.Passed++
			rep.Passed++
		case statusUnsupported:
			stat.Unsupported++
			rep.Unsupported++
		default:
			stat.Failed++
			rep.Failed++
			if len(rep.FirstFailures) < 25 {
				rep.FirstFailures = append(rep.FirstFailures,
					fmt.Sprintf("%s: %s [%s]", c.Name(), detail, c.Test))
			}
		}
	}

	// Parity is measured over cases the harness can actually express. Cases it
	// cannot replay are reported separately rather than counted as passes.
	comparable := rep.Passed + rep.Failed
	if comparable > 0 {
		rep.ParityPct = 100 * float64(rep.Passed) / float64(comparable)
	}
	return rep
}

type status int

const (
	statusPassed status = iota
	statusFailed
	statusUnsupported
)

// replay runs one recorded call against the Go port and compares the outcome.
func replay(c *testcase.Case) (status, string) {
	args, err := c.DecodedArgs()
	if err != nil {
		return statusUnsupported, "undecodable arguments: " + err.Error()
	}
	want, err := c.DecodedResult()
	if err != nil {
		return statusUnsupported, "undecodable result: " + err.Error()
	}

	switch c.Module + "." + c.API {
	case "lib/picomatch.isMatch":
		return replayIsMatch(c, args, want)
	case "index.matcher", "lib/picomatch.matcher":
		return replayMatcher(c, args, want)
	case "lib/scan.scan":
		return replayScan(c, args, want)

	// Deliberately out of scope for parity:
	//   *.picomatch — the factory itself; its matcher calls are replayed instead.
	//   makeRe/parse — ECMAScript regex source and JS parser state are
	//                  implementation details this port does not reproduce.
	//   utils.*     — internal helpers with no exported Go equivalent.
	default:
		return statusUnsupported, "no Go equivalent"
	}
}

func replayIsMatch(c *testcase.Case, args []any, want any) (status, string) {
	input, ok := testcase.AsString(testcase.Arg(args, 0))
	if !ok {
		return statusUnsupported, "non-string input"
	}
	patterns, ok := testcase.AsStrings(testcase.Arg(args, 1))
	if !ok {
		return statusUnsupported, "non-string pattern"
	}
	opts, ok := buildOptions(args, 2, c)
	if !ok {
		return statusUnsupported, "unmapped option"
	}

	got, err := picomatch.IsMatch(input, patterns, opts)
	return compareBool(c, want, got, err)
}

func replayMatcher(c *testcase.Case, args []any, want any) (status, string) {
	construct, err := c.DecodedConstruct()
	if err != nil {
		return statusUnsupported, "undecodable construction"
	}
	glob, ok := testcase.AsString(testcase.Arg(construct, 0))
	if !ok {
		return statusUnsupported, "non-string glob"
	}
	opts, ok := buildOptions(construct, 1, c)
	if !ok {
		return statusUnsupported, "unmapped option"
	}
	input, ok := testcase.AsString(testcase.Arg(args, 0))
	if !ok {
		return statusUnsupported, "non-string input"
	}

	p, err := picomatch.New(glob, opts)
	if err != nil || c.Error != nil {
		return compareError(c, err)
	}

	// `matcher(input, true)` returns a detail object rather than a bool.
	if detail, isObject := want.(map[string]any); isObject {
		res := p.MatchDetail(input)

		// isMatch is the assertion this branch exists for. If it is missing or
		// not a bool the fixture is a shape we did not anticipate, and the case
		// must be reported unsupported rather than passing without comparing
		// anything.
		wantMatch, ok := detail["isMatch"].(bool)
		if !ok {
			return statusUnsupported, "recorded detail has no boolean isMatch"
		}
		if wantMatch != res.IsMatch {
			return statusFailed, fmt.Sprintf("isMatch: want %v, got %v", wantMatch, res.IsMatch)
		}

		// Everything else the fixture recorded is checked too. `posix` is
		// upstream's own name for `options.windows` (lib/picomatch.js: `const
		// posix = opts.windows`), so it maps onto Result.Windows — leaving it
		// uncompared would let a port that ignores platform semantics entirely
		// pass every matcher case.
		return compareFields(detail, map[string]any{
			"glob": res.Glob, "input": res.Input, "output": res.Output,
			"posix": res.Windows,
		}, matcherFieldsNotCompared)
	}

	return compareBool(c, want, p.Match(input), nil)
}

func replayScan(c *testcase.Case, args []any, want any) (status, string) {
	input, ok := testcase.AsString(testcase.Arg(args, 0))
	if !ok {
		return statusUnsupported, "non-string input"
	}
	opts, ok := buildOptions(args, 1, c)
	if !ok {
		return statusUnsupported, "unmapped option"
	}

	// A recorded throw the port did not reproduce is a divergence, not an
	// inexpressible case; scoring it unsupported would drop it from the parity
	// denominator instead of counting it against us. compareError covers both
	// directions, so the two conditions are handled together.
	got, err := picomatch.Scan(input, opts)
	if err != nil || c.Error != nil {
		return compareError(c, err)
	}

	fields, ok := want.(map[string]any)
	if !ok {
		return statusUnsupported, "unexpected scan result shape"
	}

	// Every key the fixture recorded is compared; nothing about a scan result is
	// exempt, so the exclusion set is empty. `start`, `slashes` and `parts` are
	// recorded on every scan result (see summary.json resultShapes) and are part
	// of the contract: a port that reports the wrong offset, or drops the segment
	// list under Options.Parts, must not score a clean pass here.
	return compareFields(fields, map[string]any{
		"base": got.Base, "glob": got.Glob, "prefix": got.Prefix, "input": got.Input,
		"start":  got.Start,
		"isGlob": got.IsGlob, "isBrace": got.IsBrace, "isBracket": got.IsBracket,
		"isGlobstar": got.IsGlobstar, "isExtglob": got.IsExtglob,
		"negated": got.Negated, "negatedExtglob": got.NegatedExtglob,
		"parts": got.Parts, "slashes": got.Slashes,
	}, nil)
}

// matcherFieldsNotCompared lists the recorded matcher keys compareFields does
// not check, each with the reason it is exempt.
//
// It exists so that the exemptions are declared rather than implied. Any other
// recorded key that the harness fails to supply a value for is reported
// unsupported instead of being skipped in silence.
var matcherFieldsNotCompared = map[string]string{
	"isMatch": "compared directly in replayMatcher, before this call",
	"match":   "ECMAScript match object; not reproduced by this port",
	"regex":   "ECMAScript regex source; not reproduced by this port",
}

// compareFields checks every field the fixture recorded against the port's value.
//
// It iterates the recording, not the port's own map, and that direction is the
// whole safety property: a key upstream recorded but the harness forgot to
// supply is a harness gap, and is reported unsupported so it leaves the parity
// numerator and denominator both. Iterating the port's map instead would make
// such a key invisible — add a field to ScanResult, forget to list it here, and
// parity would keep reporting a clean pass over a field nobody compares.
//
// Keys the recording does not carry are simply absent from the loop, which is
// how the two recorded scan shapes (with and without `parts`/`slashes`) are
// handled without a special case.
func compareFields(recorded map[string]any, actual map[string]any, notCompared map[string]string) (status, string) {
	names := make([]string, 0, len(recorded))
	for name := range recorded {
		names = append(names, name)
	}
	sort.Strings(names) // deterministic first-failure reporting

	for _, name := range names {
		got, supplied := actual[name]
		if !supplied {
			if _, exempt := notCompared[name]; exempt {
				continue
			}
			return statusUnsupported, fmt.Sprintf("%s: recorded but not compared by the harness", name)
		}

		expected := recorded[name]
		equal, comparable := sameValue(expected, got)
		if !comparable {
			return statusUnsupported, fmt.Sprintf("%s: uncomparable recorded type %T", name, expected)
		}
		if !equal {
			return statusFailed, fmt.Sprintf("%s: want %v, got %v", name, expected, got)
		}
	}
	return statusPassed, ""
}

// sameValue compares a recorded fixture value against the port's value.
//
// It refuses to equate different types: formatting both sides and comparing the
// strings would make the recorded string "true" equal to a Go bool true, turning
// a real type-level divergence into a parity pass. The second return reports
// whether the recorded type is one this harness knows how to compare at all.
func sameValue(recorded, actual any) (equal, comparable bool) {
	switch want := recorded.(type) {
	case string:
		got, ok := actual.(string)
		return ok && want == got, ok
	case bool:
		got, ok := actual.(bool)
		return ok && want == got, ok
	case float64:
		got, ok := actual.(int)
		return ok && want == float64(got), ok
	case []any:
		switch got := actual.(type) {
		case []string:
			return sameSlice(want, got, func(a any, b string) bool { s, ok := a.(string); return ok && s == b }), true
		case []int:
			return sameSlice(want, got, func(a any, b int) bool { f, ok := a.(float64); return ok && f == float64(b) }), true
		default:
			return false, false
		}
	default:
		return false, false
	}
}

func sameSlice[T any](want []any, got []T, eq func(any, T) bool) bool {
	if len(want) != len(got) {
		return false
	}
	for i := range want {
		if !eq(want[i], got[i]) {
			return false
		}
	}
	return true
}

// compareBool checks a boolean-returning call, honouring a recorded throw.
func compareBool(c *testcase.Case, want any, got bool, err error) (status, string) {
	if c.Error != nil || err != nil {
		return compareError(c, err)
	}

	expected, ok := want.(bool)
	if !ok {
		return statusUnsupported, "non-boolean expectation"
	}
	if expected != got {
		return statusFailed, fmt.Sprintf("want %v, got %v", expected, got)
	}
	return statusPassed, ""
}

// compareError resolves a call where either side produced an error.
//
// A recorded throw is only reproduced if the port raises the *same* exception:
// same JavaScript constructor name, same message. Accepting any error for any
// recorded throw would score "Missing closing: )" as equivalent to "exceeds
// maximum allowed length", and would let a port pass the throw cases by failing
// for entirely unrelated reasons. The fixtures carry name and message — 22 cases
// across 8 distinct messages — so the comparison costs nothing but the code to
// do it.
//
// ErrNotImplemented is never a match. It is this port's placeholder, not a
// behavioural answer: matching it against a recorded throw would score the
// absence of an implementation as behavioural equivalence, while calling it
// unsupported would drop the case from the denominator entirely and report a
// flattering percentage over the handful of cases left. A missing implementation
// is precisely a failure to reproduce upstream's behaviour.
func compareError(c *testcase.Case, err error) (status, string) {
	if errors.Is(err, picomatch.ErrNotImplemented) {
		return statusFailed, "not implemented"
	}
	if c.Error == nil {
		return statusFailed, "unexpected error: " + err.Error()
	}
	if err == nil {
		return statusFailed, fmt.Sprintf("want %s %q, got no error", c.Error.Name, c.Error.Message)
	}

	// An error the port cannot describe in upstream's terms cannot be shown to be
	// upstream's error, so it does not count as one.
	var e *picomatch.Error
	if !errors.As(err, &e) {
		return statusFailed, fmt.Sprintf("want %s %q, got untyped error %q",
			c.Error.Name, c.Error.Message, err.Error())
	}
	if e.Name != c.Error.Name || e.Message != c.Error.Message {
		return statusFailed, fmt.Sprintf("want %s %q, got %s %q",
			c.Error.Name, c.Error.Message, e.Name, e.Message)
	}
	return statusPassed, ""
}

// inertOptions are keys the upstream suite passes that upstream itself never
// reads, verified by grepping every `opts.X` / `options.X` in tests/original/lib
// and the two entry points.
//
// `relaxSlashes` appears once, in test/slashes-posix.js, and is read nowhere:
// `makeRe("*")` and `makeRe("*", {relaxSlashes: true})` compile to identical
// sources and match identically. Listing it here rather than giving
// picomatch.Options a field for it keeps the Go API from promising behaviour
// upstream does not have.
var inertOptions = map[string]string{
	"relaxSlashes": "passed by test/slashes-posix.js; read nowhere in upstream lib",
}

// buildOptions maps a recorded options object onto [picomatch.Options].
//
// It returns ok=false for any key it does not recognise, so a newly introduced
// upstream option surfaces as an unsupported case rather than being silently
// ignored and counted as a pass.
func buildOptions(args []any, index int, c *testcase.Case) (*picomatch.Options, bool) {
	raw, ok := testcase.OptionsArg(args, index)
	if !ok {
		return nil, false
	}

	opts := &picomatch.Options{Windows: c.Windows()}

	boolFields := map[string]*bool{
		"windows": &opts.Windows, "bash": &opts.Bash, "dot": &opts.Dot,
		"strictSlashes": &opts.StrictSlashes,
		"posix":         &opts.Posix, "regex": &opts.Regex, "basename": &opts.Basename,
		"matchBase": &opts.MatchBase, "nobrace": &opts.NoBrace,
		"nobracket": &opts.NoBracket, "strictBrackets": &opts.StrictBrackets,
		"noextglob": &opts.NoExtglob, "noext": &opts.NoExt,
		"noglobstar": &opts.NoGlobstar, "nonegate": &opts.NoNegate,
		"noparen": &opts.NoParen, "nocase": &opts.NoCase, "capture": &opts.Capture,
		"contains": &opts.Contains, "unescape": &opts.Unescape,
		"keepQuotes": &opts.KeepQuotes,
		"scanToEnd":  &opts.ScanToEnd, "parts": &opts.Parts,
	}

	for key, value := range raw {
		if testcase.IsAbsent(value) {
			continue
		}

		// An option the suite passes but upstream never reads changes no
		// behaviour, so it maps to no field. It is still recognised here: the
		// alternative is to report the case unsupported and drop it from the
		// denominator over a key that provably cannot affect the answer.
		if _, inert := inertOptions[key]; inert {
			continue
		}

		if target, known := boolFields[key]; known {
			b, ok := testcase.AsBool(value)
			if !ok {
				return nil, false
			}
			*target = b
			continue
		}

		switch key {
		case "ignore":
			patterns, ok := testcase.AsStrings(value)
			if !ok {
				return nil, false
			}
			opts.Ignore = patterns
		case "flags":
			s, ok := testcase.AsString(value)
			if !ok {
				return nil, false
			}
			opts.Flags = s
		case "maxLength":
			n, ok := testcase.AsNumber(value)
			if !ok {
				return nil, false
			}
			opts.MaxLength = int(n)
		case "maxExtglobRecursion":
			// Upstream accepts a number or `false` here; the two must not
			// collapse onto the same Go value.
			if n, ok := testcase.AsNumber(value); ok {
				cap := int(n)
				opts.MaxExtglobRecursion = &cap
				continue
			}
			if b, ok := testcase.AsBool(value); ok && !b {
				opts.UnlimitedExtglobRecursion = true
				continue
			}
			return nil, false
		default:
			// Includes the callback options; those cases are already excluded
			// upstream of here by Case.Replayable.
			return nil, false
		}
	}

	return opts, true
}

// TestCompareFieldsRejectsUncomparedKeys pins the safety property compareFields
// exists for.
//
// Until the matcher lands, every replay path errors before reaching
// compareFields, so nothing at runtime would notice if it went back to iterating
// the port's own map — and the regression it guards against is silent by
// construction: an unchecked field reports as parity, not as a failure. This
// test is what makes the property observable in the meantime.
func TestCompareFieldsRejectsUncomparedKeys(t *testing.T) {
	recorded := map[string]any{"base": "a", "glob": "*", "start": float64(0)}

	t.Run("recorded key the harness does not supply", func(t *testing.T) {
		got, detail := compareFields(recorded, map[string]any{"base": "a", "glob": "*"}, nil)
		if got != statusUnsupported {
			t.Fatalf("status = %v, want statusUnsupported (detail: %s)", got, detail)
		}
	})

	t.Run("...unless it is declared exempt", func(t *testing.T) {
		exempt := map[string]string{"start": "test"}
		if got, detail := compareFields(recorded, map[string]any{"base": "a", "glob": "*"}, exempt); got != statusPassed {
			t.Fatalf("status = %v, want statusPassed (detail: %s)", got, detail)
		}
	})

	t.Run("keys absent from the recording are not required", func(t *testing.T) {
		actual := map[string]any{"base": "a", "glob": "*", "start": 0, "parts": []string{"a"}}
		if got, detail := compareFields(recorded, actual, nil); got != statusPassed {
			t.Fatalf("status = %v, want statusPassed (detail: %s)", got, detail)
		}
	})

	t.Run("a supplied key that differs still fails", func(t *testing.T) {
		actual := map[string]any{"base": "b", "glob": "*", "start": 0}
		if got, _ := compareFields(recorded, actual, nil); got != statusFailed {
			t.Fatalf("status = %v, want statusFailed", got)
		}
	})
}

// TestMatcherExemptionsAreDeclared checks that the matcher exemption list still
// describes the recorded shape, so a new key in the recording cannot be exempted
// by accident.
func TestMatcherExemptionsAreDeclared(t *testing.T) {
	for _, key := range []string{"isMatch", "match", "regex"} {
		if _, ok := matcherFieldsNotCompared[key]; !ok {
			t.Errorf("%q is no longer declared exempt", key)
		}
	}
	if len(matcherFieldsNotCompared) != 3 {
		t.Errorf("exemption list has %d entries, want 3 — a new exemption needs a stated reason",
			len(matcherFieldsNotCompared))
	}
}

func parityFloor(t *testing.T) float64 { return floorFor(t, "PICOMATCH_PARITY_MIN") }

func floorFor(t *testing.T, env string) float64 {
	t.Helper()
	raw := os.Getenv(env)
	if raw == "" {
		return 0
	}
	min, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		t.Fatalf("%s=%q is not a number", env, raw)
	}
	return min
}

func writeReport(t *testing.T, rep *report, path string) {
	t.Helper()
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		t.Fatalf("encode report: %v", err)
	}
	if err := os.WriteFile(filepath.Clean(path), append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write report: %v", err)
	}
}

func logReport(t *testing.T, rep *report, reportedTo string) {
	t.Helper()
	t.Logf("%s: cases=%d replayable=%d passed=%d failed=%d unsupported=%d parity=%.2f%%",
		rep.Fixture, rep.Cases, rep.Replayable, rep.Passed, rep.Failed, rep.Unsupported, rep.ParityPct)

	keys := make([]string, 0, len(rep.ByAPI))
	for k := range rep.ByAPI {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		s := rep.ByAPI[k]
		t.Logf("  %-26s total=%-6d passed=%-6d failed=%-6d unsupported=%d",
			k, s.Total, s.Passed, s.Failed, s.Unsupported)
	}
	t.Logf("report written to %s", reportedTo)
}
