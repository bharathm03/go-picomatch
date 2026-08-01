package parse

// The scanner is a transcription of upstream's parse() main loop
// (tests/original/lib/parse.js:356-1322), branch for branch and in the same
// order. Line references in comments point at that file.
//
// # Options
//
// [Parse] takes no options yet, so every opts.X read in upstream resolves to its
// default here: dot, bash, capture, posix, strictBrackets, strictSlashes,
// nobrace, nobracket, noextglob, nonegate, unescape, keepQuotes and regex are all
// unset. Branches those keys select are marked with the key that will pick them
// once options are threaded through, so the sites are findable rather than
// silently baked in.
//
// # What is not built yet
//
// Constructs the scanner has not reached return an [UnsupportedError] naming the
// construct and the upstream site. They are not approximated as text — a wrong
// token stream scores as a pass only if it happens to match, and treating an
// unbuilt construct as literal text is exactly the kind of near-miss that would.
// DECISIONS.md §9.
//
// # Before adding a branch
//
// docs/transcription-traps.md lists the places where the obvious Go reading of
// parse.js is wrong — the "!" fallthrough, the JavaScript-truthy text merge, and
// the rest. Read it first, and add to it when a new branch turns one up.

// Platform constants from constants.js. These are the POSIX set; the Windows set
// (SLASH_LITERAL "[\\/]" and friends) arrives with Options.Windows.
const (
	dotLiteral   = `\.`
	slashLiteral = `\/`
	plusLiteral  = `\+`
)

// maxLength is constants.MAX_LENGTH, 1024 * 64.
const maxLength = 1024 * 64

// replacements is constants.REPLACEMENTS: whole-input rewrites applied before
// anything else looks at the pattern.
var replacements = map[string]string{
	"***":      "*",
	"**/**":    "**",
	"**/**/**": "**",
}

// token is the mutable form of [Token]. Upstream rewrites tokens after pushing
// them — prev.value += value, and the extglob and POSIX paths rewrite earlier
// entries outright — so the scanner holds pointers and exports values at the end.
type token struct {
	typ    string
	value  units
	output *units

	extglob bool
	posix   bool
	comma   bool
	star    bool

	outputIndex *int
	tokensIndex *int
	suffix      *units

	prev *token
}

type scanner struct {
	input    units
	index    int
	start    int
	consumed units
	output   units
	prefix   string

	backtrack bool
	negated   bool

	brackets int
	braces   int
	parens   int
	quotes   int

	tokens []*token
	bos    *token
	prev   *token

	err error
}

func newScanner(pattern string) (*scanner, error) {
	if r, ok := replacements[pattern]; ok {
		pattern = r
	}

	// parse.js:364-369. The comparison is in code units; see units.go. Upstream
	// reads input.length in O(1) and throws before touching the string, so the
	// count runs before the conversion here too — otherwise the allocation the
	// guard exists to bound happens first. A pattern is never shorter in bytes
	// than in code units, so the byte length is a sound cheap filter.
	if len(pattern) > maxLength {
		if n := countUnits(pattern); n > maxLength {
			return nil, &LengthError{Length: n, Max: maxLength}
		}
	}
	input := encode(pattern)

	// parse.js:371. bos carries output "" rather than no output — opts.prepend
	// defaults to the empty string, and the recording shows the field present.
	empty := units{}
	bos := &token{typ: "bos", value: units{}, output: &empty}

	s := &scanner{index: -1, tokens: []*token{bos}, bos: bos, prev: bos}

	// parse.js:430, utils.removePrefix. This runs before the loop, so the rest
	// of the scanner never sees a leading "./" and state.consumed is not a
	// prefix of the input the caller passed.
	if input.hasPrefix("./") {
		input = input[2:]
		s.prefix = "./"
	}
	s.input = input
	return s, nil
}

// --- tokenizing helpers (parse.js:443-455) ---------------------------------

// eos is upstream's `state.index === input.length - 1` widened to >=.
//
// The widening matters. Upstream's equality can be stepped over — the
// backslash-run collapse at parse.js:689-699 advances the index past the end —
// and once it is, eos() is never true again and parse() loops forever. That is
// not a hypothetical: `a\\\\` (four trailing backslashes) hangs node. This
// package detects the overshoot and errors, and >= makes the loop bounded even
// if some future branch finds another way to step over it. DECISIONS.md §11.
func (s *scanner) eos() bool { return s.index >= len(s.input)-1 }

// peek returns the unit n positions ahead, and whether it exists. JavaScript's
// input[i] yields undefined past the end, which compares unequal to everything;
// the bool carries that instead of a sentinel.
func (s *scanner) peek(n int) (uint16, bool) {
	i := s.index + n
	if i < 0 || i >= len(s.input) {
		return 0, false
	}
	return s.input[i], true
}

func (s *scanner) peekIs(n int, c uint16) bool {
	got, ok := s.peek(n)
	return ok && got == c
}

// advance steps to the next unit and returns it. Every call site must already
// have established that the unit exists — the loop's own !eos() test, a peek
// that returned ok, or the end-of-input check in the escape branch — because
// upstream's `input[++state.index] || ""` yields the empty string past the end
// and appends nothing, which no uint16 can stand for. The 0 below is a
// last-resort value for a case none of the guards allow, not a transcription of
// that fallback: a one-character string holding U+0000 is truthy in JavaScript,
// so upstream's fallback fires on undefined alone.
func (s *scanner) advance() uint16 {
	s.index++
	if s.index >= len(s.input) {
		return 0
	}
	return s.input[s.index]
}

func (s *scanner) remaining() units {
	if s.index+1 >= len(s.input) {
		return nil
	}
	return s.input[s.index+1:]
}

// consume is upstream's consume(value, num) at parse.js:447-450, where both
// parameters default. num advances the index as well, which is easy to drop
// because eight of upstream's nine call sites leave it defaulted — the ninth is
// consume("/**", 3) in the globstar branch (parse.js:1175), which is not built
// yet. The parameter is carried here so that branch cannot be written against a
// helper that silently does half the job.
func (s *scanner) consume(value units, num int) {
	s.consumed = append(s.consumed, value...)
	s.index += num
}

// emit is upstream's append(): the output side of a token, without the token
// bookkeeping. parse.js:452-455.
func (s *scanner) emit(t *token) {
	if t.output != nil {
		s.output = append(s.output, *t.output...)
	} else {
		s.output = append(s.output, t.value...)
	}
	s.consume(t.value, 0)
}

// negate handles a run of leading "!". parse.js:457-473.
//
// Only an odd run negates; an even one cancels out and leaves state.negated
// false. Either way the whole run is consumed and state.start advances past it,
// which is why "!!!!abc" records consumed "abc".
func (s *scanner) negate() {
	count := 1
	for s.peekIs(1, '!') && (!s.peekIs(2, '(') || s.peekIs(3, '?')) {
		s.advance()
		s.start++
		count++
	}
	if count%2 == 0 {
		return
	}
	s.negated = true
	s.start++
}

// push adds a token, merging consecutive text. parse.js:493-521.
func (s *scanner) push(t *token) {
	// parse.js:494-505 rewrites a preceding globstar back to a star. Not
	// transcribed: no branch here produces a globstar token yet, so the code
	// would be unreachable and untested. The star branch adds it.
	if s.prev != nil && s.prev.typ == "globstar" {
		s.fail(&UnsupportedError{Construct: "globstar lookbehind", Site: "parse.js:494", Index: s.index})
		return
	}
	// parse.js:507-509 accumulates into the innermost extglob. Unreachable
	// while "(" is unsupported.

	// parse.js:511 — JavaScript truthiness: an empty value and an empty output
	// both count as absent here.
	if len(t.value) > 0 || (t.output != nil && len(*t.output) > 0) {
		s.emit(t)
	}

	// parse.js:512-516. Note the merge reads prev.output only when it is
	// non-empty; an output of "" falls back to the value.
	//
	// Both fields grow by appending in place. The obvious clone-then-append
	// reads more safely and is quadratic: a merge costs a copy of everything
	// merged so far, so a 65,536-unit literal — exactly maxLength, and therefore
	// legal input — takes seconds. The one copy that is required is the first
	// seed of output from value, because the two fields must not end up sharing
	// a backing array: they are appended to independently from here on, and the
	// second append would write over the first field's tail.
	if s.prev != nil && s.prev.typ == "text" && t.typ == "text" {
		if s.prev.output != nil && len(*s.prev.output) > 0 {
			merged := append(*s.prev.output, t.value...)
			s.prev.output = &merged
		} else {
			merged := s.prev.value.clone().appendUnits(t.value)
			s.prev.output = &merged
		}
		s.prev.value = append(s.prev.value, t.value...)
		return
	}

	t.prev = s.prev
	s.tokens = append(s.tokens, t)
	s.prev = t
}

func (u units) appendUnits(v units) units { return append(u, v...) }

func (s *scanner) fail(err error) {
	if s.err == nil {
		s.err = err
	}
}

func (s *scanner) unsupported(construct, site string) error {
	return &UnsupportedError{Construct: construct, Site: site, Index: s.index}
}

func out(s string) *units {
	u := encode(s)
	return &u
}

// --- the main loop (parse.js:661-1284) -------------------------------------

func (s *scanner) run() error {
	for !s.eos() {
		c := s.advance()
		value := units{c}

		if c == 0 { // parse.js:664, 'U+0000'
			continue
		}

		// Escaped characters. parse.js:672-711.
		if c == '\\' {
			next, ok := s.peek(1)

			if ok && next == '/' { // opts.bash
				continue
			}
			if ok && (next == '.' || next == ';') {
				continue
			}
			if !ok {
				value = append(value, '\\')
				s.push(&token{typ: "text", value: value})
				continue
			}

			// Collapse runs of backslashes. parse.js:689-699.
			rem := s.remaining()
			run := 0
			for run < len(rem) && rem[run] == '\\' {
				run++
			}
			if run > 2 {
				s.index += run
				if run%2 != 0 {
					value = append(value, '\\')
				}
			}

			// The collapse is the one place upstream steps its index past the
			// end, and it is where parse() stops terminating: eos() is an
			// equality, so once the index is past length-1 it is never true
			// again. Reproduced against node — `a` followed by four or more
			// backslashes never returns, while three or fewer do. There is no
			// recorded behaviour to be faithful to and no fixture can ever hold
			// one, so this refuses rather than inventing an answer upstream does
			// not give. DECISIONS.md §11.
			if s.index+1 >= len(s.input) {
				return &NonTerminatingError{Site: "parse.js:689", Index: s.index}
			}

			value = append(value, s.advance()) // opts.unescape replaces rather than appends

			if s.brackets == 0 {
				s.push(&token{typ: "text", value: value})
				if s.err != nil {
					return s.err
				}
				continue
			}
			return s.unsupported(`\ inside a character class`, "parse.js:707")
		}

		// Character class body. parse.js:718-758. Unreachable while "[" is
		// unsupported, since nothing else increments state.brackets.
		if s.brackets > 0 {
			return s.unsupported("character class body", "parse.js:718")
		}

		// Quoted string body. parse.js:765-770.
		if s.quotes == 1 && c != '"' {
			value = escapeRegex(value)
			s.prev.value = append(s.prev.value, value...) // see push() on why not clone
			s.emit(&token{value: value})
			continue
		}

		// Double quotes. parse.js:776-782.
		if c == '"' {
			if s.quotes == 1 {
				s.quotes = 0
			} else {
				s.quotes = 1
			}
			continue // opts.keepQuotes pushes a text token here
		}

		// Parentheses. parse.js:788-808.
		if c == '(' {
			return s.unsupported("(", "parse.js:788")
		}
		if c == ')' {
			return s.unsupported(")", "parse.js:794")
		}

		// Square brackets. parse.js:814-875.
		if c == '[' {
			return s.unsupported("[", "parse.js:814")
		}
		if c == ']' {
			// prev.type == "bracket" is unreachable while "[" is unsupported,
			// so this is always the state.brackets == 0 arm at parse.js:835.
			if s.brackets == 0 {
				s.push(&token{typ: "text", value: value, output: out(`\]`)})
				if s.err != nil {
					return s.err
				}
				continue
			}
			return s.unsupported("]", "parse.js:844")
		}

		// Braces. parse.js:881-940.
		if c == '{' {
			return s.unsupported("{", "parse.js:881")
		}
		if c == '}' {
			// Always the "no open brace" arm at parse.js:900 while "{" is
			// unsupported. Note the output is the literal "}", not an escape.
			s.push(&token{typ: "text", value: value, output: out("}")})
			if s.err != nil {
				return s.err
			}
			continue
		}

		// Pipes. parse.js:946-952.
		if c == '|' {
			s.push(&token{typ: "text", value: value})
			if s.err != nil {
				return s.err
			}
			continue
		}

		// Commas. parse.js:958-969. Outside braces the output is the comma
		// itself; inside, it becomes the alternation bar.
		if c == ',' {
			s.push(&token{typ: "comma", value: value, output: out(",")})
			if s.err != nil {
				return s.err
			}
			continue
		}

		// Slashes. parse.js:975-991.
		if c == '/' {
			// A leading "./" that survived removePrefix — it can only get here
			// behind a negation, as in "!./foo". The dot token is discarded and
			// the scanner restarts its bookkeeping from the character after it.
			if s.prev.typ == "dot" && s.index == s.start+1 {
				s.start = s.index + 1
				s.consumed = nil
				s.output = nil
				s.tokens = s.tokens[:len(s.tokens)-1]
				s.prev = s.bos
				continue
			}

			s.push(&token{typ: "slash", value: value, output: out(slashLiteral)})
			if s.err != nil {
				return s.err
			}
			continue
		}

		// Dots. parse.js:997-1015.
		if c == '.' {
			// parse.js:998-1006 handles ".." inside braces. Unreachable while
			// "{" is unsupported.
			if s.braces+s.parens == 0 && s.prev.typ != "bos" && s.prev.typ != "slash" {
				s.push(&token{typ: "text", value: value, output: out(dotLiteral)})
				if s.err != nil {
					return s.err
				}
				continue
			}
			s.push(&token{typ: "dot", value: value, output: out(dotLiteral)})
			if s.err != nil {
				return s.err
			}
			continue
		}

		// Question marks. parse.js:1021-1048.
		if c == '?' {
			return s.unsupported("?", "parse.js:1021")
		}

		// Exclamation. parse.js:1053-1065. Neither arm is a fallthrough guard:
		// a "!" that is neither an extglob opener nor at index 0 drops out of
		// this branch and is picked up as plain text below.
		if c == '!' {
			if s.peekIs(1, '(') { // opts.noextglob
				if !s.peekIs(2, '?') || !isLookaroundIntro(s.peek(3)) {
					return s.unsupported("!( extglob", "parse.js:1056")
				}
			}
			if s.index == 0 { // opts.nonegate
				s.negate()
				continue
			}
		}

		// Plus. parse.js:1071-1089.
		if c == '+' {
			if s.peekIs(1, '(') && !s.peekIs(2, '?') { // opts.noextglob
				return s.unsupported("+( extglob", "parse.js:1073")
			}
			// The two arms at parse.js:1077 and :1082 need a preceding paren,
			// bracket or brace token, none of which exist yet. This is the
			// bare-plus arm at :1087 — note the escape lands in value, and the
			// token has no output at all.
			s.push(&token{typ: "plus", value: encode(plusLiteral)})
			if s.err != nil {
				return s.err
			}
			continue
		}

		// At. parse.js:1095-1103.
		if c == '@' {
			if s.peekIs(1, '(') && !s.peekIs(2, '?') { // opts.noextglob
				s.push(&token{typ: "at", extglob: true, value: value, output: out("")})
				if s.err != nil {
					return s.err
				}
				continue
			}
			s.push(&token{typ: "text", value: value})
			if s.err != nil {
				return s.err
			}
			continue
		}

		// Plain text. parse.js:1109-1122.
		if c != '*' {
			if c == '$' || c == '^' {
				value = units{'\\', c}
			}
			if rem := s.remaining(); len(rem) > 0 {
				if n := nonSpecialRun(rem); n > 0 {
					value = append(value, rem[:n]...)
					s.index += n
				}
			}
			s.push(&token{typ: "text", value: value})
			if s.err != nil {
				return s.err
			}
			continue
		}

		// Stars. parse.js:1128-1283.
		return s.unsupported("*", "parse.js:1128")
	}

	if s.err != nil {
		return s.err
	}

	// Unclosed brackets, parens and braces. parse.js:1286-1302. All three
	// counters are pinned at zero while their opening characters are
	// unsupported, so escapeLast has nothing to rewrite yet.

	// parse.js:1304-1306, opts.strictSlashes. Unreachable until the star and
	// bracket branches land, which are the only producers of those types.
	if s.prev.typ == "star" || s.prev.typ == "bracket" {
		s.push(&token{typ: "maybe_slash", value: units{}, output: out(slashLiteral + "?")})
		if s.err != nil {
			return s.err
		}
	}

	// Rebuild the output from the tokens if anything was rewritten after being
	// emitted. parse.js:1309-1319. This is the site that makes the token array
	// authoritative and the output string a cache; see the package doc.
	if s.backtrack {
		s.output = nil
		for _, t := range s.tokens {
			if t.output != nil {
				s.output = append(s.output, *t.output...)
			} else {
				s.output = append(s.output, t.value...)
			}
			if t.suffix != nil {
				s.output = append(s.output, *t.suffix...)
			}
		}
	}

	return nil
}

// isLookaroundIntro is the /[!=<:]/ test at parse.js:1055, which distinguishes
// "!(?!" and friends from a plain "!(?".
func isLookaroundIntro(c uint16, ok bool) bool {
	if !ok {
		return false
	}
	switch c {
	case '!', '=', '<', ':':
		return true
	}
	return false
}

func (s *scanner) export() *State {
	st := &State{
		Consumed:  s.consumed.String(),
		Output:    s.output.String(),
		Negated:   s.negated,
		Backtrack: s.backtrack,
		Tokens:    make([]Token, 0, len(s.tokens)),
	}
	for _, t := range s.tokens {
		st.Tokens = append(st.Tokens, t.export())
	}
	return st
}

func (t *token) export() Token {
	e := Token{
		Type:    t.typ,
		Value:   t.value.String(),
		Extglob: t.extglob,
		Posix:   t.posix,
		Comma:   t.comma,
		Star:    t.star,
	}
	if t.output != nil {
		v := t.output.String()
		e.Output = &v
	}
	if t.outputIndex != nil {
		v := *t.outputIndex
		e.OutputIndex = &v
	}
	if t.tokensIndex != nil {
		v := *t.tokensIndex
		e.TokensIndex = &v
	}
	return e
}
