//go:build conformance

// The emitter gate: replays the emitter and compiler output recorded from
// upstream against this port.
//
//	make emit                        # report only, never fails on the percentage
//	PICOMATCH_EMIT_MIN=25 make emit  # gate at 25%
//
// # What this localises that TestTokenParity does not
//
// TestTokenParity already pins state.output for the full scanner, and pins it
// hard — 1,491 of 1,491, 0 wrong. But it does so under DEFAULT options only, for
// the full scanner only, and it stops at the bare output. Three things it cannot
// see at all:
//
//	the option surface     only 1,020 of these 2,038 pairs use default options
//	parse.fastpaths()      a second emitter with its own constants table
//	picomatch.compileRe    the ^(?:X)$ wrap, the negation wrap, and the flags
//
// So the ordering between the three gates is:
//
//	tokens differ                            -> parser bug     (make tokens)
//	tokens match, source differs             -> emitter bug    (make emit)
//	tokens + source match, behaviour differs -> matcher bug    (make conformance)
//
// # It is never folded into parity
//
// Same line DECISIONS.md §6 draws for testdata/tokens: these are upstream's own
// patterns but upstream's internal state, not independent evidence of behaviour.
// They are reported to their own file under their own name.
package picomatch_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	picomatch "github.com/bharathm03/go-picomatch"
	"github.com/bharathm03/go-picomatch/internal/emitcase"
	"github.com/bharathm03/go-picomatch/internal/parse"
)

const (
	emitPath       = "testdata/emit/cases.jsonl"
	emitReportPath = "emit-report.json"
)

// emitBlockers names, for each recorded field the port has no entry point for at
// all, what has to exist before the field can even be attempted.
//
// It is declared rather than inferred, and TestEmitAttemptabilityIsDeclared pins
// both its contents and its length, so a new "the port cannot do this yet" entry
// cannot be added without a stated reason. Adding a field here is how a real
// disagreement gets hidden: an unbuilt field is still a failure, but it is an
// expected one and nobody reads the list twice.
//
// The three scanner fields are deliberately absent: internal/parse.Parse answers
// them today, so declaring them unbuilt would move a genuine wrong answer into
// the column that does not fail the run. So are `output` and `throw`, which the
// census does not count at all — internal/emitcase.Case.Layers says why, and
// compareEmitField will report either as a harness gap if that ever changes.
var emitBlockers = map[string]string{
	emitcase.FieldPath:           "no path selector (picomatch.js:312-316)",
	emitcase.FieldFastpathOutput: "no fastpaths pass (parse.js:1330)",
	emitcase.FieldSource:         "no compileRe (picomatch.js:273)",
	emitcase.FieldFlags:          "no toRegex (picomatch.js:343)",
}

// emitStratum is one stratum's count-matched-pct triple.
//
// The strata are counted and reported, never used as filters. The whole value of
// this gate on day one is that it says which layer a score came from: a headline
// percentage that mixes a scanner the port has with a compileRe it does not would
// describe neither.
type emitStratum struct {
	Fields  int     `json:"fields"`
	Matched int     `json:"matched"`
	Percent float64 `json:"percent"`
}

// emitReport is the emitter gate's own report. It is deliberately neither the
// conformance `report` nor `tokenReport`: the layer stratification below is the
// point of this harness, and reusing a shape without it drops the number that
// says what a green score is worth.
type emitReport struct {
	Fixture string `json:"fixture"`
	Cases   int    `json:"cases"`

	// Scoring is FIELD-level, not case-level. A case-level score would force a
	// weighting judgment — is a record carrying a fastpath output worth more
	// than one without? — where the field denominator is mechanical: every
	// recorded comparable field counts once, and each layer contributes exactly
	// in proportion to how much of it was recorded. The census lives in
	// internal/emitcase.Case.Layers, next to the JSON tags.
	Fields  int     `json:"fields"`
	Matched int     `json:"matched"`
	Percent float64 `json:"percent"`

	// Unbuilt and Wrong split the failures into the two kinds that mean
	// completely different things while the emitter is being written.
	//
	// Unbuilt is a field the port has no entry point for — no fastpaths pass, no
	// compileRe, or options that cannot reach internal/parse.Parse at all. It is
	// a failure, never a skip: it counts in the denominator exactly as
	// ErrNotImplemented does in the conformance harness, so an absent emitter
	// cannot shrink the denominator instead of counting against it.
	//
	// Wrong is a field the port answered and got a different value for. That is
	// a bug in a layer that already exists, and unlike Unbuilt it is not supposed
	// to be non-zero at any point. Watch this number, not the percentage.
	Unbuilt int `json:"unbuilt"`
	Wrong   int `json:"wrong"`

	// ByLayer and ByOptions are the two stratifications. ByLayer says which of
	// the four recorded layers a score came from; ByOptions says how much of it
	// is default-options work that `make tokens` already proved.
	ByLayer   map[string]*emitStratum `json:"byLayer"`
	ByOptions map[string]*emitStratum `json:"byOptions"`

	// UnbuiltByBlocker counts unbuilt fields against what is blocking them, in
	// the "<field> (<blocker>)" form, so the report says what to build next
	// rather than only how far there is to go.
	UnbuiltByBlocker map[string]int `json:"unbuiltByBlocker"`

	// WrongFields lists every field in Wrong, UNCAPPED. This is the list that
	// must stay empty, so truncating it would hide the thing it exists to
	// surface.
	WrongFields []string `json:"wrongFields"`
}

// Option strata names.
const (
	emitDefaultOptions    = "defaultOptions"
	emitNonDefaultOptions = "nonDefaultOptions"
)

// emitAnsweredOptions are the upstream option keys internal/parse.Options can
// express, and therefore the keys a record may carry and still be attempted.
//
// It is not the list of keys internal/parse *mentions*: roughly forty `opts.`
// sites are transcribed but marked rather than written, and a record carrying one
// of those would parse cleanly under the wrong configuration and score a pass
// wherever the two happen to agree. A key joins this map on the day its branch
// lands, which is the same day it earns a field on internal/parse.Options — the
// two lists are kept in step by emitParseOptions failing to compile otherwise.
var emitAnsweredOptions = map[string]bool{
	"windows":       true, // constants.globChars, parse.js:377
	"bash":          true, // parse.js:401, :675, :1156, :1248
	"strictSlashes": true, // parse.js:1193, :1304
	"dot":           true, // parse.js:396, :399, :1041, :1270
	"noextglob":     true, // parse.js:1023, :1054, :1072, :1096, :1140
	"noext":         true, // parse.js:408, merged over noextglob
	"posix":         true, // parse.js:719 (!== false), :751 (=== true)
	"regex":         true, // parse.js:1077 (=== false), :1257 (=== true)
}

// emitParseOptions converts a record's recorded options into the ones
// internal/parse takes, or names the first key it cannot express.
//
// Returning the key rather than a bool is what lets the report say `opts.dot`
// instead of "non-default": the blocker string is the build order, and "some
// option" would rank nothing. SetKeys is sorted, so the key blamed for a record
// carrying several is stable between runs.
func emitParseOptions(o *emitcase.Options) (parse.Options, string) {
	for _, k := range o.SetKeys() {
		if !emitAnsweredOptions[k] {
			return parse.Options{}, k
		}
	}
	// Read through the pointer: `{"windows":false}` is a set key with the default
	// value, and treating its presence as truth would parse it for the wrong
	// platform. It is set-ness that decides attemptability and the value that
	// decides the table.
	//
	// Posix, Regex and NoExt are the exception that proves the rule: each is read
	// twice upstream under two different tests, so unset is a third state and the
	// pointer is carried through rather than collapsed.
	return parse.Options{
		Windows:       o.Windows != nil && *o.Windows,
		Bash:          o.Bash != nil && *o.Bash,
		StrictSlashes: o.StrictSlashes != nil && *o.StrictSlashes,
		Dot:           o.Dot != nil && *o.Dot,
		NoExtglob:     o.NoExtglob != nil && *o.NoExtglob,
		NoExt:         o.NoExt,
		Posix:         o.Posix,
		Regex:         o.Regex,
	}, ""
}

func TestEmitParity(t *testing.T) {
	cases, err := emitcase.Load(emitPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Skip("emitter fixtures not generated; run `make emit-fixture`")
		}
		t.Fatalf("load emitter fixtures: %v\n\nRun `make emit-fixture` to generate them.", err)
	}
	if len(cases) == 0 {
		t.Fatalf("emitter fixture file is empty: %s", emitPath)
	}

	rep := replayEmitSet(cases, emitPath)
	writeEmitReport(t, &rep)
	logEmitReport(t, &rep)

	// A wrong answer is not progress-shaped: it means a layer that exists
	// disagrees with the recording, and no amount of further building fixes it.
	// Unlike the percentage floor this is not opt-in, because there is no stage
	// of the port at which a nonzero value here is acceptable.
	if rep.Wrong > 0 {
		for _, w := range rep.WrongFields {
			t.Errorf("  %s", w)
		}
		t.Fatalf("%d recorded field(s) disagreed with the port — see wrongFields in %s", rep.Wrong, emitReportPath)
	}

	if min := floorFor(t, "PICOMATCH_EMIT_MIN"); rep.Percent < min {
		t.Fatalf("emitter parity %.2f%% is below the required %.2f%%", rep.Percent, min)
	}
}

// replayEmitSet replays one fixture file and returns its report.
func replayEmitSet(cases []emitcase.Case, path string) emitReport {
	rep := emitReport{
		Fixture:          path,
		Cases:            len(cases),
		ByLayer:          map[string]*emitStratum{},
		ByOptions:        map[string]*emitStratum{},
		UnbuiltByBlocker: map[string]int{},
	}

	for i := range cases {
		c := &cases[i]

		// The scanner is parsed once per case rather than once per field, and
		// only when internal/parse can express the record's options.
		//
		// The stratum and the attempt are two different questions and are asked
		// separately: a `{"windows":true}` record is attempted, and still counts
		// under nonDefaultOptions, because that stratum exists to say how much of
		// the score is work `make tokens` had already proved. Folding the two
		// would move each newly threaded key's records into the default column and
		// quietly inflate the one number that is supposed to stay honest.
		var st *parse.State
		var perr error
		optStratum := emitNonDefaultOptions
		if c.Options.IsDefault() {
			optStratum = emitDefaultOptions
		}
		if popts, unexpressible := emitParseOptions(&c.Options); unexpressible == "" {
			st, perr = parse.Parse(c.Pattern, popts)
		}

		for _, field := range c.Layers() {
			layer := emitStratumFor(rep.ByLayer, emitcase.LayerOf(field))
			opts := emitStratumFor(rep.ByOptions, optStratum)

			rep.Fields++
			layer.Fields++
			opts.Fields++

			detail, blocker := compareEmitField(c, field, st, perr)
			switch {
			case blocker != "":
				rep.Unbuilt++
				rep.UnbuiltByBlocker[fmt.Sprintf("%s (%s)", field, blocker)]++
			case detail != "":
				rep.Wrong++
				rep.WrongFields = append(rep.WrongFields,
					fmt.Sprintf("%q %s: %s", c.Pattern, c.Options.Key(), detail))
			default:
				rep.Matched++
				layer.Matched++
				opts.Matched++
			}
		}
	}

	if rep.Fields > 0 {
		rep.Percent = 100 * float64(rep.Matched) / float64(rep.Fields)
	}
	for _, s := range rep.ByLayer {
		s.percent()
	}
	for _, s := range rep.ByOptions {
		s.percent()
	}
	return rep
}

func (s *emitStratum) percent() {
	if s.Fields > 0 {
		s.Percent = 100 * float64(s.Matched) / float64(s.Fields)
	}
}

func emitStratumFor(m map[string]*emitStratum, key string) *emitStratum {
	s := m[key]
	if s == nil {
		s = &emitStratum{}
		m[key] = s
	}
	return s
}

// compareEmitField replays one recorded field of one case.
//
// It returns "" and "" on a match; a non-empty detail for a disagreement, which
// is fatal; and a non-empty blocker when the port has no entry point for the
// field, which is an expected failure that shrinks as layers land. Exactly one of
// the two is ever non-empty.
//
// A field the harness neither compares nor declares unbuilt returns a detail, not
// a blocker. That direction is the safety property: a recorded field silently
// filed as unbuilt would leave the gate reporting a percentage over an assertion
// nobody makes.
func compareEmitField(c *emitcase.Case, field string, st *parse.State, perr error) (detail, blocker string) {
	switch field {
	case emitcase.FieldScannerOutput, emitcase.FieldNegated, emitcase.FieldScannerThrow:
		// Attemptable only under options internal/parse can express. A record
		// carrying a key the port cannot pass has no callable entry point and must
		// be unbuilt — never wrong. Without this the gate would manufacture a
		// thousand false disagreements on its first run and the Wrong column would
		// stop meaning anything.
		//
		// The converse is the whole point of threading a key through: once
		// `windows` is expressible, a windows record that disagrees is Wrong, and
		// the gate fails on it. Leaving a threaded key out of emitAnsweredOptions
		// would keep its records in the column nobody reads twice.
		if _, unexpressible := emitParseOptions(&c.Options); unexpressible != "" {
			return "", "opts." + unexpressible
		}
		return compareScannerField(c, field, st, perr)
	}

	if b, ok := emitBlockers[field]; ok {
		return "", b
	}
	return fmt.Sprintf("%s: recorded, but the harness neither compares it nor declares it unbuilt", field), ""
}

// compareScannerField compares one field the port's scanner does answer.
func compareScannerField(c *emitcase.Case, field string, st *parse.State, perr error) (detail, blocker string) {
	// A construct the scanner declined is unbuilt, exactly as in the token gate:
	// the two columns mean different things and only Wrong fails the run.
	if unbuilt, ok := errors.AsType[*parse.UnsupportedError](perr); ok {
		return "", fmt.Sprintf("%s (%s)", unbuilt.Construct, unbuilt.Site)
	}

	if field == emitcase.FieldScannerThrow {
		return compareScannerThrow(c.ScannerThrow, perr), ""
	}

	// The recording says the scanner returned; the port raised instead.
	if perr != nil {
		return "parse: " + perr.Error(), ""
	}
	if st == nil {
		return "parse returned no state and no error", ""
	}

	switch field {
	case emitcase.FieldScannerOutput:
		if c.ScannerOutput == nil {
			return "scannerOutput: scored, but the record does not carry one", ""
		}
		if *c.ScannerOutput != st.Output {
			return fmt.Sprintf("scannerOutput: want %q, got %q", *c.ScannerOutput, st.Output), ""
		}
	case emitcase.FieldNegated:
		if c.Negated == nil {
			return "negated: scored, but the record does not carry one", ""
		}
		if *c.Negated != st.Negated {
			return fmt.Sprintf("negated: want %v, got %v", *c.Negated, st.Negated), ""
		}
	}
	return "", ""
}

// compareScannerThrow resolves a case the recording says the full scanner threw
// on.
//
// A recorded throw is only reproduced if the port raises the same JavaScript
// exception: same constructor name, same message. Accepting any error would score
// "Missing closing: )" as equivalent to "exceeds maximum allowed length", exactly
// as compareError refuses to in the conformance harness. Meeting a recorded throw
// with no error at all is Wrong, never Matched.
func compareScannerThrow(want *emitcase.Throw, err error) string {
	if want == nil {
		return "scannerThrow: scored, but the record does not carry one"
	}
	if err == nil {
		return fmt.Sprintf("scannerThrow: want %s %q, got no error", want.Name, want.Message)
	}
	name, message, ok := emitThrowOf(err)
	if !ok {
		return fmt.Sprintf("scannerThrow: want %s %q, got an error the port cannot state in upstream's terms: %v",
			want.Name, want.Message, err)
	}
	if name != want.Name || message != want.Message {
		return fmt.Sprintf("scannerThrow: want %s %q, got %s %q", want.Name, want.Message, name, message)
	}
	return ""
}

// emitThrowOf restates an internal/parse error in the JavaScript terms the
// recording uses, and reports ok=false for one it cannot.
//
// Only [parse.LengthError] maps today, and it maps exactly: its message is a
// transcription of parse.js:367. [parse.NonTerminatingError] deliberately has no
// upstream equivalent — DECISIONS.md §11 — and cannot appear here, because the
// recorder hangs on the same input and so never records it.
func emitThrowOf(err error) (name, message string, ok bool) {
	if le, is := errors.AsType[*parse.LengthError](err); is {
		return picomatch.SyntaxError, le.Error(), true
	}
	return "", "", false
}

func writeEmitReport(t *testing.T, rep *emitReport) {
	t.Helper()
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		t.Fatalf("encode emitter report: %v", err)
	}
	if err := os.WriteFile(emitReportPath, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write emitter report: %v", err)
	}
}

func logEmitReport(t *testing.T, rep *emitReport) {
	t.Helper()
	t.Logf("%s: cases=%d fields=%d matched=%d unbuilt=%d wrong=%d emitter=%.2f%%",
		rep.Fixture, rep.Cases, rep.Fields, rep.Matched, rep.Unbuilt, rep.Wrong, rep.Percent)

	logEmitStrata(t, "layer", rep.ByLayer,
		[]string{emitcase.LayerScanner, emitcase.LayerFastpath, emitcase.LayerCompile, emitcase.LayerPath})
	logEmitStrata(t, "options", rep.ByOptions,
		[]string{emitDefaultOptions, emitNonDefaultOptions})

	logEmitBlockers(t, rep)
	t.Logf("report written to %s", emitReportPath)
}

func logEmitStrata(t *testing.T, kind string, strata map[string]*emitStratum, order []string) {
	t.Helper()
	for _, name := range order {
		s := strata[name]
		if s == nil {
			continue
		}
		t.Logf("  %-8s %-18s %d of %d matched (%.2f%%)", kind, name, s.Matched, s.Fields, s.Percent)
	}
}

// logEmitBlockers prints what is blocking the most fields. It is the build
// order, measured: the top line is the layer or option worth threading next.
func logEmitBlockers(t *testing.T, rep *emitReport) {
	t.Helper()
	if len(rep.UnbuiltByBlocker) == 0 {
		return
	}
	keys := make([]string, 0, len(rep.UnbuiltByBlocker))
	for k := range rep.UnbuiltByBlocker {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if rep.UnbuiltByBlocker[keys[i]] != rep.UnbuiltByBlocker[keys[j]] {
			return rep.UnbuiltByBlocker[keys[i]] > rep.UnbuiltByBlocker[keys[j]]
		}
		return keys[i] < keys[j]
	})
	if len(keys) > 10 {
		keys = keys[:10]
	}
	for _, k := range keys {
		t.Logf("  blocked on %-52s %d", k, rep.UnbuiltByBlocker[k])
	}
}

// TestCompareEmitDetectsDifferences proves the comparison can fail, and on what.
//
// Until an emitter lands, every non-scanner field is unbuilt and TestEmitParity
// exercises almost none of the comparison — it would report the same percentage
// if every check in compareScannerField were `return "", ""`. This is what makes
// the comparison observable in the meantime.
//
// The expectation is built from the scanner's own output and then perturbed one
// field at a time. That is circular as a claim about picomatch and is not made as
// one — testdata/emit is the claim about picomatch. What it asserts is that the
// harness notices a difference at all, which is the failure mode that would
// otherwise read as a rising score.
func TestCompareEmitDetectsDifferences(t *testing.T) {
	const pattern = "a/b"

	st, err := parse.Parse(pattern, parse.Options{})
	if err != nil {
		t.Fatalf("Parse(%q): %v", pattern, err)
	}

	base := func() emitcase.Case {
		return emitcase.Case{
			Pattern:       pattern,
			ScannerOutput: ptr(st.Output),
			Negated:       ptr(st.Negated),
		}
	}

	for _, field := range []string{emitcase.FieldScannerOutput, emitcase.FieldNegated} {
		c := base()
		if detail, blocker := compareEmitField(&c, field, st, nil); detail != "" || blocker != "" {
			t.Fatalf("%s: the scanner's own output did not compare equal (detail %q, blocker %q)",
				field, detail, blocker)
		}
	}

	perturbations := map[string]func(*emitcase.Case){
		emitcase.FieldScannerOutput: func(c *emitcase.Case) { c.ScannerOutput = ptr(*c.ScannerOutput + "x") },
		emitcase.FieldNegated:       func(c *emitcase.Case) { c.Negated = ptr(!*c.Negated) },
	}
	for field, perturb := range perturbations {
		t.Run(field+" difference is detected", func(t *testing.T) {
			c := base()
			perturb(&c)
			detail, blocker := compareEmitField(&c, field, st, nil)
			if detail == "" {
				t.Fatalf("a %s difference compared equal", field)
			}
			if blocker != "" {
				t.Fatalf("a %s difference was reported as unbuilt: %s", field, blocker)
			}
		})
	}

	// The line TestEmitParity's scoring rests on. It counts an empty detail as a
	// match, so a recorded throw routed to "" would score every pattern the port
	// silently succeeds on as agreement with a throw.
	t.Run("a recorded throw met with no error is wrong", func(t *testing.T) {
		c := base()
		c.ScannerOutput, c.Negated = nil, nil
		c.ScannerThrow = &emitcase.Throw{
			Name:    picomatch.SyntaxError,
			Message: "Input length: 9, exceeds maximum allowed length: 8",
		}
		detail, blocker := compareEmitField(&c, emitcase.FieldScannerThrow, st, nil)
		if detail == "" {
			t.Fatal("a recorded throw met with no error compared equal, which the gate scores as a match")
		}
		if blocker != "" {
			t.Fatalf("a recorded throw met with no error was reported as unbuilt: %s", blocker)
		}
	})

	// A real throw, compared rather than accepted on sight. The input is over
	// maxLength, which cannot stop being an error: upstream throws on it too, so
	// unlike a choice of unbuilt construct this does not expire as the port grows.
	t.Run("a recorded throw is compared by name and message", func(t *testing.T) {
		overlong := strings.Repeat("a", 64*1024+1)
		_, perr := parse.Parse(overlong, parse.Options{})
		name, message, ok := emitThrowOf(perr)
		if !ok {
			t.Fatalf("the port's length error cannot be stated in upstream's terms: %v", perr)
		}

		c := emitcase.Case{Pattern: overlong, ScannerThrow: &emitcase.Throw{Name: name, Message: message}}
		if detail, blocker := compareEmitField(&c, emitcase.FieldScannerThrow, nil, perr); detail != "" || blocker != "" {
			t.Fatalf("the port's own throw did not compare equal (detail %q, blocker %q)", detail, blocker)
		}

		c.ScannerThrow = &emitcase.Throw{Name: name, Message: message + " (and more)"}
		if detail, _ := compareEmitField(&c, emitcase.FieldScannerThrow, nil, perr); detail == "" {
			t.Fatal("a throw message difference compared equal")
		}

		c.ScannerThrow = &emitcase.Throw{Name: picomatch.TypeError, Message: message}
		if detail, _ := compareEmitField(&c, emitcase.FieldScannerThrow, nil, perr); detail == "" {
			t.Fatal("a throw name difference compared equal")
		}
	})

	// Every field with no entry point must be unbuilt, and must never be a match.
	// A blocker that decayed into "" would silently count the layer as built.
	t.Run("fields with no entry point are unbuilt, not matched", func(t *testing.T) {
		for field := range emitBlockers {
			c := base()
			detail, blocker := compareEmitField(&c, field, st, nil)
			if blocker == "" {
				t.Errorf("%s: want an unbuilt blocker, got detail %q", field, detail)
			}
		}
	})

	// A recorded field the harness forgot about is a harness gap, and must fail
	// rather than be filed under an unbuilt layer.
	t.Run("an unrecognised field is a harness gap", func(t *testing.T) {
		c := base()
		detail, blocker := compareEmitField(&c, "somethingNew", st, nil)
		if detail == "" {
			t.Fatal("an unrecognised recorded field compared equal")
		}
		if blocker != "" {
			t.Fatalf("an unrecognised recorded field was filed as unbuilt: %s", blocker)
		}
	})

	// Non-default options make even a built field unbuilt, and blame the option
	// rather than the layer. This is the distinction that stops the gate
	// manufacturing a thousand false disagreements on its first run.
	t.Run("non-default options are unbuilt, blamed on the option", func(t *testing.T) {
		var opts emitcase.Options
		// nocase, and not one of the scanner keys: every key this case has used
		// so far has since been threaded, and the fix each time was to pick
		// another. nocase cannot be threaded — `grep -n "\.nocase"
		// tests/original/lib/*.js` finds one site, picomatch.js:343, in the
		// compile layer, so lib/parse.js never reads it and no branch of
		// internal/parse can ever earn a field for it.
		if err := json.Unmarshal([]byte(`{"nocase":true,"windows":true}`), &opts); err != nil {
			t.Fatalf("decode options: %v", err)
		}
		c := base()
		c.Options = opts
		detail, blocker := compareEmitField(&c, emitcase.FieldScannerOutput, st, nil)
		// Both keys are set and only one is expressible, so the blocker names the
		// one that is not — sorted order would otherwise report opts.windows and
		// claim a threaded key is what is missing.
		if blocker != "opts.nocase" {
			t.Fatalf("blocker = %q, want %q (detail %q)", blocker, "opts.nocase", detail)
		}
	})
}

// TestReplayEmitSetTallies covers the scoring loop itself, which the real
// fixture cannot exercise: with no emitter, every field it sees is either matched
// or unbuilt, so nothing at runtime would notice if the Wrong branch stopped
// counting. That branch is the one TestEmitParity fails on.
func TestReplayEmitSetTallies(t *testing.T) {
	const pattern = "a/b"

	st, err := parse.Parse(pattern, parse.Options{})
	if err != nil {
		t.Fatalf("Parse(%q): %v", pattern, err)
	}

	// opts.nocase, and deliberately not a scanner key: the stand-in has to be
	// one emitAnsweredOptions will never carry, or this case quietly inverts on
	// the day that key is threaded. nocase qualifies permanently — its only
	// reader in the whole of lib/ is picomatch.js:343, in the compile layer, so
	// lib/parse.js never sees it. The guard below still fires if that changes.
	var blocking emitcase.Options
	if err := json.Unmarshal([]byte(`{"nocase":true}`), &blocking); err != nil {
		t.Fatalf("decode options: %v", err)
	}
	if _, unexpressible := emitParseOptions(&blocking); unexpressible != "nocase" {
		t.Fatalf("opts.nocase is expressible now (%q); this case needs a key the port still cannot pass", unexpressible)
	}

	inline := emitcase.PathInline
	wrapped := "^(?:" + st.Output + ")$"
	source := "^(?:" + wrapped + ")$"

	// Three records: one the port answers correctly, one it answers wrongly, and
	// one it cannot attempt because the options never reach internal/parse.
	right := emitcase.Case{
		Pattern: pattern, Path: &inline, Output: &wrapped,
		ScannerOutput: ptr(st.Output), Negated: ptr(st.Negated),
		Source: &source, Flags: "",
	}
	wrong := right
	wrong.ScannerOutput = ptr(st.Output + "x")
	blocked := right
	blocked.Options = blocking

	rep := replayEmitSet([]emitcase.Case{right, wrong, blocked}, "synthetic")

	// Each record contributes path, scannerOutput, negated, source and flags.
	if rep.Fields != 15 {
		t.Fatalf("fields = %d, want 15", rep.Fields)
	}
	// right's scannerOutput and negated, plus wrong's negated — the perturbed
	// record still answers everything the port has an entry point for.
	if rep.Matched != 3 {
		t.Errorf("matched = %d, want 3", rep.Matched)
	}
	if rep.Wrong != 1 {
		t.Errorf("wrong = %d, want 1", rep.Wrong)
	}
	if rep.Unbuilt != 11 {
		t.Errorf("unbuilt = %d, want 11", rep.Unbuilt)
	}
	if len(rep.WrongFields) != rep.Wrong {
		t.Errorf("wrongFields holds %d of %d wrong fields; the list is uncapped on purpose",
			len(rep.WrongFields), rep.Wrong)
	}
	if rep.Matched+rep.Wrong+rep.Unbuilt != rep.Fields {
		t.Errorf("%d matched + %d wrong + %d unbuilt != %d fields — a field escaped scoring",
			rep.Matched, rep.Wrong, rep.Unbuilt, rep.Fields)
	}

	// The blocker label is the build order, so its form is part of the contract.
	if got := rep.UnbuiltByBlocker["scannerOutput (opts.nocase)"]; got != 1 {
		t.Errorf("unbuiltByBlocker[%q] = %d, want 1 (got %v)",
			"scannerOutput (opts.nocase)", got, rep.UnbuiltByBlocker)
	}

	if s := rep.ByOptions[emitDefaultOptions]; s == nil || s.Fields != 10 || s.Matched != 3 {
		t.Errorf("defaultOptions stratum = %+v, want 3 of 10", s)
	}
	if s := rep.ByOptions[emitNonDefaultOptions]; s == nil || s.Fields != 5 || s.Matched != 0 {
		t.Errorf("nonDefaultOptions stratum = %+v, want 0 of 5", s)
	}
	if s := rep.ByLayer[emitcase.LayerScanner]; s == nil || s.Fields != 6 || s.Matched != 3 {
		t.Errorf("scanner stratum = %+v, want 3 of 6", s)
	}
	if s := rep.ByLayer[emitcase.LayerCompile]; s == nil || s.Matched != 0 {
		t.Errorf("compile stratum = %+v, want 0 matched", s)
	}
}

// TestEmitAttemptabilityIsDeclared pins the blocker map's contents and its
// length, so a new "the port cannot do this yet" entry cannot be added without a
// stated reason. Same shape as TestMatcherExemptionsAreDeclared, and for the same
// reason: unbuilt is the column nobody reads twice, so what lands in it has to be
// a decision rather than a default.
func TestEmitAttemptabilityIsDeclared(t *testing.T) {
	want := []string{
		emitcase.FieldPath,
		emitcase.FieldFastpathOutput,
		emitcase.FieldSource,
		emitcase.FieldFlags,
	}
	for _, field := range want {
		if _, ok := emitBlockers[field]; !ok {
			t.Errorf("%q is no longer declared unbuilt", field)
		}
	}
	if len(emitBlockers) != len(want) {
		t.Errorf("blocker map has %d entries, want %d — a new unbuilt field needs a stated reason",
			len(emitBlockers), len(want))
	}

	// The fields the port CAN answer must stay out of the map. Adding one there
	// would move a genuine disagreement into the column that does not fail.
	for _, field := range []string{emitcase.FieldScannerOutput, emitcase.FieldNegated, emitcase.FieldScannerThrow} {
		if _, ok := emitBlockers[field]; ok {
			t.Errorf("%q has an entry point in internal/parse; declaring it unbuilt would hide a real disagreement", field)
		}
	}

	// So must the two the census does not count. A blocker for either would read
	// as "the port cannot do this yet" when the truth is that nothing scores it,
	// which is the more serious of the two states and the one that needs to keep
	// failing loudly through compareEmitField.
	for _, field := range []string{emitcase.FieldOutput, emitcase.FieldThrow} {
		if _, ok := emitBlockers[field]; ok {
			t.Errorf("%q is recorded but not counted; declaring it unbuilt would hide that nothing compares it", field)
		}
		if detail, blocker := compareEmitField(&emitcase.Case{}, field, nil, nil); detail == "" || blocker != "" {
			t.Errorf("%q: want a harness-gap detail, got detail %q blocker %q", field, detail, blocker)
		}
	}

	// Every declared blocker must name a field the fixture can actually carry,
	// and every field must belong to a layer — otherwise the census and the
	// blocker list drift apart without either failing.
	for field := range emitBlockers {
		if emitcase.LayerOf(field) == "" {
			t.Errorf("blocker declared for %q, which is not a recorded field", field)
		}
	}
}

// TestEmitFixtureShape guards the fixture itself, and runs whether or not an
// emitter exists.
//
// Without it the gate has a silent failure mode that looks like success: if
// tools/emit/generate.js ever wrote records with no comparable fields, or dropped
// the path selector, TestEmitParity would still report a percentage — over
// assertions that assert nothing.
func TestEmitFixtureShape(t *testing.T) {
	cases, err := emitcase.Load(emitPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Skip("emitter fixtures not generated; run `make emit-fixture`")
		}
		t.Fatalf("load emitter fixtures: %v", err)
	}

	s := emitcase.Summarise(cases)
	t.Logf("cases=%d patterns=%d optionSets=%d default=%d nonDefault=%d",
		s.Total, s.Patterns, s.OptionSets, s.DefaultOptions, s.NonDefaultOptions)
	t.Logf("comparableFields=%d byLayer=%v", s.ComparableFields, s.ByLayer)
	t.Logf("path=%v fastpath eligible=%d returnedOutput=%d threw=%d",
		s.ByPath, s.FastpathEligible, s.FastpathOutput, s.FastpathThrew)
	t.Logf("negated=%d makeReThrew=%d scannerThrew=%d", s.Negated, s.Threw, s.ScannerThrew)

	seen := map[string]bool{}
	for i := range cases {
		c := &cases[i]
		id := c.Pattern + "\x00" + c.Options.Key()

		// The empty pattern is a legitimate record, not a missing one: makeRe
		// rejects it at picomatch.js:306 and the recorded TypeError is the
		// answer. Asserting a non-empty pattern here would delete real evidence.
		if seen[id] {
			t.Fatalf("%q %s: recorded twice; the composite key is pattern + options",
				c.Pattern, c.Options.Key())
		}
		seen[id] = true

		if len(c.Layers()) == 0 {
			t.Fatalf("%q %s: no comparable fields recorded", c.Pattern, c.Options.Key())
		}

		// makeRe either threw or produced all four of its fields; a record with
		// some of both means the recorder caught a throw at the wrong layer.
		if c.Throw != nil {
			if c.Path != nil || c.Output != nil || c.Source != nil {
				t.Fatalf("%q %s: makeRe threw but path/output/source were also recorded",
					c.Pattern, c.Options.Key())
			}
		} else {
			if c.Path == nil || c.Output == nil {
				t.Fatalf("%q %s: makeRe did not throw but path or output is missing",
					c.Pattern, c.Options.Key())
			}
			switch *c.Path {
			case emitcase.PathNone, emitcase.PathTop, emitcase.PathInline:
			default:
				t.Fatalf("%q %s: unknown path %q", c.Pattern, c.Options.Key(), *c.Path)
			}
		}

		// The full scanner either returned output or threw, never both and never
		// neither: scannerOutput absent is what records the throw.
		if (c.ScannerOutput == nil) == (c.ScannerThrow == nil) {
			t.Fatalf("%q %s: exactly one of scannerOutput and scannerThrow must be recorded",
				c.Pattern, c.Options.Key())
		}
		if (c.ScannerOutput == nil) != (c.Negated == nil) {
			t.Fatalf("%q %s: negated is recorded iff scannerOutput is", c.Pattern, c.Options.Key())
		}

		// The three fastpath states must stay distinguishable: not eligible, and
		// eligible-but-falsy, are different recorded answers.
		if !c.FastpathEligible && (c.FastpathOutput != nil || c.FastpathThrow != nil) {
			t.Fatalf("%q %s: fastpaths produced a result for an ineligible pattern",
				c.Pattern, c.Options.Key())
		}
		if c.FastpathOutput != nil && c.FastpathThrow != nil {
			t.Fatalf("%q %s: fastpaths both returned and threw", c.Pattern, c.Options.Key())
		}

		// The trap that has already cost this repo once, as an assertion rather
		// than a footnote: the inline path wraps inside parse() (parse.js:653),
		// so its recorded output is ^(?:X)$ where the other two are bare. A
		// recorder that diffed .source instead would double-count that layer.
		if c.Path != nil && *c.Path == emitcase.PathInline && c.Output != nil {
			if !strings.HasPrefix(*c.Output, "^(?:") {
				t.Fatalf("%q %s: inline path recorded a bare output %q; utils.wrapOutput runs inside parse()",
					c.Pattern, c.Options.Key(), *c.Output)
			}
		}

		// Output against the layer the path names it came from.
		//
		// The gate does not score Output — emitcase's doc says it is the path
		// plus the layer outputs read as one answer — but "not scored" was
		// letting it be the one recorded field nothing at all constrained, so a
		// fabricated value survived the whole suite. On two of the three paths
		// the fixture already carries the ingredient, and agreement is exact:
		// 79/79 on top, 1750/1750 on none. Assert it, and the field stops being
		// forgeable wherever it is derivable.
		//
		// PathInline is deliberately absent. Its output is wrapOutput's, and the
		// bare parse(pattern, opts) it wraps is not recorded — scannerOutput is
		// the {fastpaths:false} run, which for 59 of the 199 inline records is a
		// different string. That non-derivability is exactly why Output is
		// recorded rather than computed, and asserting a false equality here
		// would be worse than asserting nothing.
		if c.Throw == nil && c.Path != nil && c.Output != nil {
			var want *string
			switch *c.Path {
			case emitcase.PathTop:
				want = c.FastpathOutput
			case emitcase.PathNone:
				want = c.ScannerOutput
			}
			if want != nil && *want != *c.Output {
				t.Fatalf("%q %s: path %q but output %q does not match that layer's %q",
					c.Pattern, c.Options.Key(), *c.Path, *c.Output, *want)
			}
		}
	}

	// A fixture with no full-scanner records, or none the port can attempt,
	// would gate nothing at all.
	if s.ByPath[emitcase.PathNone] == 0 {
		t.Error("no full-scanner records; the path selector is wrong")
	}
	if s.DefaultOptions == 0 {
		t.Error("no default-options records; nothing in the set is attemptable today")
	}

	// DECISIONS.md §16: function-valued options are excluded by a mechanical
	// rule and counted in summary.json, never silently dropped. Nothing here may
	// carry one.
	for i := range cases {
		if cases[i].Options.ExpandRange != nil {
			t.Fatalf("%q: carries a function-valued option; DECISIONS.md §16 excludes those pairs",
				cases[i].Pattern)
		}
	}

	// The census leaves `throw` uncounted on the grounds that the layer below
	// already carries it, except on the empty pattern where makeRe's own guard
	// (picomatch.js:306) fires before any parser runs. That is an argument about
	// this data, so it is checked against this data: an upstream bump that
	// introduced a makeRe-only throw would leave it uncompared, and this is what
	// forces the decision to be made again rather than inherited.
	for i := range cases {
		c := &cases[i]
		if c.Throw == nil {
			continue
		}
		if c.Pattern == "" {
			continue
		}
		if c.ScannerThrow == nil || *c.ScannerThrow != *c.Throw {
			t.Errorf("%q %s: makeRe threw %s %q, which no counted field reproduces — see internal/emitcase.Case.Layers",
				c.Pattern, c.Options.Key(), c.Throw.Name, c.Throw.Message)
		}
	}
}
