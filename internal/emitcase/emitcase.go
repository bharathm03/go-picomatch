// Package emitcase loads the emitter and compiler output recorded from upstream
// picomatch.
//
// Each case is one (pattern, emit-relevant-options) pair and everything upstream
// produced for it below the matcher: what parse.fastpaths returned, what the full
// scanner's parse() returned, which of the three parsers makeRe actually used,
// the output makeRe handed compileRe, and the RegExp source and flags that came
// out the other side. Nothing here depends on the port, so the fixtures can be
// inspected and counted before an emitter exists.
//
// Fixtures are produced by tools/emit/generate.js. The recorded shape is kept
// deliberately separate from [github.com/bharathm03/go-picomatch/internal/parse]'s
// own types and from the root package's [Options] rather than shared: if the
// port's emitter drifts from what upstream records, the conversion is where that
// becomes visible. A shared struct would make a drift compile cleanly and
// disappear.
//
// # Absent is not zero, in both directions
//
// Presence is load-bearing throughout. `scannerOutput` absent means the scanner
// threw, which is a different claim from an empty output; `noext: false` absent
// means upstream never read the key, while `noext: false` present overwrites
// `noextglob: true` at parse.js's option merge; `maxExtglobRecursion: false`
// disables the cap where `0` sets it to zero. Every such field is a pointer, and
// [ExtglobCap] exists so that the last pair cannot collapse either.
package emitcase

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// Path values: which of picomatch's three parsers makeRe really used for this
// (pattern, options) pair. Recorded, never derived — eligibility is not use, and
// picomatch.js:316 falls through to the scanner whenever fastpaths returns a
// falsy value.
const (
	// PathNone means the full scanner ran and its output was compiled.
	PathNone = "none"
	// PathTop means parse.fastpaths() returned output and parse() never ran
	// (picomatch.js:312-316).
	PathTop = "top"
	// PathInline means parse() returned from the fast path at parse.js:606.
	// Its output is ALREADY wrapped as ^(?:X)$ by utils.wrapOutput inside
	// parse() (parse.js:653), where the scanner's and fastpaths' are bare.
	PathInline = "inline"
)

// Layer names. Every recorded comparable field belongs to exactly one, and the
// gate stratifies on them rather than filtering by them: each layer needs a
// different piece of the port before it can be attempted at all, so a single
// percentage over all four says very little on its own.
const (
	// LayerPath is makeRe's own layer — which parser it chose, the output it
	// handed compileRe, and the throw that escapes when neither parser returns.
	LayerPath = "path"
	// LayerScanner is parse(input, {fastpaths: false}) — the layer
	// internal/parse.State already targets.
	LayerScanner = "scanner"
	// LayerFastpath is parse.fastpaths(), which has no Go entry point at all.
	LayerFastpath = "fastpath"
	// LayerCompile is picomatch.compileRe plus toRegex — the ^(?:X)$ wrap, the
	// negation wrap, and the flags.
	LayerCompile = "compile"
)

// Throw is a recorded JavaScript exception: the constructor name and the message
// verbatim. Messages are never normalised — syntaxError at parse.js:45 emits two
// literal backslashes, and that is part of the recorded value.
type Throw struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

// ExtglobCap is upstream's `maxExtglobRecursion`, which accepts a number or the
// literal `false`.
//
// The two must not collapse onto one Go value: `false` disables the cap and `0`
// sets it to zero, which are opposite instructions. N is nil exactly when
// Disabled is true.
type ExtglobCap struct {
	N        *int
	Disabled bool
}

// UnmarshalJSON decodes upstream's number-or-false, and rejects anything else so
// a new shape fails the load rather than decoding to a plausible default.
func (c *ExtglobCap) UnmarshalJSON(data []byte) error {
	var n int
	if err := json.Unmarshal(data, &n); err == nil {
		c.N, c.Disabled = &n, false
		return nil
	}
	var b bool
	if err := json.Unmarshal(data, &b); err == nil && !b {
		c.N, c.Disabled = nil, true
		return nil
	}
	return fmt.Errorf("maxExtglobRecursion: want a number or false, got %s", data)
}

// Options is the projection of a recorded options object onto the keys upstream
// reads below the matcher: the 21 emitter keys, the 3 that only reach the error
// surface (maxLength, strictBrackets, debug), and the 2 that only reach
// compileRe/toRegex (nocase, flags).
//
// Every field is a pointer because absent and false are different states. The
// clearest case is `noext`: parse.js merges it over `noextglob`, so passing
// `noext: false` alongside `noextglob: true` turns extglobs back on, while
// omitting `noext` leaves them off. A plain bool records both as false.
//
// The set is closed on purpose — see [Options.UnmarshalJSON].
type Options struct {
	Bash                *bool       `json:"bash"`
	Capture             *bool       `json:"capture"`
	Contains            *bool       `json:"contains"`
	Debug               *bool       `json:"debug"`
	Dot                 *bool       `json:"dot"`
	ExpandRange         *RawValue   `json:"expandRange"`
	Fastpaths           *bool       `json:"fastpaths"`
	Flags               *string     `json:"flags"`
	KeepQuotes          *bool       `json:"keepQuotes"`
	LiteralBrackets     *bool       `json:"literalBrackets"`
	MaxExtglobRecursion *ExtglobCap `json:"maxExtglobRecursion"`
	MaxLength           *int        `json:"maxLength"`
	NoBrace             *bool       `json:"nobrace"`
	NoBracket           *bool       `json:"nobracket"`
	NoCase              *bool       `json:"nocase"`
	NoExt               *bool       `json:"noext"`
	NoExtglob           *bool       `json:"noextglob"`
	NoGlobstar          *bool       `json:"noglobstar"`
	NoNegate            *bool       `json:"nonegate"`
	Posix               *bool       `json:"posix"`
	Prepend             *string     `json:"prepend"`
	Regex               *bool       `json:"regex"`
	StrictBrackets      *bool       `json:"strictBrackets"`
	StrictSlashes       *bool       `json:"strictSlashes"`
	Unescape            *bool       `json:"unescape"`
	Windows             *bool       `json:"windows"`

	// raw is the record's own options object, kept so that two pairs sharing a
	// key set but not its values still count as distinct option sets. It is the
	// generator's canonical form — inner keys sorted — so it is stable enough to
	// use as a map key.
	raw string
}

// RawValue holds a recorded option value the loader does not interpret.
//
// It exists for `expandRange` alone, which upstream reads at parse.js:23 as a
// caller-supplied function. Every pair carrying one is dropped by the recorder
// (DECISIONS.md §16) because a function's behaviour is not in the recording, so
// this field is never populated in practice — a non-nil value means that
// exclusion has regressed, which is exactly why the key stays in the allow-list
// rather than being deleted from it.
type RawValue = json.RawMessage

// optionKeys is the closed allow-list, paired with the field each key decodes
// into. It is the single definition of "an emit-relevant option": UnmarshalJSON
// rejects anything absent from it, and IsDefault and SetKeys read it rather than
// re-listing the fields.
func (o *Options) optionKeys() []optionKey {
	return []optionKey{
		{"bash", func() bool { return o.Bash != nil }, func(r json.RawMessage) error { return decodeBool("bash", r, &o.Bash) }},
		{"capture", func() bool { return o.Capture != nil }, func(r json.RawMessage) error { return decodeBool("capture", r, &o.Capture) }},
		{"contains", func() bool { return o.Contains != nil }, func(r json.RawMessage) error { return decodeBool("contains", r, &o.Contains) }},
		{"debug", func() bool { return o.Debug != nil }, func(r json.RawMessage) error { return decodeBool("debug", r, &o.Debug) }},
		{"dot", func() bool { return o.Dot != nil }, func(r json.RawMessage) error { return decodeBool("dot", r, &o.Dot) }},
		{"expandRange", func() bool { return o.ExpandRange != nil }, func(r json.RawMessage) error { return decodeRaw(r, &o.ExpandRange) }},
		{"fastpaths", func() bool { return o.Fastpaths != nil }, func(r json.RawMessage) error { return decodeBool("fastpaths", r, &o.Fastpaths) }},
		{"flags", func() bool { return o.Flags != nil }, func(r json.RawMessage) error { return decodeString("flags", r, &o.Flags) }},
		{"keepQuotes", func() bool { return o.KeepQuotes != nil }, func(r json.RawMessage) error { return decodeBool("keepQuotes", r, &o.KeepQuotes) }},
		{"literalBrackets", func() bool { return o.LiteralBrackets != nil }, func(r json.RawMessage) error { return decodeBool("literalBrackets", r, &o.LiteralBrackets) }},
		{"maxExtglobRecursion", func() bool { return o.MaxExtglobRecursion != nil }, func(r json.RawMessage) error { return decodeCap("maxExtglobRecursion", r, &o.MaxExtglobRecursion) }},
		{"maxLength", func() bool { return o.MaxLength != nil }, func(r json.RawMessage) error { return decodeInt("maxLength", r, &o.MaxLength) }},
		{"nobrace", func() bool { return o.NoBrace != nil }, func(r json.RawMessage) error { return decodeBool("nobrace", r, &o.NoBrace) }},
		{"nobracket", func() bool { return o.NoBracket != nil }, func(r json.RawMessage) error { return decodeBool("nobracket", r, &o.NoBracket) }},
		{"nocase", func() bool { return o.NoCase != nil }, func(r json.RawMessage) error { return decodeBool("nocase", r, &o.NoCase) }},
		{"noext", func() bool { return o.NoExt != nil }, func(r json.RawMessage) error { return decodeBool("noext", r, &o.NoExt) }},
		{"noextglob", func() bool { return o.NoExtglob != nil }, func(r json.RawMessage) error { return decodeBool("noextglob", r, &o.NoExtglob) }},
		{"noglobstar", func() bool { return o.NoGlobstar != nil }, func(r json.RawMessage) error { return decodeBool("noglobstar", r, &o.NoGlobstar) }},
		{"nonegate", func() bool { return o.NoNegate != nil }, func(r json.RawMessage) error { return decodeBool("nonegate", r, &o.NoNegate) }},
		{"posix", func() bool { return o.Posix != nil }, func(r json.RawMessage) error { return decodeBool("posix", r, &o.Posix) }},
		{"prepend", func() bool { return o.Prepend != nil }, func(r json.RawMessage) error { return decodeString("prepend", r, &o.Prepend) }},
		{"regex", func() bool { return o.Regex != nil }, func(r json.RawMessage) error { return decodeBool("regex", r, &o.Regex) }},
		{"strictBrackets", func() bool { return o.StrictBrackets != nil }, func(r json.RawMessage) error { return decodeBool("strictBrackets", r, &o.StrictBrackets) }},
		{"strictSlashes", func() bool { return o.StrictSlashes != nil }, func(r json.RawMessage) error { return decodeBool("strictSlashes", r, &o.StrictSlashes) }},
		{"unescape", func() bool { return o.Unescape != nil }, func(r json.RawMessage) error { return decodeBool("unescape", r, &o.Unescape) }},
		{"windows", func() bool { return o.Windows != nil }, func(r json.RawMessage) error { return decodeBool("windows", r, &o.Windows) }},
	}
}

type optionKey struct {
	name   string
	isSet  func() bool
	decode func(json.RawMessage) error
}

// UnmarshalJSON decodes a recorded options object, and fails on any key it does
// not know.
//
// The rejection is the point. tools/emit/generate.js throws on a key that is in
// neither the emit allow-list nor the declared matcher-only set, so the two ends
// agree; a key that appeared on only one side would otherwise be dropped in
// silence, and a silently smaller option surface is indistinguishable from a
// port that handles more than it does.
func (o *Options) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("options: %w", err)
	}

	// Deterministic order, so a record with two unknown keys always names the
	// same one.
	names := make([]string, 0, len(raw))
	for name := range raw {
		names = append(names, name)
	}
	sort.Strings(names)

	known := make(map[string]func(json.RawMessage) error, 26)
	for _, k := range o.optionKeys() {
		known[k.name] = k.decode
	}

	for _, name := range names {
		decode, ok := known[name]
		if !ok {
			return fmt.Errorf("unknown option key %q: it is in neither the emit allow-list nor a declared exclusion", name)
		}
		if err := decode(raw[name]); err != nil {
			return err
		}
	}

	o.raw = string(data)
	return nil
}

func decodeBool(key string, raw json.RawMessage, dst **bool) error {
	var v bool
	if err := json.Unmarshal(raw, &v); err != nil {
		return fmt.Errorf("option %q: %w", key, err)
	}
	*dst = &v
	return nil
}

func decodeString(key string, raw json.RawMessage, dst **string) error {
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return fmt.Errorf("option %q: %w", key, err)
	}
	*dst = &v
	return nil
}

func decodeInt(key string, raw json.RawMessage, dst **int) error {
	var v int
	if err := json.Unmarshal(raw, &v); err != nil {
		return fmt.Errorf("option %q: %w", key, err)
	}
	*dst = &v
	return nil
}

func decodeCap(key string, raw json.RawMessage, dst **ExtglobCap) error {
	var v ExtglobCap
	if err := json.Unmarshal(raw, &v); err != nil {
		return fmt.Errorf("option %q: %w", key, err)
	}
	*dst = &v
	return nil
}

// decodeRaw stores a value the loader deliberately does not interpret. It never
// fails: the point is to surface the key at all, not to give it a meaning.
func decodeRaw(raw json.RawMessage, dst **RawValue) error {
	v := make(RawValue, len(raw))
	copy(v, raw)
	*dst = &v
	return nil
}

// IsDefault reports whether the record carries no emit-relevant option at all.
//
// It decides the gate's option *stratum*, not attemptability. The two used to be
// the same question and stopped being one the moment a key was threaded through:
// internal/parse.Parse now takes an Options, so a `{"windows":true}` record has a
// callable entry point and is scored, while still counting as non-default.
//
// Attemptability is emitParseOptions in emit_test.go, which asks whether
// internal/parse.Options can express the keys this record carries. Reusing
// IsDefault for it would freeze the gate at the day it was written; reusing
// attemptability for the stratum would migrate each newly threaded key into the
// defaultOptions row and inflate the number that exists to say how much of the
// score `make tokens` had already proved.
func (o *Options) IsDefault() bool {
	for _, k := range o.optionKeys() {
		if k.isSet() {
			return false
		}
	}
	return true
}

// SetKeys returns the upstream names of the options this record carries, sorted.
//
// The first element is the blocker the gate reports for a field it cannot
// attempt, which is why the order is stable rather than incidental: the report's
// build order would otherwise reshuffle between runs.
func (o *Options) SetKeys() []string {
	var keys []string
	for _, k := range o.optionKeys() {
		if k.isSet() {
			keys = append(keys, k.name)
		}
	}
	sort.Strings(keys)
	return keys
}

// Key identifies the option set, values included.
//
// Two records with the same keys but different values are different option sets;
// counting by SetKeys alone would merge {"maxLength":1} with {"maxLength":9}.
func (o *Options) Key() string {
	if o.raw == "" {
		return "{}"
	}
	return o.raw
}

// Case is one (pattern, options) pair and everything upstream produced for it
// below the matcher.
//
// The composite key is Pattern plus Options; there is no id field, for the
// reason tools/extract/extract.js records — an id assigned after sorting
// renumbers unrelated cases the first time a new upstream test lands.
type Case struct {
	Pattern string  `json:"pattern"`
	Options Options `json:"options"`

	// FastpathEligible is picomatch.js:312's predicate: input[0] is "." or "*",
	// over UTF-16 code units.
	//
	// It is a field of its own because parse.fastpaths has three outcomes, not
	// two: not eligible, eligible and falsy — the picomatch.js:316 fall-through,
	// the single most consequential recorded fact about that path — and eligible
	// with a string. A lone *string would merge the first two.
	FastpathEligible bool `json:"fastpathEligible"`
	// FastpathOutput is what parse.fastpaths returned, when it returned a
	// string. The value is BARE: fastpaths never wraps.
	FastpathOutput *string `json:"fastpathOutput"`
	// FastpathThrow is set when parse.fastpaths threw. It is called bare at
	// picomatch.js:313, so the throw escapes makeRe from there rather than from
	// parse().
	FastpathThrow *Throw `json:"fastpathThrow"`

	// ScannerOutput is parse(pattern, {...opts, fastpaths: false}).output — the
	// full scanner, BARE, and exactly what internal/parse.State.Output targets.
	ScannerOutput *string `json:"scannerOutput"`
	// Negated is state.negated, which compileRe:274 tests `=== true`. The wrap
	// it applies, `^(?!source).*$`, differs structurally from utils.wrapOutput's
	// `(?:^(?!output).*$)`, and the two are reachable on different paths.
	Negated *bool `json:"negated"`
	// ScannerThrow is set when the full scanner threw.
	ScannerThrow *Throw `json:"scannerThrow"`

	// Path is which of the three parsers makeRe actually used: [PathNone],
	// [PathTop] or [PathInline].
	Path *string `json:"path"`
	// Output is makeRe(pattern, opts, true) — state.output, the returnOutput
	// short-circuit at picomatch.js:265.
	//
	// It is recorded even though it is derivable from Path plus the two outputs
	// above, because the derivation is the claim being tested. It is also what
	// makes the inline double-wrap a fixture fact rather than a footnote: under
	// default options "foo" records path "inline", output "^(?:foo)$" and source
	// "^(?:^(?:foo)$)$".
	Output *string `json:"output"`

	// Source is the compiled RegExp's .source — compileRe's ^(?:…)$ layer on top
	// of Output (picomatch.js:273), wrapped again as ^(?!…).*$ when Negated.
	//
	// It is NOT that string on 8 of the 2,028 compiled records, for two reasons
	// that a compile layer written from the sentence above will not reproduce.
	// Both are transcribed in docs/transcription-traps.md, #52 and #53, with the
	// census command; the short form is:
	//
	//   - .source is a SERIALISATION, not the string handed to the constructor.
	//     ECMAScript escapes every unescaped "/" outside a character class so the
	//     result can be re-read as a /…/ literal, so "\[/\]" comes back "\[\/\]".
	//     5 records, all containing "[/]".
	//   - toRegex swallows a SyntaxError and returns /$^/ unless opts.debug
	//     (picomatch.js:344-347), so Source is the literal "$^" and the matcher
	//     answers false to everything rather than failing. 3 records.
	//
	// Scoring is fatal on any Wrong, so these are 8 false disagreements waiting
	// for the day emitBlockers[FieldSource] is lifted. Reproduce the two rules;
	// do not "fix" Output to make the diff go away.
	Source *string `json:"source"`
	// Flags is the compiled RegExp's flags, from opts.flags or opts.nocase
	// (picomatch.js:343).
	//
	// It is a plain string rather than a pointer because the recorder never
	// writes one without the other: presence is Source's presence, which
	// [Case.HasCompile] states once so no caller has to know it.
	Flags string `json:"flags"`

	// Throw is set when makeRe itself threw; Path, Output, Source and Flags are
	// all absent then.
	Throw *Throw `json:"throw"`
}

// HasCompile reports whether compileRe ran, and so whether Source and Flags are
// both recorded values rather than both absent.
func (c *Case) HasCompile() bool { return c.Source != nil }

// Layers returns the recorded comparable fields present on this record, in a
// fixed order. It is the gate's denominator, computed here next to the JSON tags
// rather than in the harness, so a field added to the schema cannot be scored
// without appearing in the census. It agrees field for field with
// testdata/emit/summary.json's `layers` block, which the recorder writes from the
// other end:
//
//	node -e "const s=require('./testdata/emit/summary.json');console.log(s.cases.comparableFields, s.layers)"
//
// Scoring is field-level, not case-level, and that is deliberate: a case-level
// score forces a weighting judgment — is a record carrying a fastpath output
// worth more than one without? — where the field-level denominator is mechanical
// and each layer contributes exactly in proportion to how much of it was
// recorded.
//
// [FieldFastpathOutput] is present for every eligible record, not only the ones
// that returned a string, because "fastpaths declined this pattern" is itself a
// recorded answer the port has to reproduce — the picomatch.js:316 fall-through
// is the single most consequential recorded fact about that path.
//
// # Two recorded fields are deliberately not counted
//
// [FieldOutput] is not scored. On the top and none paths it is the layer output
// the path names — byte-identical on 79/79 and 1750/1750 — so scoring it would
// weight the same claim twice and make the compile layer look cheaper than it is
// by comparison. It is still recorded, because a gate that stored only the
// ingredients would be computing the answer it is supposed to check, and because
// on the inline path it is derivable from nothing else in the record:
// wrapOutput runs inside parse() at parse.js:653 over the bare
// parse(pattern, opts), which is not the {fastpaths:false} run [FieldScannerOutput]
// holds — they differ on 59 of the 199 inline records.
//
// Not scored is not unconstrained. TestEmitFixtureShape asserts Output against
// the named layer on top and none; without that, Output was the one recorded
// field a fabricated value could survive the whole suite in.
//
// [FieldThrow] is makeRe's own throw, and on every record but one it is the throw
// the layer below already carries:
//
//	node -e "const l=require('fs').readFileSync('testdata/emit/cases.jsonl','utf8').split('\n').filter(Boolean).map(JSON.parse).filter(r=>r.throw);console.log(l.length, l.filter(r=>JSON.stringify(r.scannerThrow)===JSON.stringify(r.throw)).length)"
//
// reports 10 and 9. The tenth is the empty pattern, which makeRe rejects at
// picomatch.js:306 before any parser runs — a guard on the entry point, not on
// the emitter. Scoring it here would credit or blame the emitter for a check
// above it.
func (c *Case) Layers() []string {
	var fields []string
	add := func(name string, present bool) {
		if present {
			fields = append(fields, name)
		}
	}

	add(FieldPath, c.Path != nil)

	add(FieldScannerOutput, c.ScannerOutput != nil)
	add(FieldNegated, c.Negated != nil)
	add(FieldScannerThrow, c.ScannerThrow != nil)

	add(FieldFastpathOutput, c.FastpathEligible)

	add(FieldSource, c.HasCompile())
	add(FieldFlags, c.HasCompile())

	return fields
}

// Recorded field names. They are constants because the gate keys its blocker map
// and its per-layer census on them, and a typo in either would read as a field
// nobody compares. [FieldOutput] and [FieldThrow] are recorded but not counted —
// see [Case.Layers].
const (
	FieldPath           = "path"
	FieldOutput         = "output"
	FieldThrow          = "throw"
	FieldScannerOutput  = "scannerOutput"
	FieldNegated        = "negated"
	FieldScannerThrow   = "scannerThrow"
	FieldFastpathOutput = "fastpathOutput"
	FieldSource         = "source"
	FieldFlags          = "flags"
)

// LayerOf reports which layer a recorded field belongs to, or "" for a name that
// is not a recorded field.
func LayerOf(field string) string {
	switch field {
	case FieldPath, FieldOutput, FieldThrow:
		return LayerPath
	case FieldScannerOutput, FieldNegated, FieldScannerThrow:
		return LayerScanner
	case FieldFastpathOutput:
		return LayerFastpath
	case FieldSource, FieldFlags:
		return LayerCompile
	default:
		return ""
	}
}

// Load reads every case from a JSONL fixture file.
func Load(path string) ([]Case, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Read(f)
}

// Read parses JSONL cases from r.
func Read(r io.Reader) ([]Case, error) {
	var cases []Case

	sc := bufio.NewScanner(r)
	// A single record carries up to five regex sources for the same pattern, and
	// the larger brace and globstar expansions run to tens of kilobytes each; the
	// default 64 KiB limit would truncate them into parse errors.
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for line := 1; sc.Scan(); line++ {
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}

		var c Case
		if err := json.Unmarshal([]byte(text), &c); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		cases = append(cases, c)
	}

	if err := sc.Err(); err != nil {
		return nil, err
	}
	return cases, nil
}

// Stats summarises a fixture set.
type Stats struct {
	Total             int
	Patterns          int
	OptionSets        int
	DefaultOptions    int
	NonDefaultOptions int

	Negated      int
	Threw        int
	ScannerThrew int

	FastpathEligible int
	FastpathOutput   int
	FastpathThrew    int

	ComparableFields int

	ByPath      map[string]int
	ByLayer     map[string]int
	ByOptionKey map[string]int
}

// Summarise counts a fixture set by category. It is the Go-side check on
// testdata/emit/summary.json, which the recorder writes from the other end.
func Summarise(cases []Case) Stats {
	s := Stats{
		ByPath:      map[string]int{},
		ByLayer:     map[string]int{},
		ByOptionKey: map[string]int{},
	}
	patterns := map[string]struct{}{}
	optionSets := map[string]struct{}{}

	for i := range cases {
		c := &cases[i]
		s.Total++
		patterns[c.Pattern] = struct{}{}
		optionSets[c.Options.Key()] = struct{}{}

		if c.Options.IsDefault() {
			s.DefaultOptions++
		} else {
			s.NonDefaultOptions++
			for _, k := range c.Options.SetKeys() {
				s.ByOptionKey[k]++
			}
		}

		if c.Negated != nil && *c.Negated {
			s.Negated++
		}
		if c.Throw != nil {
			s.Threw++
		}
		if c.ScannerThrow != nil {
			s.ScannerThrew++
		}
		if c.FastpathEligible {
			s.FastpathEligible++
		}
		if c.FastpathOutput != nil {
			s.FastpathOutput++
		}
		if c.FastpathThrow != nil {
			s.FastpathThrew++
		}
		if c.Path != nil {
			s.ByPath[*c.Path]++
		}

		for _, f := range c.Layers() {
			s.ComparableFields++
			s.ByLayer[LayerOf(f)]++
		}
	}

	s.Patterns = len(patterns)
	s.OptionSets = len(optionSets)
	return s
}
