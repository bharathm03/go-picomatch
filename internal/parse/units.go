package parse

import "unicode/utf16"

// units is a string as JavaScript sees one: a sequence of UTF-16 code units.
//
// # Why not string, and why not []rune
//
// picomatch indexes, slices and measures its input in UTF-16 code units, and
// three things in this package depend on the count rather than on the
// characters:
//
//   - the maxLength guard, which compares input.length against 65536;
//   - "?", which consumes exactly one code unit, so one astral character is two;
//   - character-class bodies, which are accumulated one unit at a time and are
//     therefore mid-surrogate between loop iterations — unrepresentable in a Go
//     string.
//
// Go's two obvious representations are both wrong. len(s) counts UTF-8 bytes and
// `for i, r := range s` walks runes; for "\U0001F600" those give 4 and 1 where
// picomatch gives 2. Nothing in testdata/original would report the mistake:
// tools/mutate measures that the runes-not-code-units mutation survives all
// 18,792 upstream fixtures, and the token corpus contains 5 non-ASCII patterns,
// all of them BMP (U+30C0..U+30EB), so its counts agree under either reading.
// The evidence that this matters is in testdata/charaxis, not here.
//
// Conversion happens only at this package's boundary. A Go string cannot hold an
// unpaired surrogate, so a pattern arriving with one has already lost it before
// Parse sees it — that is a question about the public API's signature, not about
// the scanner, and it is not settled here.
type units []uint16

// encode converts a Go string to code units.
//
// It is defined for any input but is not lossless, and the two lossy cases are
// different in kind:
//
//   - Invalid UTF-8 has no JavaScript counterpart at all — a JS string is a
//     sequence of code units, so there is nothing for a stray 0xFF byte to
//     become. []rune substitutes U+FFFD for each one, which collapses inputs
//     that differ: encode("a\xffb") and encode("a\xfeb") are the same units.
//     [State.Consumed] therefore does not round-trip a pattern containing
//     invalid UTF-8, and any two such patterns differing only in their bad
//     bytes are indistinguishable to this package. DECISIONS.md §10.
//   - Unpaired surrogates cannot be carried by a Go string in the first place,
//     so a pattern arriving with one has already lost it before Parse is
//     called. That is a question about the public API's signature, not about
//     the scanner.
func encode(s string) units { return utf16.Encode([]rune(s)) }

// countUnits is len(encode(s)) without the two allocations, so the maxLength
// guard can reject an oversized pattern before converting it. Astral runes cost
// two units; everything else, including the U+FFFD each invalid byte becomes,
// costs one.
func countUnits(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xFFFF {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// String converts back at the package boundary.
//
// utf16.Decode substitutes U+FFFD for an unpaired surrogate, and the scanner can
// hold one: upstream splits pairs mid-token (parse.js:765-770 appends to
// prev.value only), so a token value or output can end on a lone high surrogate.
// The substitution is a measured divergence from upstream's recorded tokens, not
// an oversight — DECISIONS.md §10 has the case and the re-check.
func (u units) String() string { return string(utf16.Decode(u)) }

// clone returns a copy with no shared backing array. Token values are mutated in
// place by the scanner (prev.value += value), so anything stored on a token must
// not alias a slice the loop still holds.
func (u units) clone() units {
	if u == nil {
		return nil
	}
	return append(units{}, u...)
}

// hasPrefix reports whether u begins with an ASCII literal.
func (u units) hasPrefix(s string) bool {
	p := encode(s)
	if len(u) < len(p) {
		return false
	}
	for i, c := range p {
		if u[i] != c {
			return false
		}
	}
	return true
}

// escapeRegex mirrors utils.escapeRegex: /([-*+?.^${}(|)[\]])/g -> "\\$1".
func escapeRegex(u units) units {
	out := make(units, 0, len(u))
	for _, c := range u {
		if isRegexSpecial(c) {
			out = append(out, '\\')
		}
		out = append(out, c)
	}
	return out
}

// isRegexSpecial is the character set of constants.REGEX_SPECIAL_CHARS,
// /[-*+?.^${}(|)[\]]/.
func isRegexSpecial(c uint16) bool {
	switch c {
	case '-', '*', '+', '?', '.', '^', '$', '{', '}', '(', '|', ')', '[', ']':
		return true
	}
	return false
}

// nonSpecialRun returns the length of the leading run matched by
// constants.REGEX_NON_SPECIAL_CHARS, /^[^@![\].,$*+?^{}()|\\/]+/.
//
// Every code unit outside that ASCII set counts, including both halves of a
// surrogate pair, so an astral character is consumed as two units — which is
// what the reference does and why this is a loop over units rather than a
// regexp over a Go string.
func nonSpecialRun(u units) int {
	n := 0
	for n < len(u) {
		switch u[n] {
		case '@', '!', '[', ']', '.', ',', '$', '*', '+', '?', '^', '{', '}', '(', ')', '|', '\\', '/':
			return n
		}
		n++
	}
	return n
}
