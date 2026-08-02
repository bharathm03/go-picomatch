package emitcase

import (
	"encoding/json"
	"strings"
	"testing"
)

// decodeOptions decodes one recorded options object, or fails the test.
func decodeOptions(t *testing.T, s string) Options {
	t.Helper()
	var o Options
	if err := json.Unmarshal([]byte(s), &o); err != nil {
		t.Fatalf("decode %s: %v", s, err)
	}
	return o
}

// TestUnknownOptionKeyIsAnError pins the one property that fails silently
// without a test.
//
// The recorder throws on a key that is in neither the emit allow-list nor the
// declared matcher-only set, so the two ends agree about the option surface. If
// this end quietly ignored an unknown key instead, a new upstream option would
// vanish from the projection and the gate's denominator would shrink — which is
// indistinguishable from progress.
func TestUnknownOptionKeyIsAnError(t *testing.T) {
	var o Options
	err := json.Unmarshal([]byte(`{"windows":true,"onMatch":true}`), &o)
	if err == nil {
		t.Fatal("an unknown option key decoded cleanly")
	}
	if !strings.Contains(err.Error(), "onMatch") {
		t.Errorf("error does not name the offending key: %v", err)
	}
}

// TestUnknownOptionKeyIsNamedDeterministically checks that two unknown keys
// always produce the same message, so a fixture bug does not report differently
// between runs.
func TestUnknownOptionKeyIsNamedDeterministically(t *testing.T) {
	const in = `{"zebra":true,"onMatch":true,"windows":true}`
	var first string
	for i := 0; i < 20; i++ {
		var o Options
		err := json.Unmarshal([]byte(in), &o)
		if err == nil {
			t.Fatal("unknown option keys decoded cleanly")
		}
		if i == 0 {
			first = err.Error()
			continue
		}
		if err.Error() != first {
			t.Fatalf("message is not stable: %q then %q", first, err.Error())
		}
	}
	if !strings.Contains(first, "onMatch") {
		t.Errorf("want the alphabetically first unknown key named, got %q", first)
	}
}

// TestFalseIsNotAbsent is the reason every option field is a pointer.
//
// parse.js merges `noext` over `noextglob`, so `{noextglob: true, noext: false}`
// turns extglobs back on where omitting `noext` leaves them off. A plain bool
// records both as false, and the gate would then score a record whose options it
// has misread as one it can attempt.
func TestFalseIsNotAbsent(t *testing.T) {
	set := decodeOptions(t, `{"noext":false}`)
	absent := decodeOptions(t, `{}`)

	if set.NoExt == nil {
		t.Fatal("noext:false decoded as absent")
	}
	if *set.NoExt {
		t.Fatal("noext:false decoded as true")
	}
	if absent.NoExt != nil {
		t.Fatal("an absent noext decoded as present")
	}

	if set.IsDefault() {
		t.Error("a record carrying noext:false reports default options")
	}
	if !absent.IsDefault() {
		t.Error("a record carrying no options reports non-default options")
	}
	if got := set.SetKeys(); len(got) != 1 || got[0] != "noext" {
		t.Errorf("SetKeys() = %v, want [noext]", got)
	}
}

// TestExtglobCapDistinguishesFalseFromZero pins the other collapse the schema
// forbids: upstream's maxExtglobRecursion takes a number or the literal false,
// and `false` disables the cap where `0` sets it to zero.
func TestExtglobCapDistinguishesFalseFromZero(t *testing.T) {
	disabled := decodeOptions(t, `{"maxExtglobRecursion":false}`)
	zero := decodeOptions(t, `{"maxExtglobRecursion":0}`)
	two := decodeOptions(t, `{"maxExtglobRecursion":2}`)

	if disabled.MaxExtglobRecursion == nil || zero.MaxExtglobRecursion == nil {
		t.Fatal("maxExtglobRecursion decoded as absent")
	}
	if !disabled.MaxExtglobRecursion.Disabled {
		t.Error("false did not decode as disabled")
	}
	if disabled.MaxExtglobRecursion.N != nil {
		t.Error("false decoded with a numeric cap as well")
	}
	if zero.MaxExtglobRecursion.Disabled {
		t.Error("0 decoded as disabled")
	}
	if zero.MaxExtglobRecursion.N == nil || *zero.MaxExtglobRecursion.N != 0 {
		t.Errorf("0 decoded as %v", zero.MaxExtglobRecursion.N)
	}
	if two.MaxExtglobRecursion.N == nil || *two.MaxExtglobRecursion.N != 2 {
		t.Errorf("2 decoded as %v", two.MaxExtglobRecursion.N)
	}

	// `true` is not a shape upstream produces. It must fail rather than decode
	// to something plausible.
	var o Options
	if err := json.Unmarshal([]byte(`{"maxExtglobRecursion":true}`), &o); err == nil {
		t.Error("maxExtglobRecursion:true decoded cleanly")
	}
}

// TestSetKeysIsSortedAndStable matters because SetKeys()[0] is the blocker label
// the gate reports for a field it cannot attempt. An unstable order would
// reshuffle the measured build order between runs.
func TestSetKeysIsSortedAndStable(t *testing.T) {
	o := decodeOptions(t, `{"windows":true,"dot":true,"bash":true,"strictSlashes":true}`)
	want := []string{"bash", "dot", "strictSlashes", "windows"}

	for i := 0; i < 20; i++ {
		got := o.SetKeys()
		if len(got) != len(want) {
			t.Fatalf("SetKeys() = %v, want %v", got, want)
		}
		for j := range want {
			if got[j] != want[j] {
				t.Fatalf("SetKeys() = %v, want %v", got, want)
			}
		}
	}
}

// TestOptionSetKeyIncludesValues guards the option-set count in [Summarise]:
// two records with the same keys but different values are different option sets.
func TestOptionSetKeyIncludesValues(t *testing.T) {
	a := decodeOptions(t, `{"maxLength":1}`)
	b := decodeOptions(t, `{"maxLength":9}`)
	if a.Key() == b.Key() {
		t.Fatalf("distinct option sets share a key: %q", a.Key())
	}
	def := decodeOptions(t, `{}`)
	if got := def.Key(); got != "{}" {
		t.Errorf("default options key = %q, want %q", got, "{}")
	}
}

// TestReadSkipsBlankLinesAndReportsLineNumbers covers the loader's own contract.
// The line number is 1-indexed and counts blank lines, so a report points at the
// line an editor shows.
func TestReadSkipsBlankLinesAndReportsLineNumbers(t *testing.T) {
	const good = `{"pattern":"foo","options":{},"fastpathEligible":false,"scannerOutput":"foo","negated":false,"path":"inline","output":"^(?:foo)$","source":"^(?:^(?:foo)$)$","flags":""}`

	cases, err := Read(strings.NewReader("\n" + good + "\n\n" + good + "\n"))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(cases) != 2 {
		t.Fatalf("read %d cases, want 2", len(cases))
	}

	_, err = Read(strings.NewReader(good + "\n\nnot json\n"))
	if err == nil {
		t.Fatal("malformed input read cleanly")
	}
	if !strings.Contains(err.Error(), "line 3") {
		t.Errorf("error does not name line 3: %v", err)
	}
}

// TestReadHandlesRecordsOverTheDefaultScannerLimit guards the sc.Buffer call.
// Without it bufio.Scanner stops at 64 KiB and the record becomes a parse error
// — which would look like a corrupt fixture rather than a loader limit.
func TestReadHandlesRecordsOverTheDefaultScannerLimit(t *testing.T) {
	big := strings.Repeat("a", 80*1024)
	line := `{"pattern":"` + big + `","options":{},"fastpathEligible":false,"scannerOutput":"` + big + `","negated":false,"path":"none","output":"` + big + `","source":"^(?:` + big + `)$","flags":""}`

	cases, err := Read(strings.NewReader(line + "\n"))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(cases) != 1 {
		t.Fatalf("read %d cases, want 1", len(cases))
	}
	if len(cases[0].Pattern) != len(big) {
		t.Fatalf("pattern truncated to %d bytes", len(cases[0].Pattern))
	}
}

// TestLayersCountsRecordedFieldsOnly pins the gate's denominator. A field the
// record does not carry must not be counted; an eligible-but-falsy fastpath must
// be, because "fastpaths declined this pattern" is itself a recorded answer; and
// `output` and `throw` must not be, for the reasons [Case.Layers] states.
func TestLayersCountsRecordedFieldsOnly(t *testing.T) {
	tests := []struct {
		name string
		line string
		want []string
	}{
		{
			"a scanner record with compileRe output",
			`{"pattern":"foo","options":{},"fastpathEligible":false,"scannerOutput":"foo","negated":false,"path":"inline","output":"^(?:foo)$","source":"^(?:^(?:foo)$)$","flags":""}`,
			[]string{"path", "scannerOutput", "negated", "source", "flags"},
		},
		{
			"eligible and falsy still contributes a fastpath field",
			`{"pattern":"*","options":{},"fastpathEligible":true,"scannerOutput":"x","negated":false,"path":"none","output":"x","source":"^(?:x)$","flags":""}`,
			[]string{"path", "scannerOutput", "negated", "fastpathOutput", "source", "flags"},
		},
		{
			"a makeRe throw leaves only the layer that reproduces it",
			`{"pattern":"[a-","options":{"strictBrackets":true},"fastpathEligible":false,"scannerThrow":{"name":"SyntaxError","message":"Missing closing: \"]\""},"throw":{"name":"SyntaxError","message":"Missing closing: \"]\""}}`,
			[]string{"scannerThrow"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cases, err := Read(strings.NewReader(tt.line + "\n"))
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			got := cases[0].Layers()
			if len(got) != len(tt.want) {
				t.Fatalf("Layers() = %v, want %v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("Layers() = %v, want %v", got, tt.want)
				}
			}
			for _, f := range got {
				if LayerOf(f) == "" {
					t.Errorf("field %q belongs to no layer", f)
				}
			}
		})
	}

	if LayerOf("somethingNew") != "" {
		t.Error("an unrecognised field name was assigned a layer")
	}
}

// TestThrowMessagesAreNotNormalised checks the loader hands back a message
// byte-for-byte. syntaxError at parse.js:45 emits two literal backslashes, and
// JSON-escaping doubles them again on disk; a loader that unescaped once too
// often would make every recorded bracket throw compare unequal.
func TestThrowMessagesAreNotNormalised(t *testing.T) {
	const line = `{"pattern":"[a-","options":{"strictBrackets":true},"fastpathEligible":false,` +
		`"scannerThrow":{"name":"SyntaxError","message":"Missing closing: \"]\" - use \"\\\\]\" to match literal characters"},` +
		`"throw":{"name":"SyntaxError","message":"Missing closing: \"]\" - use \"\\\\]\" to match literal characters"}}`

	cases, err := Read(strings.NewReader(line + "\n"))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	want := `Missing closing: "]" - use "\\]" to match literal characters`
	if got := cases[0].ScannerThrow.Message; got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
}

// TestSummariseCountsWhatTheRecorderCounts is the Go-side check on the figures
// testdata/emit/summary.json states from the other end.
func TestSummariseCountsWhatTheRecorderCounts(t *testing.T) {
	lines := strings.Join([]string{
		`{"pattern":"foo","options":{},"fastpathEligible":false,"scannerOutput":"foo","negated":false,"path":"inline","output":"^(?:foo)$","source":"^(?:^(?:foo)$)$","flags":""}`,
		`{"pattern":"foo","options":{"windows":true},"fastpathEligible":false,"scannerOutput":"foo","negated":false,"path":"inline","output":"^(?:foo)$","source":"^(?:^(?:foo)$)$","flags":""}`,
		`{"pattern":"!a","options":{},"fastpathEligible":false,"scannerOutput":"a","negated":true,"path":"none","output":"a","source":"^(?!^(?:a)$).*$","flags":""}`,
		`{"pattern":"*.js","options":{},"fastpathEligible":true,"fastpathOutput":"x","scannerOutput":"y","negated":false,"path":"top","output":"x","source":"^(?:x)$","flags":""}`,
	}, "\n")

	cases, err := Read(strings.NewReader(lines + "\n"))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	s := Summarise(cases)
	for _, check := range []struct {
		name string
		got  int
		want int
	}{
		{"total", s.Total, 4},
		{"patterns", s.Patterns, 3},
		{"optionSets", s.OptionSets, 2},
		{"defaultOptions", s.DefaultOptions, 3},
		{"nonDefaultOptions", s.NonDefaultOptions, 1},
		{"negated", s.Negated, 1},
		{"fastpathEligible", s.FastpathEligible, 1},
		{"fastpathOutput", s.FastpathOutput, 1},
		{"threw", s.Threw, 0},
		{"comparableFields", s.ComparableFields, 5 + 5 + 5 + 6},
		{"path none", s.ByPath["none"], 1},
		{"path inline", s.ByPath["inline"], 2},
		{"path top", s.ByPath["top"], 1},
		{"windows pairs", s.ByOptionKey["windows"], 1},
		{"scanner fields", s.ByLayer[LayerScanner], 8},
		{"fastpath fields", s.ByLayer[LayerFastpath], 1},
	} {
		if check.got != check.want {
			t.Errorf("%s = %d, want %d", check.name, check.got, check.want)
		}
	}
}
