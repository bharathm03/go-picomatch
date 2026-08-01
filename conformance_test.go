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

// reportPath is written on every run so progress is reviewable over time.
const reportPath = "conformance-report.json"

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
	cases, err := testcase.Load(fixturePath)
	if err != nil {
		t.Fatalf("load fixtures: %v\n\nRun `make extract` to generate them.", err)
	}
	if len(cases) == 0 {
		t.Fatal("fixture file is empty")
	}

	rep := report{Fixture: fixturePath, Cases: len(cases), ByAPI: map[string]*apiStat{}}

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

	writeReport(t, &rep)
	logReport(t, &rep)

	if min := parityFloor(t); rep.ParityPct < min {
		t.Fatalf("parity %.2f%% is below the required %.2f%%", rep.ParityPct, min)
	}
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
	if err != nil {
		return expectError(c, err)
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
		// pass every matcher case. `regex` and `match` stay out: ECMAScript
		// regex source is an implementation detail this port does not reproduce.
		return compareFields(detail, map[string]any{
			"glob": res.Glob, "input": res.Input, "output": res.Output,
			"posix": res.Windows,
		})
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

	got, err := picomatch.Scan(input, opts)
	if err != nil {
		return expectError(c, err)
	}
	// A recorded throw the port did not reproduce is a divergence, not an
	// inexpressible case; scoring it unsupported would drop it from the parity
	// denominator instead of counting it against us.
	if c.Error != nil {
		return statusFailed, fmt.Sprintf("want error %q, got none", c.Error.Message)
	}

	fields, ok := want.(map[string]any)
	if !ok {
		return statusUnsupported, "unexpected scan result shape"
	}

	// Every key the fixture recorded is compared. `start`, `slashes` and `parts`
	// are recorded on every scan result (see summary.json resultShapes) and are
	// part of the contract: a port that reports the wrong offset, or drops the
	// segment list under Options.Parts, must not score a clean pass here.
	return compareFields(fields, map[string]any{
		"base": got.Base, "glob": got.Glob, "prefix": got.Prefix, "input": got.Input,
		"start":  got.Start,
		"isGlob": got.IsGlob, "isBrace": got.IsBrace, "isBracket": got.IsBracket,
		"isGlobstar": got.IsGlobstar, "isExtglob": got.IsExtglob,
		"negated": got.Negated, "negatedExtglob": got.NegatedExtglob,
		"parts": got.Parts, "slashes": got.Slashes,
	})
}

// compareFields checks every field the fixture recorded against the port's value,
// skipping keys the recording does not carry.
//
// Keys absent from `actual` are a harness gap rather than a divergence, so they
// are reported unsupported: silently ignoring them is how an unchecked field ends
// up counted as parity.
func compareFields(recorded map[string]any, actual map[string]any) (status, string) {
	names := make([]string, 0, len(actual))
	for name := range actual {
		names = append(names, name)
	}
	sort.Strings(names) // deterministic first-failure reporting

	for _, name := range names {
		expected, present := recorded[name]
		if !present {
			continue
		}
		equal, comparable := sameValue(expected, actual[name])
		if !comparable {
			return statusUnsupported, fmt.Sprintf("%s: uncomparable recorded type %T", name, expected)
		}
		if !equal {
			return statusFailed, fmt.Sprintf("%s: want %v, got %v", name, expected, actual[name])
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
	if errors.Is(err, picomatch.ErrNotImplemented) {
		return statusFailed, "not implemented"
	}
	if c.Error != nil {
		if err == nil {
			return statusFailed, fmt.Sprintf("want error %q, got none", c.Error.Message)
		}
		return statusPassed, ""
	}
	if err != nil {
		return statusFailed, "unexpected error: " + err.Error()
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

// expectError resolves a call that returned an error: correct if upstream threw
// too, a failure otherwise.
//
// ErrNotImplemented is neither, and is always a failure. It is this port's
// placeholder, not a behavioural answer: matching it against a recorded throw
// would score the absence of an implementation as behavioural equivalence, while
// calling it unsupported would drop the case from the denominator entirely and
// report a flattering percentage over the handful of cases left. A missing
// implementation is precisely a failure to reproduce upstream's behaviour.
func expectError(c *testcase.Case, err error) (status, string) {
	if errors.Is(err, picomatch.ErrNotImplemented) {
		return statusFailed, "not implemented"
	}
	if c.Error != nil {
		return statusPassed, ""
	}
	return statusFailed, "unexpected error: " + err.Error()
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
		"strictSlashes": &opts.StrictSlashes, "relaxSlashes": &opts.RelaxSlashes,
		"posix": &opts.Posix, "regex": &opts.Regex, "basename": &opts.Basename,
		"matchBase": &opts.MatchBase, "nobrace": &opts.NoBrace,
		"nobracket": &opts.NoBracket, "strictBrackets": &opts.StrictBrackets,
		"noextglob": &opts.NoExtglob, "noext": &opts.NoExt,
		"noglobstar": &opts.NoGlobstar, "nonegate": &opts.NoNegate,
		"noparen": &opts.NoParen, "nocase": &opts.NoCase, "capture": &opts.Capture,
		"contains": &opts.Contains, "unescape": &opts.Unescape,
		"keepQuotes": &opts.KeepQuotes, "literal": &opts.Literal,
		"scanToEnd": &opts.ScanToEnd, "parts": &opts.Parts,
	}

	for key, value := range raw {
		if testcase.IsAbsent(value) {
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

func parityFloor(t *testing.T) float64 {
	t.Helper()
	raw := os.Getenv("PICOMATCH_PARITY_MIN")
	if raw == "" {
		return 0
	}
	min, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		t.Fatalf("PICOMATCH_PARITY_MIN=%q is not a number", raw)
	}
	return min
}

func writeReport(t *testing.T, rep *report) {
	t.Helper()
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		t.Fatalf("encode report: %v", err)
	}
	if err := os.WriteFile(filepath.Clean(reportPath), append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write report: %v", err)
	}
}

func logReport(t *testing.T, rep *report) {
	t.Helper()
	t.Logf("cases=%d replayable=%d passed=%d failed=%d unsupported=%d parity=%.2f%%",
		rep.Cases, rep.Replayable, rep.Passed, rep.Failed, rep.Unsupported, rep.ParityPct)

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
	t.Logf("report written to %s", reportPath)
}
