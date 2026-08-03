package parse

// Extglobs: !( +( *( ?( and the paren machinery underneath them.
//
// This file is a transcription of three separate regions of upstream's
// lib/parse.js, kept together because they are one construct:
//
//   - the ReDoS analysis helpers at :22-347, which decide whether a repeated
//     extglob body is "risky" and must be rewritten to a literal;
//   - extglobOpen at :523-537 and extglobClose at :539-600;
//   - constants.extglobChars at constants.js:167-175.
//
// The main loop's call sites stay in scanner.go, in upstream's order.
//
// # Everything here indexes code units
//
// Upstream's helpers mix two iteration styles: splitTopLevel and isPlainBranch
// use `for (const ch of str)`, which walks *code points*, while
// parseRepeatedExtglob, normalizeSimpleBranch and hasRepeatedCharPrefixOverlap
// use str[i] and str.length, which are *code units*. This file walks units
// throughout, which is what the second group requires and what the first group
// cannot tell apart: every character the code-point loops branch on is ASCII, so
// a surrogate half falls through to the same "append it" arm its pair does, and
// the partition is identical either way. DECISIONS.md §8.

import "errors"

// defaultMaxExtglobRecursion is constants.DEFAULT_MAX_EXTGLOB_RECURSION.
//
// It is the cap analyzeRepeatedExtglob compares against at parse.js:335, and the
// one site opts.maxExtglobRecursion reaches (parse.js:288 and :293 — a number
// caps, `false` disables the analysis outright). Options are not threaded into
// this package yet, so the site is named rather than parameterised.
const defaultMaxExtglobRecursion = 0

// extglob is one entry of upstream's `extglobs` stack — the object built at
// parse.js:524 by spreading EXTGLOB_CHARS[value] and adding the open-time
// snapshot the close needs.
type extglob struct {
	typ   string
	open  string
	close string

	// conditions counts the alternation branches, incremented by the pipe branch
	// at parse.js:948. Upstream never reads it; it is carried because upstream
	// maintains it, on the same footing as State.Globstar.
	conditions int
	// inner accumulates the value of every non-paren token pushed while this is
	// the innermost open extglob (parse.js:507-509). extglobClose reads it for
	// "/" and "*".
	inner units

	// The open-time snapshot, all set at parse.js:526-530.
	prev        *token
	parens      int
	output      units // state.output as it stood before the extglob emitted anything
	startIndex  int
	tokensIndex int
}

// --- the ReDoS analysis (parse.js:48-347) ----------------------------------

// splitTopLevel is parse.js:48-98: split on "|" at nesting depth zero, with
// backslash escapes, double quotes, brackets and parens all suppressing the
// split.
func splitTopLevel(input units) []units {
	var parts []units
	bracket, paren, quote := 0, 0, 0
	var value units
	escaped := false

	for _, ch := range input {
		if escaped {
			value = append(value, ch)
			escaped = false
			continue
		}
		if ch == '\\' {
			value = append(value, ch)
			escaped = true
			continue
		}
		if ch == '"' {
			if quote == 1 {
				quote = 0
			} else {
				quote = 1
			}
			value = append(value, ch)
			continue
		}
		if quote == 0 {
			switch {
			case ch == '[':
				bracket++
			case ch == ']' && bracket > 0:
				bracket--
			case bracket == 0:
				switch {
				case ch == '(':
					paren++
				case ch == ')' && paren > 0:
					paren--
				case ch == '|' && paren == 0:
					parts = append(parts, value)
					value = nil
					continue
				}
			}
		}
		value = append(value, ch)
	}

	return append(parts, value)
}

// isPlainBranch is parse.js:100-120: no unescaped glob metacharacter anywhere.
func isPlainBranch(branch units) bool {
	escaped := false
	for _, ch := range branch {
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		switch ch {
		case '?', '*', '+', '@', '!', '(', ')', '[', ']', '{', '}':
			return false
		}
	}
	return true
}

// isAtGroup is the /^@\([^\\()[\]{}|]+\)$/ test at parse.js:129.
//
// The class excludes ")", so the greedy "+" cannot cross the closing paren and
// the match is fully determined by the two ends plus a scan of the middle.
func isAtGroup(v units) bool {
	if len(v) < 4 || v[0] != '@' || v[1] != '(' || v[len(v)-1] != ')' {
		return false
	}
	for _, ch := range v[2 : len(v)-1] {
		switch ch {
		case '\\', '(', ')', '[', ']', '{', '}', '|':
			return false
		}
	}
	return true
}

// unescapeAny is `value.replace(/\\(.)/g, '$1')` at parse.js:139.
//
// The regexp has no `u` flag, so "." is one code unit — half a surrogate pair
// counts — and it does not match a line terminator, so a backslash before one is
// left in place.
func unescapeAny(v units) units {
	out := make(units, 0, len(v))
	for i := 0; i < len(v); i++ {
		if v[i] == '\\' && i+1 < len(v) && !isLineTerminator(v[i+1]) {
			out = append(out, v[i+1])
			i++
			continue
		}
		out = append(out, v[i])
	}
	return out
}

// normalizeSimpleBranch is parse.js:122-140. The second result is JavaScript's
// undefined-versus-string distinction; callers additionally test the result for
// truthiness, so an empty result is not the same as a usable one.
func normalizeSimpleBranch(branch units) (units, bool) {
	value := branch.trim()
	for {
		if !isAtGroup(value) {
			break
		}
		value = value[2 : len(value)-1]
	}
	if !isPlainBranch(value) {
		return nil, false
	}
	return unescapeAny(value), true
}

// truthyBranch applies JavaScript's `if (branch)` to normalizeSimpleBranch's
// result: undefined and the empty string are both falsy.
func truthyBranch(v units, ok bool) (units, bool) { return v, ok && len(v) > 0 }

// hasRepeatedCharPrefixOverlap is parse.js:142-162: two branches that are each
// a run of the same single character, where one is a prefix of the other.
func hasRepeatedCharPrefixOverlap(branches []units) bool {
	var values []units
	for _, b := range branches {
		if v, ok := truthyBranch(normalizeSimpleBranch(b)); ok {
			values = append(values, v)
		}
	}

	for i := 0; i < len(values); i++ {
		for j := i + 1; j < len(values); j++ {
			a, b := values[i], values[j]
			ch := a[0] // values are non-empty, so a[0] is never JavaScript's falsy ""
			if !isRepeatOf(a, ch) || !isRepeatOf(b, ch) {
				continue
			}
			if a.equal(b) || a.startsWith(b) || b.startsWith(a) {
				return true
			}
		}
	}
	return false
}

// isRepeatOf is `s === char.repeat(s.length)`.
func isRepeatOf(s units, ch uint16) bool {
	for _, c := range s {
		if c != ch {
			return false
		}
	}
	return true
}

// repeatedExtglob is parseRepeatedExtglob's return value at parse.js:223-227.
type repeatedExtglob struct {
	typ  uint16
	body units
	end  int
}

// parseRepeatedExtglob is parse.js:164-231: match a leading "+(...)" or "*(...)",
// optionally requiring it to span the whole pattern.
func parseRepeatedExtglob(pattern units, requireEnd bool) (repeatedExtglob, bool) {
	if len(pattern) < 2 || (pattern[0] != '+' && pattern[0] != '*') || pattern[1] != '(' {
		return repeatedExtglob{}, false
	}

	bracket, paren, quote := 0, 0, 0
	escaped := false

	for i := 1; i < len(pattern); i++ {
		ch := pattern[i]

		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == '"' {
			if quote == 1 {
				quote = 0
			} else {
				quote = 1
			}
			continue
		}
		if quote == 1 {
			continue
		}
		if ch == '[' {
			bracket++
			continue
		}
		if ch == ']' && bracket > 0 {
			bracket--
			continue
		}
		if bracket > 0 {
			continue
		}
		if ch == '(' {
			paren++
			continue
		}
		if ch == ')' {
			paren--
			if paren == 0 {
				if requireEnd && i != len(pattern)-1 {
					return repeatedExtglob{}, false
				}
				return repeatedExtglob{typ: pattern[0], body: pattern[2:i], end: i}, true
			}
		}
	}
	return repeatedExtglob{}, false
}

// buildCharClassStar is parse.js:233-239.
func buildCharClassStar(chars []units) units {
	var source units
	if len(chars) == 1 {
		source = escapeRegex(chars[0])
	} else {
		source = append(source, '[')
		for _, ch := range chars {
			source = append(source, escapeRegex(ch)...)
		}
		source = append(source, ']')
	}
	return append(source, '*')
}

// getStarExtglobSequenceChars is parse.js:241-271: a whole branch that is a
// concatenation of single-character "*(x)" groups, returning those characters.
func getStarExtglobSequenceChars(pattern units) ([]units, bool) {
	index := 0
	var chars []units

	for index < len(pattern) {
		match, ok := parseRepeatedExtglob(pattern[index:], false)
		if !ok || match.typ != '*' {
			return nil, false
		}
		branches := splitTopLevelTrimmed(match.body)
		if len(branches) != 1 {
			return nil, false
		}
		branch, ok := truthyBranch(normalizeSimpleBranch(branches[0]))
		if !ok || len(branch) != 1 {
			return nil, false
		}
		chars = append(chars, branch)
		index += match.end + 1
	}

	if len(chars) < 1 {
		return nil, false
	}
	return chars, true
}

// repeatedExtglobRecursion is parse.js:273-285: how many "+(" / "*(" wrappers
// nest directly inside one another.
func repeatedExtglobRecursion(pattern units) int {
	depth := 0
	value := pattern.trim()
	match, ok := parseRepeatedExtglob(value, true)
	for ok {
		depth++
		value = match.body.trim()
		match, ok = parseRepeatedExtglob(value, true)
	}
	return depth
}

// splitTopLevelTrimmed is `splitTopLevel(body).map(branch => branch.trim())`,
// which upstream writes at parse.js:252 and :297.
func splitTopLevelTrimmed(body units) []units {
	parts := splitTopLevel(body)
	out := make([]units, len(parts))
	for i, p := range parts {
		out[i] = p.trim()
	}
	return out
}

// extglobAnalysis is analyzeRepeatedExtglob's return value. hasSafeOutput
// carries JavaScript's presence-versus-empty distinction on `safeOutput`.
type extglobAnalysis struct {
	risky         bool
	safeOutput    units
	hasSafeOutput bool
}

// analyzeRepeatedExtglob is parse.js:287-347: decide whether a "+(...)" or
// "*(...)" body is prone to catastrophic backtracking, and if every branch
// reduces to single characters, what flat character class replaces it.
//
// opts.maxExtglobRecursion === false disables the whole analysis at :288. That
// is the third state Options.MaxExtglobRecursion's *int exists for; options do
// not reach this package yet, so the default cap is used and the site is named.
func analyzeRepeatedExtglob(body units) extglobAnalysis {
	max := defaultMaxExtglobRecursion

	branches := splitTopLevelTrimmed(body)

	if len(branches) > 1 {
		for _, b := range branches {
			if len(b) == 0 || isStarQmarkRun(b) {
				return extglobAnalysis{risky: true}
			}
		}
		if hasRepeatedCharPrefixOverlap(branches) {
			return extglobAnalysis{risky: true}
		}
	}

	var safeChars []units
	sawStarSequence := false
	combinable := true

	for _, branch := range branches {
		if chars, ok := getStarExtglobSequenceChars(branch); ok {
			sawStarSequence = true
			safeChars = append(safeChars, chars...)
			continue
		}
		if literal, ok := truthyBranch(normalizeSimpleBranch(branch)); ok && len(literal) == 1 {
			safeChars = append(safeChars, literal)
			continue
		}

		combinable = false

		if repeatedExtglobRecursion(branch) > max {
			return extglobAnalysis{risky: true}
		}
	}

	if sawStarSequence {
		if combinable {
			return extglobAnalysis{
				risky:         true,
				safeOutput:    buildCharClassStar(dedupeUnits(safeChars)),
				hasSafeOutput: true,
			}
		}
		return extglobAnalysis{risky: true}
	}

	return extglobAnalysis{}
}

// isStarQmarkRun is the /^[*?]+$/ test at parse.js:302.
func isStarQmarkRun(b units) bool {
	if len(b) == 0 {
		return false
	}
	for _, c := range b {
		if c != '*' && c != '?' {
			return false
		}
	}
	return true
}

// dedupeUnits is `[...new Set(chars)]`: first occurrence wins, order preserved.
func dedupeUnits(in []units) []units {
	seen := make(map[string]bool, len(in))
	out := make([]units, 0, len(in))
	for _, v := range in {
		k := v.key()
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, v)
	}
	return out
}

// --- open and close (parse.js:523-600) --------------------------------------

// extglobOpen is parse.js:523-537.
//
// Three things happen in an order that is easy to lose. The snapshot of
// state.output is taken *before* anything is emitted, because extglobClose's
// risky path rebuilds state.output from it. increment('parens') runs before both
// pushes. And the ONE_CHAR on the first token is chosen from state.output's
// JavaScript truthiness at the moment the object literal is built — an empty
// output, not merely an absent one.
func (s *scanner) extglobOpen(typ string, value units) {
	_, open, closing, _ := s.chars.extglobChars(value[0])

	e := &extglob{
		typ:         typ,
		open:        open,
		close:       closing,
		conditions:  1,
		prev:        s.prev,
		parens:      s.parens,
		output:      s.output.clone(),
		startIndex:  s.index,
		tokensIndex: len(s.tokens),
	}

	// parse.js:531. opts.capture prepends "(".
	output := encode(e.open)

	s.increment("parens")

	first := units{}
	if len(s.output) == 0 {
		first = encode(s.chars.oneChar)
	}
	s.push(&token{typ: typ, value: value, output: &first})
	if s.err != nil {
		return
	}
	// advance() is the "(" that the call site's peek already established.
	s.push(&token{typ: "paren", extglob: true, value: units{s.advance()}, output: &output})
	if s.err != nil {
		return
	}

	s.extglobs = append(s.extglobs, e)
}

// extglobClose is parse.js:539-600.
func (s *scanner) extglobClose(e *extglob, value units) error {
	// Both slices alias s.input, which is never written; anything stored on a
	// token is cloned, because push() appends to token values in place.
	literal := s.input[e.startIndex : s.index+1]
	body := s.input[e.startIndex+2 : s.index]
	analysis := analyzeRepeatedExtglob(body)

	// parse.js:544-566. A repeated extglob whose body can blow up is un-emitted:
	// the opening token becomes literal text, every token after it is blanked,
	// and state.output is rebuilt from the open-time snapshot.
	if (e.typ == "plus" || e.typ == "star") && analysis.risky {
		var openOutput units
		if analysis.hasSafeOutput {
			// parse.js:546. token.output is the snapshot, tested for JavaScript
			// truthiness — an empty snapshot takes the ONE_CHAR.
			if len(e.output) == 0 {
				openOutput = append(openOutput, encode(s.chars.oneChar)...)
			}
			openOutput = append(openOutput, analysis.safeOutput...) // opts.capture wraps this in a group
		} else {
			openOutput = escapeRegex(literal)
		}

		open := s.tokens[e.tokensIndex]
		open.typ = "text"
		open.value = literal.clone()
		open.output = &openOutput

		for i := e.tokensIndex + 1; i < len(s.tokens); i++ {
			s.tokens[i].value = units{}
			blank := units{}
			s.tokens[i].output = &blank
			s.tokens[i].suffix = nil
		}

		s.output = append(e.output.clone(), openOutput...)
		s.backtrack = true

		empty := units{}
		s.push(&token{typ: "paren", extglob: true, value: value, output: &empty})
		if s.err != nil {
			return s.err
		}
		s.decrement("parens")
		return nil
	}

	output := encode(e.close) // opts.capture appends ")"

	if e.typ == "negate" {
		// parse.js:572-580. extglobStar is compared against `star` by value, so
		// the test at :578 is "did the body force a globstar body".
		extglobStar := s.star
		if len(e.inner) > 1 && e.inner.contains('/') {
			extglobStar = s.globstarBody
		}

		if extglobStar != s.star || s.eos() || isAllCloseParens(s.remaining()) {
			output = encode(`)$))` + extglobStar)
		}

		// parse.js:582-591. A non-magical suffix such as ".ts" after the closing
		// paren is parsed on its own and spliced into the close.
		if e.inner.contains('*') {
			if rest := s.remaining(); len(rest) > 0 && isDotSuffix(rest) {
				inner, err := parseSuffix(rest, s.opts)
				if err != nil {
					var u *UnsupportedError
					if errors.As(err, &u) {
						// The expression is part of this token's output, so
						// there is no prefix to hand back that is merely short
						// rather than wrong. DECISIONS.md §9.
						return &UnsupportedError{Construct: u.Construct, Site: u.Site, Index: s.index}
					}
					return err
				}
				output = append(append(encode(`)`), inner...), encode(`)`+extglobStar+`)`)...)
			}
		}

		if e.prev.typ == "bos" {
			s.negatedExtglob = true
		}
	}

	s.push(&token{typ: "paren", extglob: true, value: value, output: &output})
	if s.err != nil {
		return s.err
	}
	s.decrement("parens")
	return nil
}

// isAllCloseParens is the /^\)+$/ test at parse.js:578.
func isAllCloseParens(rest units) bool {
	if len(rest) == 0 {
		return false
	}
	for _, c := range rest {
		if c != ')' {
			return false
		}
	}
	return true
}

// isDotSuffix is the /^\.[^\\/.]+$/ test at parse.js:582.
func isDotSuffix(rest units) bool {
	if len(rest) < 2 || rest[0] != '.' {
		return false
	}
	for _, c := range rest[1:] {
		switch c {
		case '\\', '/', '.':
			return false
		}
	}
	return true
}

// parseSuffix is the recursive `parse(rest, { ...options, fastpaths: false })`
// at parse.js:588, returning only the .output the caller reads.
//
// It runs on units rather than round-tripping through a Go string: units.String
// folds an unpaired surrogate to U+FFFD, and the suffix is not this scanner's to
// launder. DECISIONS.md §10.
//
// The two things newScanner does that this skips cannot fire here. REPLACEMENTS
// is keyed on "***", "**/**" and "**/**/**", and isDotSuffix has already
// established that rest begins with "."; and the maxLength guard has already
// passed for the whole input, of which rest is a suffix.
func parseSuffix(rest units, opts Options) (units, error) {
	sub := newScannerUnits(rest.clone(), opts)
	if err := sub.run(); err != nil {
		return nil, err
	}
	return sub.output, nil
}
