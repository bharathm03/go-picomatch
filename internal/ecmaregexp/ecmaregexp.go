// Package ecmaregexp answers one question: would `new RegExp(src)` return, or
// throw?
//
// # Why the question is asked twice
//
// Upstream reaches for the RegExp constructor in two places that have nothing
// else in common, and in both it uses the *outcome* rather than the regex.
//
// expandRange (parse.js:22-38) decides what a "{a..b}" compiles to by building
// a character class and handing it to the constructor:
//
//	const value = `[${args.join('-')}]`;
//	try { new RegExp(value); } catch (ex) {
//	  return args.map(v => utils.escapeRegex(v)).join('..');
//	}
//	return value;
//
// toRegex (picomatch.js:341-348) does the same thing to the finished source,
// and on a throw returns /$^/ — a regex matching nothing — so a pattern that
// cannot compile answers false to everything instead of failing. That is the 3
// recorded "$^" sources in testdata/emit, and docs/transcription-traps.md #53.
//
// Both are decisions made by what the host engine accepts. Go has no such
// engine to ask: regexp is RE2, which rejects sources V8 accepts
// (backreferences, lookaround) and accepts sources V8 rejects, so calling
// regexp.Compile here would answer a different question — see DECISIONS.md §1
// for the same mismatch one level up, §15 for expandRange and §17 for toRegex.
//
// The package was inside internal/parse while expandRange was its only caller.
// It moved out on the day the compile layer landed rather than being reached
// for across a package boundary, because "which layer is wrong" is the whole
// point of the four-oracle split and a shared helper that lives in one of them
// blurs it.
package ecmaregexp

import "unicode"

// units is a sequence of UTF-16 code units, and the reason is the one in
// DECISIONS.md §8: the grammar below counts code units, so a surrogate pair is
// two characters and "[<U+1F600>-<U+1F601>]" is a range *error*. It is an alias
// rather than a defined type so internal/parse can pass its own `units` without
// a conversion at the call site.
type units = []uint16

// What follows is the acceptance predicate itself, transcribed from
// the ECMAScript pattern grammar (ES2024 22.2.1 plus the Annex B B.1.2
// extensions) as V8 implements it for a non-unicode pattern with no flags. It
// answers only "would the constructor throw"; it does not compile, match or
// hold any state.
//
// # The five things that make it different from a regexp parser
//
//   - It runs in **non-unicode mode**. A surrogate pair is two code units, not
//     one code point, which is why [<U+1F600>-<U+1F601>] is a *range error*
//     (\uDE00-\uD83D) rather than a one-character range. units is the right
//     representation for the same reason the scanner uses it — DECISIONS.md §8.
//   - **Annex B leniency is on.** "\c" not followed by a control letter is a
//     literal backslash; "\8" is an identity escape; an unmatched "{" is a
//     pattern character; "]" and "}" outside a class are pattern characters;
//     "\1" with no capture group is a legacy octal escape rather than an error.
//   - **Only two things in a character class can fail**: an unterminated class
//     and a range whose ends are out of order. Everything else in a class is
//     accepted, so the work is entirely in computing each ClassAtom's *value*.
//   - **A "{" is a quantifier only if it parses as one.** "a{1,2}" quantifies,
//     "a{1,2,3}" is five literal characters, and "{1}" at the start of an
//     alternative is "Nothing to repeat" while "{" alone is fine.
//   - **"\k" changes meaning depending on the rest of the pattern.** It is a
//     named backreference if a GroupName appears anywhere — before or after it
//     — and an identity escape otherwise, which is why there is a pre-scan.
//
// The predicate is checked against V8 rather than argued for; the enumeration
// and its count are in DECISIONS.md §15.

// Valid reports whether `new RegExp(src)` returns rather than throws, for a
// pattern compiled with no flags.
//
// No flags is not a simplification here: `nocase` and `flags` take exactly two
// values across the whole corpus, "" and "i", and neither can make the
// constructor throw. A flags string that could — a repeated or unknown letter —
// has no recorded case, so it is left unanswered rather than guessed at.
func Valid(src units) bool {
	v := &reValidator{src: src, named: scanHasGroupName(src), names: map[string]bool{}}
	if !v.disjunction(0) {
		return false
	}
	if v.i != len(v.src) {
		return false
	}
	// A named backreference may name a group declared later, so references are
	// resolved after the whole pattern has been read.
	for _, r := range v.refs {
		if !v.names[r] {
			return false
		}
	}
	return true
}

type reValidator struct {
	src units
	i   int

	// named is the pre-scan result: does a GroupName appear anywhere. It decides
	// whether "\k" is a backreference or an identity escape, and the decision is
	// global rather than positional.
	named bool
	names map[string]bool
	refs  []string
}

// scanGroupName-style pre-scan. V8 does the same walk (RegExpParser's
// ScanForCaptures) before the real parse, because the grammar parameter that
// turns "\k" into a backreference depends on the whole pattern.
//
// It has to track escapes and character classes, because "(?<" inside a class
// or behind a backslash is not a group.
func scanHasGroupName(src units) bool {
	for i := 0; i < len(src); {
		switch src[i] {
		case '\\':
			i += 2
		case '[':
			i++
			for i < len(src) && src[i] != ']' {
				if src[i] == '\\' {
					i += 2
					continue
				}
				i++
			}
			i++
		case '(':
			i++
			if i < len(src) && src[i] == '?' {
				i++
				if i < len(src) && src[i] == '<' {
					if i+1 >= len(src) || (src[i+1] != '=' && src[i+1] != '!') {
						return true
					}
				}
			}
		default:
			i++
		}
	}
	return false
}

func (v *reValidator) disjunction(depth int) bool {
	for {
		if !v.alternative(depth) {
			return false
		}
		if v.i < len(v.src) && v.src[v.i] == '|' {
			v.i++
			continue
		}
		return true
	}
}

func (v *reValidator) alternative(depth int) bool {
	for v.i < len(v.src) {
		switch v.src[v.i] {
		case '|':
			return true
		case ')':
			// Unmatched ")" at the top level; the close of a group below it.
			return depth > 0
		}
		if !v.term(depth) {
			return false
		}
	}
	return true
}

// quantifiability is what a Term hands to the quantifier check. The three states
// are distinct because ECMAScript reports them differently ("Nothing to repeat"
// versus "Invalid quantifier") and because Annex B makes lookaheads — and only
// lookaheads — quantifiable.
type quantifiability int

const (
	quantifiable    quantifiability = iota // an Atom, or an Annex B QuantifiableAssertion
	notQuantifiable                        // ^ $ \b \B: "Nothing to repeat"
	badQuantifier                          // a lookbehind: "Invalid quantifier"
)

func (v *reValidator) term(depth int) bool {
	q := quantifiable

	switch c := v.src[v.i]; {
	case c == '^' || c == '$':
		v.i++
		q = notQuantifiable

	case c == '\\':
		if v.i+1 < len(v.src) && (v.src[v.i+1] == 'b' || v.src[v.i+1] == 'B') {
			v.i += 2
			q = notQuantifiable
			break
		}
		if !v.atomEscape() {
			return false
		}

	case c == '(':
		g, ok := v.group(depth)
		if !ok {
			return false
		}
		q = g

	case c == '[':
		if !v.characterClass() {
			return false
		}

	case c == '*' || c == '+' || c == '?':
		return false // Nothing to repeat

	case c == '{':
		// Annex B: a "{" that does not parse as a quantifier is an
		// ExtendedPatternCharacter. One that does is a quantifier with no atom
		// in front of it, which is an error however its numbers are ordered.
		if _, _, ok := v.quantifierBraces(v.i); ok {
			return false
		}
		v.i++

	default:
		// ExtendedPatternCharacter. Annex B admits "]" and "}" here, which is
		// why "[a]b]" and "a}" are valid patterns.
		v.i++
	}

	return v.quantifier(q)
}

// quantifier consumes any quantifiers following a term and reports whether they
// are legal there.
//
// The loop matters: a second quantifier is "Nothing to repeat" ("a**"), while
// the "?" that makes one lazy is not a second quantifier ("a*?"). So each pass
// consumes one quantifier plus an optional lazy marker, and leaves the term
// unquantifiable for the next pass.
func (v *reValidator) quantifier(q quantifiability) bool {
	for v.i < len(v.src) {
		switch v.src[v.i] {
		case '*', '+', '?':
			v.i++
		case '{':
			end, ordered, ok := v.quantifierBraces(v.i)
			if !ok {
				return true // a literal "{"; the term is finished
			}
			if !ordered {
				return false // numbers out of order in {} quantifier
			}
			v.i = end
		default:
			return true
		}

		if q != quantifiable {
			return false
		}
		if v.i < len(v.src) && v.src[v.i] == '?' {
			v.i++ // the lazy marker
		}
		q = notQuantifiable
	}
	return true
}

// quantifierBraces tests whether src[at] opens a QuantifierPrefix of the "{n}",
// "{n,}" or "{n,m}" forms, and returns the index after its "}" plus whether the
// bounds are in order.
//
// Anything else — "{", "{}", "{,2}", "{1", "{1,2,3}" — is not a quantifier at
// all under Annex B, and the caller treats the "{" as a literal.
func (v *reValidator) quantifierBraces(at int) (end int, ordered, ok bool) {
	i := at + 1
	lo, n := v.decimalDigits(i)
	if n == 0 {
		return 0, false, false
	}
	i += n

	if i < len(v.src) && v.src[i] == '}' {
		return i + 1, true, true
	}
	if i >= len(v.src) || v.src[i] != ',' {
		return 0, false, false
	}
	i++
	if i < len(v.src) && v.src[i] == '}' {
		return i + 1, true, true // "{n,}" — no upper bound to be out of order
	}
	hi, m := v.decimalDigits(i)
	if m == 0 {
		return 0, false, false
	}
	i += m
	if i >= len(v.src) || v.src[i] != '}' {
		return 0, false, false
	}
	return i + 1, !decimalGreater(lo, hi), true
}

// decimalDigits returns the digit run starting at i and its length.
func (v *reValidator) decimalDigits(i int) (units, int) {
	j := i
	for j < len(v.src) && v.src[j] >= '0' && v.src[j] <= '9' {
		j++
	}
	return v.src[i:j], j - i
}

// decimalGreater compares two digit runs by mathematical value.
//
// It is not a uint64 conversion: the bounds are unbounded in the grammar, and
// V8 compares the exact values — "a{99999999999,1}" is out of order while
// "a{01,1}" is not.
func decimalGreater(a, b units) bool {
	a, b = stripLeadingZeros(a), stripLeadingZeros(b)
	if len(a) != len(b) {
		return len(a) > len(b)
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func stripLeadingZeros(u units) units {
	i := 0
	for i < len(u)-1 && u[i] == '0' {
		i++
	}
	return u[i:]
}

// group parses "(", everything to its ")", and reports how the result may be
// quantified.
func (v *reValidator) group(depth int) (quantifiability, bool) {
	v.i++ // "("
	q := quantifiable

	if v.i < len(v.src) && v.src[v.i] == '?' {
		v.i++
		if v.i >= len(v.src) {
			return q, false // Invalid group
		}
		switch v.src[v.i] {
		case ':':
			v.i++
		case '=', '!':
			v.i++ // Annex B QuantifiableAssertion: a lookahead may be quantified
		case '<':
			v.i++
			if v.i < len(v.src) && (v.src[v.i] == '=' || v.src[v.i] == '!') {
				v.i++
				q = badQuantifier // a lookbehind may not
				break
			}
			name, ok := v.groupName()
			if !ok {
				return q, false // Invalid capture group name
			}
			if v.names[name] {
				return q, false // Duplicate capture group name
			}
			v.names[name] = true
		default:
			return q, false // Invalid group
		}
	}

	if !v.disjunction(depth + 1) {
		return q, false
	}
	if v.i >= len(v.src) || v.src[v.i] != ')' {
		return q, false // Unterminated group
	}
	v.i++
	return q, true
}

// groupName reads a RegExpIdentifierName up to its ">", starting just after the
// "<".
//
// Two things separate it from an ASCII identifier scan. Names are matched by
// *code point*, so a surrogate pair is one character — U+1D400 is a valid name
// and U+1F600 is not, because the first is a letter and the second is not. And
// a name may spell its characters with "\u" escapes, including the "\u{...}"
// form, which is accepted here even though the surrounding pattern is not in
// unicode mode.
func (v *reValidator) groupName() (string, bool) {
	var name []rune
	for {
		if v.i >= len(v.src) {
			return "", false
		}
		if v.src[v.i] == '>' {
			v.i++
			break
		}
		cp, ok := v.identCodePoint()
		if !ok {
			return "", false
		}
		if len(name) == 0 {
			if !isIDStartCP(cp) {
				return "", false
			}
		} else if !isIDContinueCP(cp) {
			return "", false
		}
		name = append(name, cp)
	}
	if len(name) == 0 {
		return "", false // "(?<>x)"
	}
	return string(name), true
}

// identCodePoint reads one character of a group name: a "\u" escape, a
// surrogate pair, or a lone unit.
func (v *reValidator) identCodePoint() (rune, bool) {
	if v.src[v.i] == '\\' {
		if v.i+1 >= len(v.src) || v.src[v.i+1] != 'u' {
			return 0, false
		}
		v.i += 2
		if v.i < len(v.src) && v.src[v.i] == '{' {
			j := v.i + 1
			val := rune(0)
			n := 0
			for j < len(v.src) && isHexUnit(v.src[j]) {
				val = val*16 + hexValue(v.src[j])
				if val > 0x10FFFF {
					return 0, false
				}
				j++
				n++
			}
			if n == 0 || j >= len(v.src) || v.src[j] != '}' {
				return 0, false
			}
			v.i = j + 1
			return val, true
		}
		if v.i+3 >= len(v.src) {
			return 0, false
		}
		val := rune(0)
		for k := 0; k < 4; k++ {
			if !isHexUnit(v.src[v.i+k]) {
				return 0, false
			}
			val = val*16 + hexValue(v.src[v.i+k])
		}
		v.i += 4
		// A "\u" escape may itself be half of a pair.
		if val >= 0xD800 && val <= 0xDBFF {
			if lo, n, ok := v.peekTrailSurrogate(); ok {
				v.i += n
				return 0x10000 + (val-0xD800)<<10 + (lo - 0xDC00), true
			}
		}
		return val, true
	}

	c := rune(v.src[v.i])
	v.i++
	if c >= 0xD800 && c <= 0xDBFF {
		if lo, n, ok := v.peekTrailSurrogate(); ok {
			v.i += n
			return 0x10000 + (c-0xD800)<<10 + (lo - 0xDC00), true
		}
	}
	return c, true
}

// peekTrailSurrogate reports a trailing surrogate at the cursor, written either
// literally or as a "\uDCxx" escape, and how many units it occupies.
func (v *reValidator) peekTrailSurrogate() (rune, int, bool) {
	if v.i < len(v.src) && v.src[v.i] >= 0xDC00 && v.src[v.i] <= 0xDFFF {
		return rune(v.src[v.i]), 1, true
	}
	if v.i+5 < len(v.src) && v.src[v.i] == '\\' && v.src[v.i+1] == 'u' {
		val := rune(0)
		for k := 0; k < 4; k++ {
			if !isHexUnit(v.src[v.i+2+k]) {
				return 0, 0, false
			}
			val = val*16 + hexValue(v.src[v.i+2+k])
		}
		if val >= 0xDC00 && val <= 0xDFFF {
			return val, 6, true
		}
	}
	return 0, 0, false
}

// atomEscape consumes a "\" and what follows it, outside a character class.
//
// Only one escape can fail. In non-unicode Annex B, IdentityEscape admits any
// character, DecimalEscape admits any digit run (an over-large backreference is
// a legacy octal escape rather than an error), and a malformed "\x" or "\u" is
// an identity escape — so "\" plus one unit is always consumed and always
// legal. The exceptions are "\" at the end of the pattern, and "\k" once the
// pattern is known to declare a GroupName, at which point it must be a
// resolvable named backreference.
//
// Consuming exactly one unit after the "\" is deliberate rather than a
// shortcut. The longer escapes ("\x41", "A", "\101") spell their tails
// with characters that are inert as pattern characters, so treating those tails
// as literals cannot change whether the pattern parses — it can only change
// what it matches, which is not the question here. Inside a class it *is* the
// question, and classAtom reads the full escape for that reason.
func (v *reValidator) atomEscape() bool {
	v.i++ // "\"
	if v.i >= len(v.src) {
		return false // \ at end of pattern
	}
	if v.src[v.i] == 'k' && v.named {
		v.i++
		if v.i >= len(v.src) || v.src[v.i] != '<' {
			return false // Invalid named reference
		}
		v.i++
		name, ok := v.groupName()
		if !ok {
			return false // Invalid capture group name
		}
		v.refs = append(v.refs, name)
		return true
	}
	v.i++
	return true
}

// characterClass parses "[" to its "]".
//
// Two rules carry all the weight. A "]" straight after "[" or "[^" *closes* an
// empty class — unlike picomatch's own bracket branch, where it is the first
// member (docs/transcription-traps.md #31) — so "[]" and "[^]" are legal and
// "[]]" is an empty class followed by a literal "]". And a "-" between two
// ClassAtoms is a range only when a right-hand atom exists before the "]", so
// "[a-]" and "[-a]" are member lists rather than malformed ranges.
func (v *reValidator) characterClass() bool {
	v.i++ // "["
	if v.i < len(v.src) && v.src[v.i] == '^' {
		v.i++
	}
	for {
		if v.i >= len(v.src) {
			return false // Unterminated character class
		}
		if v.src[v.i] == ']' {
			v.i++
			return true
		}
		lo, hasLo, ok := v.classAtom()
		if !ok {
			return false
		}
		if v.i >= len(v.src) || v.src[v.i] != '-' {
			continue
		}
		if v.i+1 < len(v.src) && v.src[v.i+1] == ']' {
			v.i++ // a trailing "-" is a member, and the "]" closes next time round
			continue
		}
		if v.i+1 >= len(v.src) {
			v.i++ // "-" at the end of the input; the class is unterminated
			continue
		}
		v.i++ // the "-"
		hi, hasHi, ok := v.classAtom()
		if !ok {
			return false
		}
		// Annex B: when either end is a CharacterClassEscape (\d \w \s and
		// their negations) the "-" is a literal member and no range is formed,
		// so there is nothing to be out of order. "[a-\d]" and "[\w-a]" are
		// both legal.
		if hasLo && hasHi && lo > hi {
			return false // Range out of order in character class
		}
	}
}

// classAtom consumes one ClassAtom and returns its code unit value.
//
// The second result is JavaScript's distinction between an atom that *has* a
// value and one that does not: \d, \D, \s, \S, \w and \W stand for sets, and an
// endpoint that is a set suppresses the range check rather than failing it.
//
// The third is whether the atom parsed at all, which only "\" at the end of the
// pattern and a "\k" under a declared GroupName can fail.
func (v *reValidator) classAtom() (value uint16, hasValue, ok bool) {
	if v.src[v.i] != '\\' {
		c := v.src[v.i]
		v.i++
		return c, true, true
	}
	if v.i+1 >= len(v.src) {
		return 0, false, false // \ at end of pattern
	}

	switch e := v.src[v.i+1]; e {
	case 'd', 'D', 's', 'S', 'w', 'W':
		v.i += 2
		return 0, false, true
	case 'b':
		v.i += 2
		return 0x08, true, true // ClassEscape :: b is BACKSPACE, not a word boundary
	case 'f':
		v.i += 2
		return 0x0C, true, true
	case 'n':
		v.i += 2
		return 0x0A, true, true
	case 'r':
		v.i += 2
		return 0x0D, true, true
	case 't':
		v.i += 2
		return 0x09, true, true
	case 'v':
		v.i += 2
		return 0x0B, true, true

	case 'c':
		// Annex B widens ClassControlLetter to the digits and "_" as well as
		// the letters. When the next unit is none of those the escape is not a
		// control escape at all: the ClassAtom is the *backslash itself*, and
		// the "c" is left to be read as the next atom. That is why "[a-\c]" is
		// a range error — 'a' against 0x5C — rather than a literal "c".
		if v.i+2 < len(v.src) && isClassControlLetter(v.src[v.i+2]) {
			val := v.src[v.i+2] & 0x1F
			v.i += 3
			return val, true, true
		}
		v.i++
		return '\\', true, true

	case 'x':
		if v.i+3 < len(v.src) && isHexUnit(v.src[v.i+2]) && isHexUnit(v.src[v.i+3]) {
			val := uint16(hexValue(v.src[v.i+2])<<4 | hexValue(v.src[v.i+3]))
			v.i += 4
			return val, true, true
		}
		v.i += 2
		return 'x', true, true // IdentityEscape

	case 'u':
		// The "\u{...}" form needs unicode mode, so here it is the identity
		// escape "u" followed by the literal characters "{", "6", "1", "}".
		if v.i+5 < len(v.src) && isHexUnit(v.src[v.i+2]) && isHexUnit(v.src[v.i+3]) &&
			isHexUnit(v.src[v.i+4]) && isHexUnit(v.src[v.i+5]) {
			val := uint16(hexValue(v.src[v.i+2])<<12 | hexValue(v.src[v.i+3])<<8 |
				hexValue(v.src[v.i+4])<<4 | hexValue(v.src[v.i+5]))
			v.i += 6
			return val, true, true
		}
		v.i += 2
		return 'u', true, true

	case '0', '1', '2', '3', '4', '5', '6', '7':
		val, n := v.legacyOctal(v.i + 1)
		v.i += 1 + n
		return val, true, true

	case 'k':
		if v.named {
			return 0, false, false // Invalid escape: \k is reserved once a name exists
		}
		v.i += 2
		return 'k', true, true

	default:
		v.i += 2
		return e, true, true
	}
}

// legacyOctal is Annex B's LegacyOctalEscapeSequence as V8 reads it: up to
// three octal digits, and only three when the first two are below 32. So "\377"
// is 255 and "\400" is 32 followed by a literal "0".
func (v *reValidator) legacyOctal(at int) (uint16, int) {
	val := uint16(v.src[at] - '0')
	n := 1
	if at+1 < len(v.src) && isOctalUnit(v.src[at+1]) {
		val = val*8 + uint16(v.src[at+1]-'0')
		n = 2
		if val < 32 && at+2 < len(v.src) && isOctalUnit(v.src[at+2]) {
			val = val*8 + uint16(v.src[at+2]-'0')
			n = 3
		}
	}
	return val, n
}

func isOctalUnit(c uint16) bool { return c >= '0' && c <= '7' }

func isHexUnit(c uint16) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func hexValue(c uint16) rune {
	switch {
	case c >= '0' && c <= '9':
		return rune(c - '0')
	case c >= 'a' && c <= 'f':
		return rune(c-'a') + 10
	default:
		return rune(c-'A') + 10
	}
}

// isClassControlLetter is Annex B's ClassControlLetter: DecimalDigit or "_" on
// top of the ControlLetter set the main grammar allows.
func isClassControlLetter(c uint16) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') || c == '_'
}

// isIDStartCP and isIDContinueCP are UnicodeIDStart and UnicodeIDContinue plus
// the characters ECMAScript adds to each: "$" and "_" to the first, "$" and the
// two zero-width joiners to the second.
func isIDStartCP(r rune) bool {
	if r == '$' || r == '_' {
		return true
	}
	if r < 0x80 {
		return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
	}
	if unicode.Is(unicode.Pattern_Syntax, r) || unicode.Is(unicode.Pattern_White_Space, r) {
		return false
	}
	return unicode.Is(unicode.L, r) || unicode.Is(unicode.Nl, r) ||
		unicode.Is(unicode.Other_ID_Start, r)
}

func isIDContinueCP(r rune) bool {
	if r == '$' || r == 0x200C || r == 0x200D {
		return true
	}
	if r < 0x80 {
		return r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
	}
	if unicode.Is(unicode.Pattern_Syntax, r) || unicode.Is(unicode.Pattern_White_Space, r) {
		return false
	}
	return unicode.Is(unicode.L, r) || unicode.Is(unicode.Nl, r) ||
		unicode.Is(unicode.Other_ID_Start, r) || unicode.Is(unicode.Mn, r) ||
		unicode.Is(unicode.Mc, r) || unicode.Is(unicode.Nd, r) ||
		unicode.Is(unicode.Pc, r) || unicode.Is(unicode.Other_ID_Continue, r)
}
