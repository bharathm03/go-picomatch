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

// equal reports whether two unit sequences are the same, as JavaScript's === is
// for strings.
func (u units) equal(v units) bool {
	if len(u) != len(v) {
		return false
	}
	for i := range u {
		if u[i] != v[i] {
			return false
		}
	}
	return true
}

// startsWith is String.prototype.startsWith for a unit sequence.
func (u units) startsWith(v units) bool {
	return len(u) >= len(v) && u[:len(v)].equal(v)
}

// contains is String.prototype.includes for a single code unit.
func (u units) contains(c uint16) bool {
	for _, x := range u {
		if x == c {
			return true
		}
	}
	return false
}

// sliceFrom is String.prototype.slice(i): past the end it is the empty string
// rather than a panic. parse.js:720, :727 and :846 all take it on a value whose
// length they have not checked.
//
// The result shares u's backing array, so a caller that goes on to append to u
// must copy first — see the bracket branch, which does.
func sliceFrom(u units, i int) units {
	if i < 0 {
		i = 0
	}
	if i > len(u) {
		i = len(u)
	}
	return u[i:]
}

// isUnit is JavaScript's `value === "x"` for a single-unit literal.
//
// The length test is the part that matters: by the time the character-class body
// runs, `value` may have grown to two units in the escape branch above
// (parse.js:704), and `"\\]" === "]"` is false. Testing only u[0] would treat an
// escaped bracket as a bare one.
func isUnit(u units, c uint16) bool { return len(u) == 1 && u[0] == c }

// escapePrefix is the backslash-prefix assignment upstream spells as a template
// literal at parse.js:744, :748 and :820. It returns a fresh sequence rather
// than appending into u's spare capacity.
func escapePrefix(u units) units { return append(units{'\\'}, u...) }

// hasRegexChars mirrors utils.hasRegexChars: REGEX_SPECIAL_CHARS.test(str) over
// /[-*+?.^${}(|)[\]]/ — the same set escapeRegex escapes, tested rather than
// replaced. The pattern has no /g flag, so unlike REGEX_SPECIAL_CHARS_GLOBAL it
// carries no lastIndex state between calls.
func hasRegexChars(u units) bool {
	for _, c := range u {
		if isRegexSpecial(c) {
			return true
		}
	}
	return false
}

// key is an exact, order-preserving map key for a unit sequence.
//
// units.String would be wrong here: it folds every unpaired surrogate to U+FFFD,
// so two distinct single-unit sequences would collide into one. DECISIONS.md §10
// records that loss at the package boundary; a map inside the package has no
// reason to inherit it.
func (u units) key() string {
	b := make([]byte, 0, len(u)*2)
	for _, c := range u {
		b = append(b, byte(c>>8), byte(c))
	}
	return string(b)
}

// trim is String.prototype.trim: the ECMAScript WhiteSpace and LineTerminator
// productions, which are not Go's unicode.IsSpace — that set includes U+0085 and
// excludes U+FEFF, and JavaScript is the other way round on both.
func (u units) trim() units {
	i, j := 0, len(u)
	for i < j && isJSWhitespace(u[i]) {
		i++
	}
	for j > i && isJSWhitespace(u[j-1]) {
		j--
	}
	return u[i:j]
}

// isJSWhitespace is WhiteSpace (TAB, VT, FF, ZWNBSP and the Unicode Zs category)
// plus LineTerminator (LF, CR, LS, PS).
func isJSWhitespace(c uint16) bool {
	switch c {
	case 0x0009, 0x000A, 0x000B, 0x000C, 0x000D, 0x0020,
		0x00A0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return c >= 0x2000 && c <= 0x200A
}

// isLineTerminator is the set JavaScript's "." excludes when a regexp has no `s`
// flag.
func isLineTerminator(c uint16) bool {
	switch c {
	case 0x000A, 0x000D, 0x2028, 0x2029:
		return true
	}
	return false
}

// escapeLast mirrors utils.escapeLast: backslash-escape the last unescaped
// occurrence of char, searching backwards from lastIdx.
//
// It is upstream's recursion rewritten as a loop; the recursive call at
// utils.js:39 is in tail position and only ever moves the search left. The
// idx == 0 case relies on JavaScript's input[-1] being undefined rather than a
// character, so a leading occurrence is escaped rather than skipped.
func escapeLast(input units, char uint16, lastIdx int) units {
	for {
		idx := lastIndexOf(input, char, lastIdx)
		if idx == -1 {
			return input
		}
		if idx > 0 && input[idx-1] == '\\' {
			lastIdx = idx - 1
			continue
		}
		out := make(units, 0, len(input)+1)
		out = append(out, input[:idx]...)
		out = append(out, '\\')
		return append(out, input[idx:]...)
	}
}

// lastIndexOf is String.prototype.lastIndexOf(char, position) for a single code
// unit. A negative position clamps to 0, which still examines index 0 — the
// behaviour escapeLast's recursion depends on.
func lastIndexOf(u units, c uint16, position int) int {
	start := position
	if start < 0 {
		start = 0
	}
	if start > len(u)-1 {
		start = len(u) - 1
	}
	for i := start; i >= 0; i-- {
		if u[i] == c {
			return i
		}
	}
	return -1
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
