// These tests assert structure and arithmetic, not behaviour.
//
// Nothing here states what upstream's answer is for a pattern. The 586 recorded
// lib/scan.scan cases and the 12 lib/utils.basename cases hold those answers,
// and scan_conformance_test.go is what replays them. What is asserted here are
// properties of this implementation that no percentage can see: that Scan
// terminates and does not panic on any short pattern, that it is deterministic,
// that the offsets it reports agree with the strings it reports, and that the
// two fields gated on Options.Parts stay absent without it.
//
// It is untagged because the conformance harness is not. `go test ./...` is what
// the repo's everyday signal runs, and a panic or an unbounded loop in a branch
// no fixture reaches would otherwise leave it green.
//
// The one arithmetic assertion here — that indices are UTF-16 code units — is
// the same kind of claim internal/parse/units_test.go makes: it is about UTF-16,
// not about picomatch, and the recorded corpus cannot check it because every
// pattern in it is BMP. DECISIONS.md §8.
package scan

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

// alphabet is every character scan.js branches on, plus one inert letter. Short
// patterns drawn from it reach every arm of the state machine and most of their
// interactions.
var alphabet = []rune{'{', '}', ',', '.', '/', '\\', '*', '?', '[', ']', '(', ')', '!', '@', '+', 'a'}

// optionSets covers each key lib/scan.js reads, alone and in the combinations
// that change each other's meaning (Parts implies scan-to-end; NoExt clears
// flags the loop had already set).
var optionSets = []Options{
	{},
	{Parts: true},
	{ScanToEnd: true},
	{Unescape: true},
	{NoExt: true},
	{NoNegate: true},
	{NoParen: true},
	{Parts: true, Unescape: true},
	{ScanToEnd: true, Unescape: true, NoExt: true},
	{Parts: true, NoNegate: true, NoParen: true},
}

// shortPatterns enumerates every string of up to three characters over the
// alphabet: 4,369 patterns, which is exhaustive rather than sampled at that
// length.
func shortPatterns() []string {
	out := []string{""}
	var build func(prefix string, depth int)
	build = func(prefix string, depth int) {
		if depth == 0 {
			return
		}
		for _, r := range alphabet {
			p := prefix + string(r)
			out = append(out, p)
			build(p, depth-1)
		}
	}
	build("", 3)
	return out
}

// TestScanTerminatesAndIsSelfConsistent is the broad structural sweep: every
// short pattern under every option set, checked for the invariants that relate
// the result's fields to each other and to the input.
//
// Upstream's parse() has inputs on which it never returns (DECISIONS.md §11).
// scan() has its own four inner loops, each advancing without an end check in at
// least one arm, so "it terminates" is a claim worth holding down rather than
// assuming.
func TestScanTerminatesAndIsSelfConsistent(t *testing.T) {
	patterns := shortPatterns()

	// The sweep runs on its own goroutine so a non-advancing loop shows up as a
	// deadline rather than a hung test binary. Failures are collected rather
	// than reported from there: t.Fatalf outside the test goroutine does not
	// stop the test it names.
	var problems []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, p := range patterns {
			for i, opts := range optionSets {
				problems = append(problems, invariantFailures(p, i, opts)...)
				if len(problems) > 25 {
					return
				}
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("Scan did not finish the sweep; an inner loop is not advancing")
	}

	for _, p := range problems {
		t.Error(p)
	}
}

// invariantFailures checks the relations that must hold between a result's
// fields and its input, and returns a description of each one that does not.
func invariantFailures(pattern string, optIndex int, opts Options) []string {
	in := utf16.Encode([]rune(pattern))
	got := Scan(pattern, opts)

	var out []string
	fail := func(format string, args ...any) {
		out = append(out, fmt.Sprintf("%q/%d: ", pattern, optIndex)+fmt.Sprintf(format, args...))
	}

	if got.Input != pattern {
		fail("Input is %q, want the pattern unchanged", got.Input)
	}

	// Start indexes the input in code units and Prefix is exactly what it skips.
	// They are reported as separate fields, so they can disagree.
	switch {
	case got.Start < 0 || got.Start > len(in):
		fail("Start %d out of range for %d code units", got.Start, len(in))
	default:
		if want := string(utf16.Decode(in[:got.Start])); got.Prefix != want {
			fail("Prefix %q, want %q for Start %d", got.Prefix, want, got.Start)
		}
	}

	// Parts and Slashes are the two fields gated on an option. Reporting them
	// unasked would be a divergence the conformance harness cannot see: it
	// iterates the recording, and a recording made without the option has no
	// such key to iterate.
	if !opts.Parts {
		if got.Parts != nil || got.Slashes != nil {
			fail("Parts/Slashes populated without Options.Parts")
		}
		return out
	}

	last := -1
	for _, s := range got.Slashes {
		if s <= last || s >= len(in) {
			fail("Slashes %v are not strictly increasing indices into %d code units", got.Slashes, len(in))
			break
		}
		if in[s] != '/' {
			fail("Slashes %v names index %d, which is not a forward slash", got.Slashes, s)
			break
		}
		last = s
	}
	if len(got.Parts) > len(got.Slashes)+1 {
		fail("%d parts from %d slashes", len(got.Parts), len(got.Slashes))
	}
	return out
}

// TestScanIsDeterministic guards against state leaking between calls — a package
// level buffer, or a slice handed out and then reused.
func TestScanIsDeterministic(t *testing.T) {
	for _, p := range shortPatterns() {
		for i, opts := range optionSets {
			first := Scan(p, opts)
			second := Scan(p, opts)

			if first.Input != second.Input || first.Prefix != second.Prefix ||
				first.Start != second.Start || first.Base != second.Base ||
				first.Glob != second.Glob || first.IsBrace != second.IsBrace ||
				first.IsBracket != second.IsBracket || first.IsGlob != second.IsGlob ||
				first.IsExtglob != second.IsExtglob || first.IsGlobstar != second.IsGlobstar ||
				first.Negated != second.Negated || first.NegatedExtglob != second.NegatedExtglob ||
				!equalStrings(first.Parts, second.Parts) || !equalInts(first.Slashes, second.Slashes) {
				t.Fatalf("%q/%d: two calls disagreed:\n %+v\n %+v", p, i, first, second)
			}
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestScanReportsCodeUnitOffsets asserts UTF-16 arithmetic, not picomatch.
//
// An astral character is two code units, one rune and four bytes. The offsets
// scan hands back — Start, and every entry in Slashes — are positions in the
// input, so the three readings give three different answers and only one of them
// is the one a JavaScript caller would get. No recorded case can report the
// mistake: every pattern in the corpus is BMP or ASCII, where all three agree.
func TestScanReportsCodeUnitOffsets(t *testing.T) {
	const astral = "\U0001F600" // U+1F600, two UTF-16 code units

	if got := len(utf16.Encode([]rune(astral))); got != 2 {
		t.Fatalf("premise wrong: %q is %d code units", astral, got)
	}
	if got := len([]rune(astral)); got != 1 {
		t.Fatalf("premise wrong: %q is %d runes", astral, got)
	}
	if got := len(astral); got != 4 {
		t.Fatalf("premise wrong: %q is %d bytes", astral, got)
	}

	got := Scan(astral+"/a", Options{Parts: true})
	if len(got.Slashes) != 1 || got.Slashes[0] != 2 {
		t.Fatalf("Slashes = %v, want [2]: 1 would be runes, 4 would be bytes", got.Slashes)
	}

	// The same arithmetic on the other reported offset. "!" then an astral
	// character: the negation prologue consumes one code unit, so the astral
	// character that follows is still whole in Glob.
	neg := Scan("!"+astral+"*", Options{})
	if neg.Start != 1 || neg.Prefix != "!" {
		t.Fatalf("Start = %d, Prefix = %q, want 1 and \"!\"", neg.Start, neg.Prefix)
	}
	if neg.Glob != astral+"*" {
		t.Fatalf("Glob = %q, want %q", neg.Glob, astral+"*")
	}
}

// TestScanHandlesLongInput checks that nothing in the loop is quadratic enough
// to matter and that a long run of the characters that advance the index without
// an end check still returns. Upstream has no length guard here, so neither does
// this port.
func TestScanHandlesLongInput(t *testing.T) {
	inputs := []string{
		strings.Repeat("a/", 50000),
		strings.Repeat(`\`, 50000),
		strings.Repeat("{", 50000),
		strings.Repeat("[", 50000),
		strings.Repeat("(", 50000),
		strings.Repeat("!", 50000),
		strings.Repeat("*(", 25000),
		strings.Repeat("../", 20000),
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, in := range inputs {
			for _, opts := range optionSets {
				Scan(in, opts)
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("Scan did not return on a long input")
	}
}

// TestBasenameReturnsASegment asserts the one property basename has independent
// of any recording: its answer is always one of the segments the path splits
// into. The 12 recorded cases pin the answers themselves.
func TestBasenameReturnsASegment(t *testing.T) {
	paths := []string{
		"", "a", "/", "//", "a/", "/a", "a/b", "a/b/", `a\b`, `a\b\`,
		`/a\b/c`, `\a/b\c/`, "a//b", "...", "./", "../a",
	}

	for _, p := range paths {
		for _, windows := range []bool{false, true} {
			got := Basename(p, windows)

			found := false
			for _, seg := range splitSeparators(p, windows) {
				if seg == got {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("Basename(%q, %v) = %q, which is not a segment of the path", p, windows, got)
			}
		}
	}
}

// TestBasenameOfEmptyPath pins the one input with no Go answer.
//
// utils.js:63 indexes segs[-1] for an empty path and returns undefined, which a
// function returning string cannot. The recorded cases are all absolute paths
// and never reach it. DECISIONS.md §13.
func TestBasenameOfEmptyPath(t *testing.T) {
	for _, windows := range []bool{false, true} {
		if got := Basename("", windows); got != "" {
			t.Fatalf("Basename(\"\", %v) = %q, want \"\"", windows, got)
		}
	}
}

// TestRemoveBackslashesKeepsBracketRuns exercises the two alternatives of
// utils.removeBackslashes separately, since Options.Unescape reaches it only
// through whatever base and glob happen to be.
//
// The assertions are about the regular expression's shape — which alternative
// matches where — rather than about a pattern's meaning: a backslash before a
// character goes, a trailing one stays because the lookahead has nothing to
// look at, and a bracket run matches whole and is put back untouched.
func TestRemoveBackslashesKeepsBracketRuns(t *testing.T) {
	cases := []struct{ in, want string }{
		{`a\b`, "ab"},
		{`a\`, `a\`},         // nothing after it for the lookahead
		{`a\\b`, "ab"},       // two matches, one code unit each
		{`\[a\]b`, "[a]b"},   // no bracket run: the "]" is preceded by a backslash
		{`[a\]`, "[a]"},      // nor here: the run needs a "]" after the "[^\\]"
		{`[a\]b]`, `[a\]b]`}, // a run, so its backslash survives
		{`[\]\a]b`, `[\]\a]b`},
		{`[]]\a`, "[]]a"},
		{`[ab]\c`, "[ab]c"},
		{`[a`, `[a`},
		{"a\\\nb", "a\\\nb"},                 // `.` does not match a line terminator
		{"\\\u2028a", "\\\u2028a"},           // and U+2028 is one of the four
		{"[a\u2029b]x]\\c", "[a\u2029b]x]c"}, // `[^\\]` matches one regardless
	}

	for _, c := range cases {
		if got := removeBackslashes(encode(c.in)).String(); got != c.want {
			t.Errorf("removeBackslashes(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
