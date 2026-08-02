//go:build conformance

// Replays the recorded lib/scan.scan and lib/utils.basename calls against this
// package.
//
// It is behind the `conformance` build tag for the same reason the root
// harness is: `go test ./...` is the everyday signal and must stay fast and
// green. Run it with
//
//	go test -tags conformance -run TestScan -v ./internal/scan/
//
// Unlike the parity harness this one has no floor to raise over time. scan.js is
// ported in full, so every replayable case is expected to pass and any failure
// is a regression rather than an unbuilt branch — the same reasoning that makes
// the token gate's `wrong` column fail outright (DECISIONS.md §9).
package scan_test

import (
	"fmt"
	"path/filepath"
	"sort"
	"testing"

	"github.com/bharathm03/go-picomatch/internal/scan"
	"github.com/bharathm03/go-picomatch/internal/testcase"
)

var fixturePath = filepath.Join("..", "..", "testdata", "original", "cases.jsonl")

type outcome int

const (
	passed outcome = iota
	failed
	unsupported
)

type tally struct {
	total, passed, failed, unsupported int
	firstFailures                      []string
}

func (t *tally) record(name string, o outcome, detail string) {
	t.total++
	switch o {
	case passed:
		t.passed++
		return
	case failed:
		t.failed++
	case unsupported:
		t.unsupported++
	}

	// A bounded sample keeps the log readable while still naming the first
	// divergences, which is what a run is read for.
	if len(t.firstFailures) < 25 {
		t.firstFailures = append(t.firstFailures, fmt.Sprintf("%s: %s", name, detail))
	}
}

func (t *tally) pct() float64 {
	comparable := t.passed + t.failed
	if comparable == 0 {
		return 0
	}
	return 100 * float64(t.passed) / float64(comparable)
}

func (t *tally) report(tb testing.TB, label string) {
	tb.Helper()
	tb.Logf("%s: %d/%d passed (%.2f%%), %d failed, %d unsupported",
		label, t.passed, t.total, t.pct(), t.failed, t.unsupported)
	for _, f := range t.firstFailures {
		tb.Logf("  %s", f)
	}
	if t.failed > 0 || t.unsupported > 0 {
		tb.Errorf("%s: %d failed, %d unsupported; every recorded case is expected to replay",
			label, t.failed, t.unsupported)
	}
}

// TestScanConformance replays every recorded lib/scan.scan call.
func TestScanConformance(t *testing.T) {
	cases, err := testcase.Load(fixturePath)
	if err != nil {
		t.Fatalf("load fixtures: %v\n\nRun `make extract` to generate them.", err)
	}

	var scanned, basenamed tally

	for i := range cases {
		c := &cases[i]
		if !c.Replayable() {
			continue
		}

		switch c.Module + "." + c.API {
		case "lib/scan.scan":
			o, detail := replayScan(c)
			scanned.record(c.Name(), o, detail)
		case "lib/utils.basename":
			o, detail := replayBasename(c)
			basenamed.record(c.Name(), o, detail)
		}
	}

	if scanned.total == 0 || basenamed.total == 0 {
		t.Fatalf("fixtures held %d scan and %d basename cases; expected both to be non-zero",
			scanned.total, basenamed.total)
	}

	scanned.report(t, "lib/scan.scan")
	basenamed.report(t, "lib/utils.basename")
}

func replayScan(c *testcase.Case) (outcome, string) {
	args, err := c.DecodedArgs()
	if err != nil {
		return unsupported, "undecodable arguments: " + err.Error()
	}
	want, err := c.DecodedResult()
	if err != nil {
		return unsupported, "undecodable result: " + err.Error()
	}

	// Argument order is scan(input, options) — the pattern is args[0] here, not
	// args[1] as it is for isMatch(str, pattern). tools/probes/lib/corpus.js is
	// the definition; this call is the one that takes the pattern first.
	input, ok := testcase.AsString(testcase.Arg(args, 0))
	if !ok {
		return unsupported, "non-string input"
	}
	opts, ok := buildOptions(args, 1)
	if !ok {
		return unsupported, "unmapped option"
	}

	// Upstream's scan() has no throw path at all: no length guard, no rejected
	// syntax. A recorded error would mean the fixture set knows something this
	// port does not, so it is reported rather than absorbed.
	if c.Error != nil {
		return failed, fmt.Sprintf("recorded a throw (%s %q) from a function with no throw path",
			c.Error.Name, c.Error.Message)
	}

	fields, ok := want.(map[string]any)
	if !ok {
		return unsupported, "unexpected scan result shape"
	}

	got := scan.Scan(input, opts)

	// Every recorded key is compared. The iteration is over the recording, not
	// over this map, so a key upstream returns and this harness forgot to supply
	// is reported unsupported instead of passing uncompared.
	return compareFields(fields, map[string]any{
		"prefix": got.Prefix, "input": got.Input, "start": got.Start,
		"base": got.Base, "glob": got.Glob,
		"isBrace": got.IsBrace, "isBracket": got.IsBracket, "isGlob": got.IsGlob,
		"isExtglob": got.IsExtglob, "isGlobstar": got.IsGlobstar,
		"negated": got.Negated, "negatedExtglob": got.NegatedExtglob,
		"parts": got.Parts, "slashes": got.Slashes,
	})
}

func replayBasename(c *testcase.Case) (outcome, string) {
	args, err := c.DecodedArgs()
	if err != nil {
		return unsupported, "undecodable arguments: " + err.Error()
	}
	want, err := c.DecodedResult()
	if err != nil {
		return unsupported, "undecodable result: " + err.Error()
	}

	path, ok := testcase.AsString(testcase.Arg(args, 0))
	if !ok {
		return unsupported, "non-string path"
	}

	// basename reads one key, `windows`, from its own options argument. It does
	// not inherit the recording's platform: utils.js:63 destructures the
	// argument and consults nothing else, so the posix and windows recordings of
	// the same call are identical and both are replayed the same way.
	raw, ok := testcase.OptionsArg(args, 1)
	if !ok {
		return unsupported, "non-object options"
	}
	windows := false
	for key, value := range raw {
		if testcase.IsAbsent(value) {
			continue
		}
		if key != "windows" {
			return unsupported, "unmapped option " + key
		}
		b, ok := testcase.AsBool(value)
		if !ok {
			return unsupported, "non-boolean windows"
		}
		windows = b
	}

	expected, ok := testcase.AsString(want)
	if !ok {
		return unsupported, "non-string expectation"
	}

	got := scan.Basename(path, windows)
	if got != expected {
		return failed, fmt.Sprintf("want %q, got %q", expected, got)
	}
	return passed, ""
}

// buildOptions maps a recorded options object onto scan.Options.
//
// Only the keys lib/scan.js reads are mapped. Any other key returns ok=false, so
// an option this port does not model surfaces as an unsupported case rather than
// being ignored and scored as a pass.
func buildOptions(args []any, index int) (scan.Options, bool) {
	raw, ok := testcase.OptionsArg(args, index)
	if !ok {
		return scan.Options{}, false
	}

	var opts scan.Options
	fields := map[string]*bool{
		"noext": &opts.NoExt, "nonegate": &opts.NoNegate, "noparen": &opts.NoParen,
		"unescape": &opts.Unescape, "scanToEnd": &opts.ScanToEnd, "parts": &opts.Parts,
	}

	for key, value := range raw {
		if testcase.IsAbsent(value) {
			continue
		}
		target, known := fields[key]
		if !known {
			return scan.Options{}, false
		}
		b, ok := testcase.AsBool(value)
		if !ok {
			return scan.Options{}, false
		}
		*target = b
	}
	return opts, true
}

func compareFields(recorded, actual map[string]any) (outcome, string) {
	names := make([]string, 0, len(recorded))
	for name := range recorded {
		names = append(names, name)
	}
	sort.Strings(names) // deterministic first-failure reporting

	for _, name := range names {
		got, supplied := actual[name]
		if !supplied {
			return unsupported, fmt.Sprintf("%s: recorded but not compared by the harness", name)
		}

		equal, comparable := sameValue(recorded[name], got)
		if !comparable {
			return unsupported, fmt.Sprintf("%s: uncomparable recorded type %T", name, recorded[name])
		}
		if !equal {
			return failed, fmt.Sprintf("%s: want %#v, got %#v", name, recorded[name], got)
		}
	}
	return passed, ""
}

// sameValue refuses to equate different types: comparing formatted strings would
// make the recorded string "true" equal to a Go bool true, turning a type-level
// divergence into a pass.
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
