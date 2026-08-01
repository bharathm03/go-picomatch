// These tests assert arithmetic, not behaviour.
//
// Nothing here states what picomatch does — that is what testdata/tokens and
// testdata/original are for, and asserting it by hand is the failure mode this
// repo is built to rule out. What is asserted is that this package counts in
// UTF-16 code units, which is a property of the representation and checkable
// against the Unicode standard rather than against picomatch.
//
// It lives in the untagged suite on purpose. The token gate cannot see any of
// it: the recorded corpus contains five non-ASCII patterns and no astral ones,
// so its counts are identical under bytes, runes and code units. tools/mutate
// measures the same blindness from the other side — the runes-not-code-units
// mutation survives all 18,792 upstream fixtures. Without this file the reason
// units exists would be untested by everything in the repo.
package parse

import (
	"errors"
	"strings"
	"testing"
)

// smile is U+1F600: two UTF-16 code units, one rune, four UTF-8 bytes. Every
// count below differs, which is the point.
const smile = "\U0001F600"

func TestUnitsCountsUTF16CodeUnits(t *testing.T) {
	u := encode(smile)
	if len(u) != 2 {
		t.Fatalf("len(encode(%q)) = %d, want 2 UTF-16 code units", smile, len(u))
	}
	if got := len([]rune(smile)); got != 1 {
		t.Fatalf("sanity: []rune gives %d, expected the reading this package must not use", got)
	}
	if got := len(smile); got != 4 {
		t.Fatalf("sanity: len() gives %d, expected the reading this package must not use", got)
	}
	if u.String() != smile {
		t.Fatalf("round-trip: got %q, want %q", u.String(), smile)
	}
}

// TestLengthErrorCountsCodeUnits pins the guard at parse.js:367 to the same
// units upstream compares in. MAX_LENGTH is transcribed from constants.js:93.
func TestLengthErrorCountsCodeUnits(t *testing.T) {
	// 32768 astral characters: 65536 code units, exactly at the cap, but only
	// 32768 runes. A rune-counting guard would accept twice the input.
	atCap := strings.Repeat(smile, maxLength/2)
	if _, err := newScanner(atCap); err != nil {
		t.Fatalf("input of exactly %d code units was rejected: %v", maxLength, err)
	}

	overByOne := atCap + "a"
	_, err := newScanner(overByOne)
	var lengthErr *LengthError
	if !errors.As(err, &lengthErr) {
		t.Fatalf("input of %d code units: got %v, want a LengthError", maxLength+1, err)
	}
	if lengthErr.Length != maxLength+1 {
		t.Fatalf("LengthError.Length = %d, want %d — the count is not in code units",
			lengthErr.Length, maxLength+1)
	}
}

func TestNonSpecialRunWalksCodeUnits(t *testing.T) {
	// Both halves of a surrogate pair fall outside the excluded ASCII set, so
	// the run covers the whole character — two units, not one.
	if got := nonSpecialRun(encode(smile + "*")); got != 2 {
		t.Fatalf("nonSpecialRun(%q) = %d, want 2", smile+"*", got)
	}
	if got := nonSpecialRun(encode("abc/d")); got != 3 {
		t.Fatalf("nonSpecialRun(%q) = %d, want 3", "abc/d", got)
	}
	if got := nonSpecialRun(encode("*abc")); got != 0 {
		t.Fatalf("nonSpecialRun(%q) = %d, want 0", "*abc", got)
	}
	// "-" is absent from REGEX_NON_SPECIAL_CHARS' exclusion set even though it
	// is a regex metacharacter, which is easy to "fix" while transcribing.
	if got := nonSpecialRun(encode("a-b")); got != 3 {
		t.Fatalf("nonSpecialRun(%q) = %d, want 3 — \"-\" is not excluded", "a-b", got)
	}
}

func TestEscapeRegexEscapesTheDocumentedSet(t *testing.T) {
	// The set is constants.REGEX_SPECIAL_CHARS_GLOBAL, /([-*+?.^${}(|)[\]])/g.
	// Every member is checked, not a representative: the set is transcribed by
	// hand, a dropped member changes the emitted regex, and escapeRegex is
	// reachable only through the quoted-string branch, where nothing else in the
	// repo would notice.
	for _, c := range []string{"-", "*", "+", "?", ".", "^", "$", "{", "}", "(", "|", ")", "[", "]"} {
		if got, want := escapeRegex(encode(c)).String(), `\`+c; got != want {
			t.Errorf("escapeRegex(%q) = %q, want %q — a member of REGEX_SPECIAL_CHARS is not escaped", c, got, want)
		}
	}
	// The complement matters as much: an over-eager set escapes characters
	// upstream leaves alone. "/" and "!" are in nonSpecialRun's exclusions but
	// not in this one, which is the pair most likely to be conflated.
	for _, c := range []string{"/", "!", "@", ",", "\\", "a", "0", "_", "~", "=", "<", ">", ":", "%", "#", "&"} {
		if got := escapeRegex(encode(c)).String(); got != c {
			t.Errorf("escapeRegex(%q) = %q, want it unchanged", c, got)
		}
	}
	if got := escapeRegex(encode(`a/b!c`)).String(); got != `a/b!c` {
		t.Fatalf("escapeRegex(%q) = %q, want it unchanged", "a/b!c", got)
	}
	if got := escapeRegex(encode(smile)).String(); got != smile {
		t.Fatalf("escapeRegex mangled a non-ASCII character: %q", got)
	}
}

// TestCloneDoesNotAlias guards a Go-specific hazard with no counterpart
// upstream: push() grows token values in place with append, and a slice sharing
// a backing array would rewrite a token that was already emitted.
//
// The check is two derivations from one base, not one. Appending to a base and
// then reading the base back cannot detect anything — append writes past the
// base's own length, so base.String() is unchanged whether clone copied or
// returned its argument. Replacing clone's body with `return u` passes that
// version of the test and every other test in the repo.
func TestCloneDoesNotAlias(t *testing.T) {
	base := make(units, 2, 16) // spare capacity: append would write in place
	copy(base, encode("ab"))

	first := base.clone().appendUnits(encode("XY"))
	second := base.clone().appendUnits(encode("Z"))

	if first.String() != "abXY" {
		t.Fatalf("the second derivation overwrote the first: %q, want %q", first.String(), "abXY")
	}
	if second.String() != "abZ" {
		t.Fatalf("appendUnits produced %q, want %q", second.String(), "abZ")
	}
	if base.String() != "ab" {
		t.Fatalf("clone aliased its source: %q", base.String())
	}
	if base = append(base, encode("!")...); first.String() != "abXY" {
		t.Fatalf("growing the base rewrote a clone: %q", first.String())
	}
}
