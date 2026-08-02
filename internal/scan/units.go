package scan

import "unicode/utf16"

// units is a string as JavaScript sees one: a sequence of UTF-16 code units.
//
// lib/scan.js indexes and slices in those units and reports two of them back to
// the caller. `start` is an index into the input and `slashes` is a list of
// them, so a pattern containing an astral character gives a different answer
// under every other reading: for "\U0001F600/a" the slash is at index 2 in code
// units, 1 in runes and 4 in bytes, and only the first is what upstream records.
// `base`, `glob` and `prefix` are cut at those same offsets.
//
// This duplicates internal/parse's own units type rather than sharing it. The
// two packages are ports of two upstream files that share no code — scan.js has
// its own state machine and does not call parse() — and a shared helper package
// would couple them at a point upstream does not.
type units []uint16

// charNone stands for the NaN that String.prototype.charCodeAt returns for an
// out-of-range index. Real code units are 0..65535, so it cannot collide with
// one, and every comparison upstream makes against a character constant is false
// for it — which is what NaN does too.
const charNone = -1

// encode converts a Go string to code units.
//
// It is not lossless: invalid UTF-8 has no JavaScript counterpart, so []rune
// substitutes U+FFFD for each bad byte and two inputs differing only there
// become one. DECISIONS.md §10 records the same boundary for internal/parse.
func encode(s string) units { return utf16.Encode([]rune(s)) }

// String converts back at the package boundary.
//
// Every slice scan takes is at a boundary it found by scanning — after a "/",
// after "!" or "./" — so no surrogate pair is split here and utf16.Decode's
// U+FFFD substitution for a lone surrogate cannot be reached through Scan.
func (u units) String() string { return string(utf16.Decode(u)) }

// at is str.charCodeAt(i): the code unit at i, or charNone when i is out of
// range. Upstream relies on the out-of-range case — advance() is called without
// an end check in four places.
func (u units) at(i int) int {
	if i < 0 || i >= len(u) {
		return charNone
	}
	return int(u[i])
}

// equalString reports whether u is the given ASCII literal. Used for upstream's
// `base !== '/'` comparison.
func (u units) equalString(s string) bool {
	if len(u) != len(s) {
		return false
	}
	for i := 0; i < len(s); i++ {
		if u[i] != uint16(s[i]) {
			return false
		}
	}
	return true
}

// equalUnits reports whether two unit slices hold the same units. Used for
// upstream's `base !== str`, which is a value comparison in JavaScript and must
// not become an identity comparison here.
func equalUnits(a, b units) bool {
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

// truthy is JavaScript's test on the result of `(code = advance())`, which
// upstream uses as a loop condition in four places.
//
// It is false for two values, and only one of them is obvious. NaN — the
// out-of-range charCodeAt — ends the loop, which is the intent. So does 0: a
// literal NUL in the pattern is a falsy code, so it terminates the brace,
// bracket, extglob and paren scan-to-end loops exactly as the end of input
// would. No fixture contains a NUL, and writing the loop as "until eos" instead
// would be the natural Go shape and would keep scanning past one.
func truthy(code int) bool { return code != 0 && code != charNone }

// isLineTerminator reports whether a code unit is one of the four characters a
// JavaScript regular expression's `.` refuses to match: LF, CR, U+2028, U+2029.
// A negated character class such as `[^\\]` matches them regardless, which is
// the distinction removeBackslashes turns on.
func isLineTerminator(c uint16) bool {
	switch c {
	case 0x000A, 0x000D, 0x2028, 0x2029:
		return true
	}
	return false
}
