package parse

// The scanner is a transcription of upstream's parse() main loop
// (tests/original/lib/parse.js:356-1322), branch for branch and in the same
// order. Line references in comments point at that file.
//
// # Options
//
// [Options] carries the keys this package answers, and [Parse] takes it. Today
// that is `windows` — a table swap rather than a branch, so it lands as
// chars.go rather than as anything in this file — `bash`, four real branches
// at parse.js:401, :675, :1156 and :1248, `strictSlashes`, two branches at
// :1193 and :1304, and `dot`, which reshapes the globstarBody and nodot
// bindings themselves (:396, :399) and gates two more sites, :1041 and :1270.
//
// Every other opts.X read in upstream still resolves to its default here:
// capture, posix, strictBrackets, nobrace, nobracket, noextglob, noglobstar,
// nonegate, unescape, keepQuotes, regex, literalBrackets, maxExtglobRecursion,
// prepend and expandRange are all unset. Branches those keys select are marked
// with the key that will pick them, so the sites are findable rather than
// silently baked in. `grep -n "opts\." internal/parse/*.go` is the list, and
// [Options] says why a marked key does not get a field until its branch is
// written.
//
// # What is not built yet
//
// Every branch of upstream's loop is written; the brace branch was the last, so
// no default-options input reaches an [UnsupportedError] any more. What is left
// is the option surface — each opts.X branch the defaults do not take is marked
// at its site with the key that selects it.
//
// The rule the markers replace still stands for whoever writes them. A construct
// this package cannot answer returns an error naming the upstream site; it is
// never approximated as text, because a wrong token stream scores as a pass
// wherever it happens to match. DECISIONS.md §9.
//
// # Retroactive rewrites
//
// Six sites edit a token after it has been pushed and emitted, and all but two
// also have to un-write what state.output already holds: starGuard
// (parse.js:1263-1281), push()'s globstar lookbehind (:494-505), the globstar
// arms (:1188-1243), which reach two tokens back, extglobClose's risky path
// (:544-566), which reaches back to the extglob's opening token and blanks
// everything after it, the POSIX-class rewrite (:730-736), which reaches all the
// way to bos and leaves state.output alone because it sets state.backtrack, and
// the brace close (:907-934), which either pops every token back to the opening
// brace or rewrites both delimiters and replays state.output from the brace's own
// outputIndex. state.backtrack is *not* the general mechanism — it is set at four
// sites, :561, :731, :922 and :1133, and the globstar arms deliberately leave it
// alone while keeping state.output in step by hand.
// docs/transcription-traps.md #19.
//
// The bos rewrite is the deepest in the file and needs no special handling for
// the same reason it needs no backtrack bookkeeping: no construct can decline
// between the bracket token being pushed and the rewrite, because nothing is
// unbuilt while state.brackets is nonzero. DECISIONS.md §9.
//
// The fourth and the sixth are the two whose firing is not decided until a later
// character, and both used to be a problem for what a *declined* parse could hand
// back. Neither is now: with nothing unbuilt the scanner cannot stop while either
// rewrite is pending. DECISIONS.md §14.
//
// # Before adding a branch
//
// docs/transcription-traps.md lists the places where the obvious Go reading of
// parse.js is wrong — the "!" fallthrough, the JavaScript-truthy text merge, and
// the rest. Read it first, and add to it when a new branch turns one up.

// The platform constants moved to chars.go when Options.Windows was threaded
// through: they are a table selected per parse (constants.globChars at
// constants.js:181), not package constants, and live on the scanner as s.chars.
//
// capture is not among them. It is the binding at parse.js:374 —
// opts.capture ? "" : "?:" — and depends on no platform value, so it stays a
// constant until Options.Capture is written.
const capture = `?:`

// maxLength is constants.MAX_LENGTH, 1024 * 64.
const maxLength = 1024 * 64

// replacements is constants.REPLACEMENTS: whole-input rewrites applied before
// anything else looks at the pattern.
var replacements = map[string]string{
	"***":      "*",
	"**/**":    "**",
	"**/**/**": "**",
}

// posixRegexSource is constants.POSIX_REGEX_SOURCE, the [[:name:]] classes.
//
// Upstream declares it with `__proto__: null`, so the lookup at parse.js:728 has
// no inherited keys to fall back on — POSIX_REGEX_SOURCE["constructor"] is
// undefined rather than a function. A Go map has that property already; the
// prototype-less declaration is upstream guarding against the same thing, not a
// behaviour a plain map would miss.
var posixRegexSource = map[string]string{
	"alnum":  "a-zA-Z0-9",
	"alpha":  "a-zA-Z",
	"ascii":  `\x00-\x7F`,
	"blank":  " \\t",
	"cntrl":  `\x00-\x1F\x7F`,
	"digit":  "0-9",
	"graph":  `\x21-\x7E`,
	"lower":  "a-z",
	"print":  `\x20-\x7E `,
	"punct":  `\-!"#$%&'()\*+,./:;<=>?@[\]^_` + "`" + `{|}~`,
	"space":  " \\t\\r\\n\\v\\f",
	"upper":  "A-Z",
	"word":   "A-Za-z0-9_",
	"xdigit": "A-Fa-f0-9",
}

// posixSource is the lookup at parse.js:728, keyed exactly.
//
// The name is compared as code units rather than through units.String: that
// conversion folds an unpaired surrogate to U+FFFD (DECISIONS.md §10), and a map
// inside this package has no reason to inherit the loss. Every key is ASCII, so
// anything holding a unit above 0x7F cannot be one and is rejected without
// converting at all.
func posixSource(name units) (string, bool) {
	b := make([]byte, len(name))
	for i, c := range name {
		if c > 0x7F {
			return "", false
		}
		b[i] = byte(c)
	}
	// parse.js:729 tests `if (posix)`, so an empty source would be skipped as
	// falsy. None of the fourteen is empty; the test is kept anyway because it
	// is the shape upstream wrote.
	src, ok := posixRegexSource[string(b)]
	return src, ok && src != ""
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

	// dots marks a brace token whose body contained "..", set by the dots arm at
	// parse.js:1004 on the open brace rather than on the dots token itself.
	//
	// It is not exported. Every other field on [Token] appears in
	// testdata/tokens/summary.json under "tokenFields" and this one does not,
	// because the brace it is set on is popped by the "}" arm before the parse
	// ends — the only way it survives is an unclosed "{a..b", which no corpus
	// pattern has. suffix is unexported for the same reason.
	dots bool

	prev *token
}

type scanner struct {
	input    units
	index    int
	start    int
	consumed units
	output   units
	prefix   string

	// opts is kept only so parse.js:588 can recurse with the same configuration.
	// Every other read of an option happens once, before the loop, into the
	// bindings below; reaching for s.opts anywhere else would be reading a key at
	// a site upstream does not read it.
	opts Options

	// chars is PLATFORM_CHARS (parse.js:377) — the constants themselves.
	chars globChars

	// star, nodot and globstarBody are the bindings parse.js derives from those
	// constants at :395-405, before the loop starts. They are fields rather than
	// reads of chars because each is a different option's one point of entry:
	//
	//	globstarBody  :395  opts.dot selects DOTS_SLASH over DOT_LITERAL
	//	nodot         :399  opts.dot selects "" — not NO_DOTS; :1353 is fastpaths
	//	star          :401  opts.bash selects globstar(opts); opts.capture groups it
	//
	// The fourth binding, `qmarkNoDot` at :400, is deliberately absent. It reads
	// as the obvious partner of the "?" branch and is not: :1041 emits the
	// *constant* QMARK_NO_DOT, guarded by its own `opts.dot !== true`, and the
	// binding's only reader in the whole file is :620, inside the inline fast
	// path this package does not have. A field for it would be dead today and
	// would invite the "?" branch to read it tomorrow, which is a different
	// answer under opts.dot: the binding yields QMARK where the branch does not
	// run at all. `grep -n "qmarkNoDot\|QMARK_NO_DOT" tests/original/lib/parse.js`.
	star         string
	nodot        string
	globstarBody string

	// noextglob, posixClasses, posixNegate, regexFalse and regexTrue are the
	// same idea for the three keys whose sites are scattered through the loop
	// rather than folded into a binding upstream. They are resolved here for the
	// same reason `opts` says not to reach for it mid-loop: noextglob is a merge
	// (parse.js:408, which upstream performs once, before the loop, by mutating
	// opts), and posix and regex are each read twice with *different* tests, so
	// naming the two answers separately is what keeps a site from picking the
	// wrong default.
	noextglob    bool
	posixClasses bool // opts.posix !== false, parse.js:719
	posixNegate  bool // opts.posix === true,  parse.js:751
	regexFalse   bool // opts.regex === false, parse.js:1077
	regexTrue    bool // opts.regex === true,  parse.js:1257

	backtrack      bool
	negated        bool
	globstar       bool
	negatedExtglob bool

	brackets int
	braces   int
	parens   int
	quotes   int

	// stack is upstream's `stack` array (parse.js:435), maintained by
	// increment/decrement alongside the counters. Its one reader is the comma
	// branch at :962, which needs the *innermost* open construct to be a brace —
	// state.braces alone would also count a brace two levels out.
	stack []string
	// extglobs is upstream's `extglobs` array (parse.js:433), the stack of open
	// !( +( *( ?( constructs.
	extglobs []*extglob
	// braceStack is upstream's `braces` array (parse.js:434), the stack of open
	// "{" *tokens*. It holds the tokens themselves rather than a parallel record
	// because upstream stores the brace's state on the token — comma at :963,
	// dots at :1004, outputIndex and tokensIndex at :888-889 — and the "}" arm
	// reads all four back off the same object it pushed.
	braceStack []*token

	tokens []*token
	bos    *token
	prev   *token

	err error
}

func newScanner(pattern string, opts Options) (*scanner, error) {
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
	return newScannerUnits(encode(pattern), opts), nil
}

// newScannerUnits is everything parse() does between the length guard and the
// loop. It is separate from newScanner because parse() calls itself at
// parse.js:588 with a slice of its own input, which is already units — see
// parseSuffix on why the two skipped steps cannot fire there.
func newScannerUnits(input units, opts Options) *scanner {
	// parse.js:371. bos carries output "" rather than no output — opts.prepend
	// defaults to the empty string, and the recording shows the field present.
	empty := units{}
	bos := &token{typ: "bos", value: units{}, output: &empty}

	s := &scanner{index: -1, tokens: []*token{bos}, bos: bos, prev: bos}

	// parse.js:377 and :395-405. The table first, then the four bindings taken
	// off it — in upstream's order, because globstarBody reads two of the
	// constants and star would read globstarBody once opts.bash is written.
	s.opts = opts
	s.chars = globCharsFor(opts.Windows)
	// parse.js:396. opts.dot selects DOTS_SLASH over DOT_LITERAL inside the
	// globstar body — every globstar arm reads s.globstarBody, so this one
	// binding is what opts.dot changes for all of them at once.
	dotArm := s.chars.dotLiteral
	if opts.Dot {
		dotArm = s.chars.dotsSlash
	}
	s.globstarBody = `(` + capture + `(?:(?!` + s.chars.startAnchor + dotArm + `).)*?)`
	// parse.js:399. opts.dot selects "" over NO_DOT.
	s.nodot = s.chars.noDot
	if opts.Dot {
		s.nodot = ""
	}
	s.star = s.chars.star
	if opts.Bash {
		// parse.js:401. globstar(opts) is exactly s.globstarBody: the closure
		// reads the same capture/PLATFORM_CHARS/opts.dot this field was built
		// from two lines up, called with the same opts. opts.capture would wrap
		// it in a group at parse.js:403-405, but Capture has no field yet, so
		// that half stays unbuilt.
		s.star = s.globstarBody
	}

	// parse.js:408-410. The minimatch spelling wins only when it is a boolean,
	// so `noext: false` cancels `noextglob: true` while an absent noext leaves
	// noextglob alone. Upstream writes the answer back onto opts, which is why
	// every one of the five reader sites can spell the test `opts.noextglob`.
	s.noextglob = opts.NoExtglob
	if opts.NoExt != nil {
		s.noextglob = *opts.NoExt
	}
	// parse.js:719 and :751. One key, two tests, two different defaults: an
	// unset posix takes the class rewrite and not the "[!" rewrite.
	s.posixClasses = opts.Posix == nil || *opts.Posix
	s.posixNegate = opts.Posix != nil && *opts.Posix
	// parse.js:1077 and :1257, the same shape again.
	s.regexFalse = opts.Regex != nil && !*opts.Regex
	s.regexTrue = opts.Regex != nil && *opts.Regex

	// parse.js:430, utils.removePrefix. This runs before the loop, so the rest
	// of the scanner never sees a leading "./" and state.consumed is not a
	// prefix of the input the caller passed.
	if input.hasPrefix("./") {
		input = input[2:]
		s.prefix = "./"
	}
	s.input = input
	return s
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

// increment and decrement are parse.js:475-483. The counter and the stack move
// together, and decrement is called unguarded from the ")" branch at :806 —
// state.parens goes negative there, and JavaScript's [].pop() on the empty stack
// is a no-op rather than an error.
func (s *scanner) increment(typ string) {
	switch typ {
	case "brackets":
		s.brackets++
	case "braces":
		s.braces++
	case "parens":
		s.parens++
	}
	s.stack = append(s.stack, typ)
}

func (s *scanner) decrement(typ string) {
	switch typ {
	case "brackets":
		s.brackets--
	case "braces":
		s.braces--
	case "parens":
		s.parens--
	}
	if len(s.stack) > 0 {
		s.stack = s.stack[:len(s.stack)-1]
	}
}

// push adds a token, merging consecutive text. parse.js:493-521.
func (s *scanner) push(t *token) {
	// parse.js:494-505. A globstar is only a globstar until something that is
	// not a separator follows it: pushing anything else rewrites it back into a
	// plain star and un-emits the globstar body from state.output. This is the
	// port's first retroactive rewrite that also *shrinks* a token — prev.value
	// is assigned "*", not appended to, so a token that had grown to "**" goes
	// back to one character while state.consumed keeps both. Trap #12.
	if s.prev != nil && s.prev.typ == "globstar" {
		// parse.js:495. increment("braces") runs before the "{" arm's push, so a
		// brace token following a globstar always finds the counter already
		// raised and is exempted from the rewrite.
		isBrace := s.braces > 0 && (t.typ == "comma" || t.typ == "brace")
		// parse.js:496. "pipe" is a type upstream never pushes — the pipe branch
		// at :950 emits a "text" token — so the second disjunct only fires on a
		// paren inside an open extglob. It is kept as upstream spells it.
		isExtglob := t.extglob || (len(s.extglobs) > 0 && (t.typ == "pipe" || t.typ == "paren"))
		if t.typ != "slash" && t.typ != "paren" && !isBrace && !isExtglob {
			if s.prev.output == nil {
				s.fail(&UnsupportedError{Construct: "globstar lookbehind on a token with no output", Site: "parse.js:499", Index: s.index})
				return
			}
			// parse.js:499. dropLast is JavaScript's slice(0, -n), which empties
			// the output when n is 0 rather than leaving it alone. Trap #7.
			s.output = dropLast(s.output, len(*s.prev.output))
			s.prev.typ = "star"
			s.prev.value = encode("*")
			s.prev.output = out(s.star)
			s.output = append(s.output, *s.prev.output...)
		}
	}
	// parse.js:507-509. Every non-paren token pushed while an extglob is open
	// accumulates into the innermost one, which is what extglobClose reads to
	// decide whether the body contained a "/" or a "*". The value is copied here
	// rather than aliased: push() grows token values in place afterwards.
	if len(s.extglobs) > 0 && t.typ != "paren" {
		e := s.extglobs[len(s.extglobs)-1]
		e.inner = append(e.inner, t.value...)
	}

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

// starGuard is the paired append at parse.js:1263-1281:
//
//	state.output += nodot;
//	prev.output  += nodot;
//
// The token before a leading star is rewritten *after* it was emitted, which is
// the first retroactive edit in the port and the reason push() seeds a text
// token's output from a clone rather than aliasing its value. Appending in place
// here would be the same hazard one level up — prev.output would grow into
// whatever backing array it shares — so the previous output is copied before the
// guard is appended. It runs at most twice per star, so the copy is not on any
// hot path.
//
// prev.output is never absent at this site: the three arms that reach it are
// bos, slash and dot, and all three carry an output. If some later branch makes
// that untrue the answer is not the empty string — JavaScript's `undefined + x`
// is the string "undefined" + x — so it refuses instead of guessing.
func (s *scanner) starGuard(guard string) {
	g := encode(guard)
	s.output = append(s.output, g...)
	if s.prev.output == nil {
		s.fail(&UnsupportedError{Construct: "star guard on a token with no output", Site: "parse.js:1264", Index: s.index})
		return
	}
	o := append(s.prev.output.clone(), g...)
	s.prev.output = &o
}

// dropLast is JavaScript's `u.slice(0, -n)`, including the degenerate case that
// makes it different from the sentence it appears to be.
//
// Upstream truncates state.output this way at five sites — parse.js:499, :861,
// :1189, :1204 and :1232 — always as "drop the fragment I am about to replace".
// When that fragment is the empty string, n is 0, JavaScript's -0 is 0, and the
// call is slice(0, 0): the *whole* output is discarded. The Go reading
// `u[:len(u)-n]` leaves it untouched instead, which is the opposite thing.
// An empty output is not rare — 1,883 of the 10,558 recorded tokens carry one.
// docs/transcription-traps.md #7.
func dropLast(u units, n int) units {
	if n <= 0 || n >= len(u) {
		return u[:0]
	}
	return u[:len(u)-n]
}

func (s *scanner) fail(err error) {
	if s.err == nil {
		s.err = err
	}
}

// The `unsupported` helper that built an [UnsupportedError] for a declined
// construct is gone with the brace branch: no branch of the loop declines one
// any more. The three remaining constructions of that type are the guards on a
// token with no output, and each is written at its site rather than through a
// helper, because each names a condition rather than a construct.

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

			// parse.js:675. Under opts.bash the condition is false and the
			// backslash falls through to the general escape handling below
			// instead of being silently dropped.
			if ok && next == '/' && !s.opts.Bash {
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

			// parse.js:710. The branch ends without a continue: an escape
			// inside a character class falls through to the body below,
			// carrying the two-unit escaped value. Only the "!next" arm at
			// :683 pushes a token while state.brackets is nonzero, and it can
			// only fire on the last unit of the input, so prev stays the
			// bracket token for as long as the body branch keeps running.
		}

		// Character class body. parse.js:718-758.
		//
		// A "]" is a member rather than the close when nothing has been
		// accumulated yet, which is why "[]" and "[^]" scan on rather than
		// closing empty.
		if s.brackets > 0 && (!isUnit(value, ']') || bracketJustOpened(s.prev.value)) {
			// POSIX classes, parse.js:719-741. The test is `opts.posix !==
			// false`, so this arm is live by default and only an explicit
			// `posix: false` turns it off. The other reader of the same key,
			// twenty lines down at :751, is `opts.posix === true` — one option,
			// two default answers.
			if s.posixClasses && isUnit(value, ':') {
				inner := sliceFrom(s.prev.value, 1)
				if inner.contains('[') {
					// parse.js:722. Set on the *outer* test, so a name that
					// does not resolve — "[[:foo:]]" — still marks the token
					// posix and still suppresses the "^" rewrite at :847.
					s.prev.posix = true

					if inner.contains(':') {
						// idx cannot be -1: inner is prev.value from index 1,
						// and it was just found to contain a "[", so the last
						// one in prev.value is at index 1 or later.
						idx := lastIndexOf(s.prev.value, '[', len(s.prev.value))
						pre := s.prev.value[:idx]
						// slice(idx + 2) steps over the "[" and the ":" that
						// opened the class name.
						rest := sliceFrom(s.prev.value, idx+2)

						if posix, ok := posixSource(rest); ok {
							// parse.js:730. The value is replaced, so it stops
							// being the text state.output holds — which is why
							// :731 sets backtrack and the output is rebuilt.
							pv := append(pre.clone(), encode(posix)...)
							s.prev.value = pv
							s.backtrack = true

							// parse.js:732. advance() steps over the "]" of
							// ":]" and returns it to nobody: the unit is never
							// appended to prev.value or to state.consumed.
							//
							// It is also the second place upstream's index can
							// pass the end. eos() is an equality, so when the
							// ":" is the last unit of the input this never
							// returns: "[][:alpha:" hangs node. Same reasoning
							// as the backslash collapse — there is no recorded
							// behaviour to match and no fixture can hold one.
							// DECISIONS.md §11.
							if s.index+1 >= len(s.input) {
								return &NonTerminatingError{Site: "parse.js:732", Index: s.index}
							}
							s.advance()

							// parse.js:734. bos.output is "" by default, which
							// is falsy, so !bos.output is true for an unset
							// opts.prepend as well as for a missing field.
							if (s.bos.output == nil || len(*s.bos.output) == 0) &&
								len(s.tokens) > 1 && s.tokens[1] == s.prev {
								s.bos.output = out(s.chars.oneChar)
							}
							continue
						}
					}
				}
			}

			// parse.js:743-745. Note "[" is left bare when a ":" follows it,
			// which is what lets the POSIX arm above see "[[:" in prev.value.
			if (isUnit(value, '[') && !s.peekIs(1, ':')) || (isUnit(value, '-') && s.peekIs(1, ']')) {
				value = escapePrefix(value)
			}

			// parse.js:747-749.
			if isUnit(value, ']') && bracketJustOpened(s.prev.value) {
				value = escapePrefix(value)
			}

			// parse.js:751-753. Under `posix: true` a "!" directly after a bare
			// "[" becomes "^". It replaces value rather than prefixing it, so
			// the "!" is gone from prev.value and from the emitted token both —
			// the class is negated, not negated-and-literal.
			//
			// The test is prev.value === "[" exactly, so only the first unit of
			// the body can take it: "[a!]" keeps its "!". And it runs after the
			// two escape arms above, neither of which can fire on "!".
			if s.posixNegate && isUnit(value, '!') && s.prev.value.equal(encode("[")) {
				value = encode("^")
			}

			// parse.js:755-756. The body accumulates onto the bracket token one
			// value at a time and never pushes, so a body is mid-surrogate
			// between iterations — the third of the three sites that force
			// units over a Go string. DECISIONS.md §8.
			s.prev.value = append(s.prev.value, value...)
			s.emit(&token{value: value})
			continue
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
			s.increment("parens")
			s.push(&token{typ: "paren", value: value})
			if s.err != nil {
				return s.err
			}
			continue
		}
		if c == ')' {
			// parse.js:795, opts.strictBrackets throws here. Marked, not written.

			// parse.js:800. The innermost extglob closes only at exactly one
			// paren deeper than it opened at; anything else is a plain group.
			if n := len(s.extglobs); n > 0 && s.parens == s.extglobs[n-1].parens+1 {
				e := s.extglobs[n-1]
				s.extglobs = s.extglobs[:n-1]
				if err := s.extglobClose(e, value); err != nil {
					return err
				}
				continue
			}

			// parse.js:805-806. state.parens is read for truthiness *before* the
			// decrement, and the decrement is unguarded — an unmatched ")" takes
			// the counter negative, which is truthy, so a second one emits ")"
			// where the first emitted "\)".
			output := `\)`
			if s.parens != 0 {
				output = `)`
			}
			s.push(&token{typ: "paren", value: value, output: out(output)})
			if s.err != nil {
				return s.err
			}
			s.decrement("parens")
			continue
		}

		// Square brackets. parse.js:814-875.
		if c == '[' {
			// parse.js:815. The test is whether a "]" appears anywhere in the
			// rest of the input, not whether one *matches*: escaping and
			// nesting are not considered, so "[a\]" opens a class that never
			// closes and is patched by escapeLast after the loop.
			if !s.remaining().contains(']') { // opts.nobracket
				// parse.js:816-818, opts.strictBrackets throws here. Marked,
				// not written.
				value = escapePrefix(value)
			} else {
				s.increment("brackets")
			}

			s.push(&token{typ: "bracket", value: value})
			if s.err != nil {
				return s.err
			}
			continue
		}
		if c == ']' {
			// parse.js:830. The bracket-of-length-one arm is unreachable under
			// default options: a "[" that incremented the counter pushes a
			// one-unit value, but then state.brackets is nonzero and the body
			// branch above claims the "]" first (prev.value === "["); a "[" that
			// did not increment pushes the two-unit "\[". It is transcribed
			// because opts.nobracket reaches it and because a guess about which
			// arm is dead is not worth the line it saves.
			if s.prev != nil && s.prev.typ == "bracket" && len(s.prev.value) == 1 { // opts.nobracket
				s.push(&token{typ: "text", value: value, output: out(`\]`)})
				if s.err != nil {
					return s.err
				}
				continue
			}

			if s.brackets == 0 {
				// parse.js:836, opts.strictBrackets throws here. Marked, not
				// written.
				//
				// The output is an *escaped* "]" where the matching arm for an
				// unopened "}" at :901 emits a bare "}". The two branches are
				// otherwise symmetrical and ninety lines apart. Trap #4.
				s.push(&token{typ: "text", value: value, output: out(`\]`)})
				if s.err != nil {
					return s.err
				}
				continue
			}

			s.decrement("brackets")

			// parse.js:846. Copied rather than sliced: prev.value is appended
			// to two lines down, and prevValue is read after that.
			prevValue := sliceFrom(s.prev.value, 1).clone()

			// parse.js:847-849. A negated class gains a "/" member, and it goes
			// into the token *value*, so state.consumed grows a unit the input
			// never had: "[^a]" consumes "[^a/]". Trap #5's second mechanism.
			if !s.prev.posix && len(prevValue) > 0 && prevValue[0] == '^' && !prevValue.contains('/') {
				value = append(units{'/'}, value...)
			}

			s.prev.value = append(s.prev.value, value...)
			s.emit(&token{value: value})

			// parse.js:856. opts.literalBrackets === false takes this exit too.
			// Marked, not written.
			if hasRegexChars(prevValue) {
				continue
			}

			escaped := escapeRegex(s.prev.value)

			// parse.js:861, the fifth slice(0, -X.length) site and the only one
			// that slices by a token *value* rather than by an output. n is at
			// least 2 here — prev.value is "[" plus the "]" just appended — so
			// unlike the four in trap #7 the degenerate -0 case cannot arise.
			// The tail of state.output is not always prev.value either: the
			// POSIX arm rewrites the value and leaves the emitted text alone,
			// and this truncates by the wrong count. That is upstream's
			// arithmetic and it is invisible, because the same arm set
			// backtrack and the output is rebuilt at :1309.
			s.output = dropLast(s.output, len(s.prev.value))

			// parse.js:865-869, opts.literalBrackets === true. Marked, not
			// written; the unset path is the one below.

			// parse.js:872-873. With nothing specified, match both the literal
			// text and the character class.
			nv := encode(`(` + capture)
			nv = append(nv, escaped...)
			nv = append(nv, '|')
			nv = append(nv, s.prev.value...)
			nv = append(nv, ')')
			s.prev.value = nv
			s.output = append(s.output, nv...)
			continue
		}

		// Braces. parse.js:881-940.
		if c == '{' { // opts.nobrace
			s.increment("braces")

			// parse.js:888-889. Both indexes are taken *before* the push, so
			// they name the position this token is about to occupy and the
			// output as it stood without it. outputIndex is the only one of its
			// kind in the file — 18 recorded tokens carry it, every one as 0.
			oi, ti := len(s.output), len(s.tokens)
			open := &token{
				typ:         "brace",
				value:       value,
				output:      out("("),
				outputIndex: &oi,
				tokensIndex: &ti,
			}

			s.braceStack = append(s.braceStack, open)
			s.push(open)
			if s.err != nil {
				return s.err
			}
			continue
		}
		if c == '}' {
			var brace *token
			if n := len(s.braceStack); n > 0 {
				brace = s.braceStack[n-1]
			}

			// parse.js:900. A "}" with nothing open emits the literal "}",
			// where the matching arm for an unopened "]" at :840 emits "\]".
			// The two branches are otherwise symmetrical and ninety lines
			// apart. Trap #4.
			if brace == nil { // opts.nobrace
				s.push(&token{typ: "text", value: value, output: out("}")})
				if s.err != nil {
					return s.err
				}
				continue
			}

			output := encode(")")

			// parse.js:907-923. A "{a..b}" range: unwind the tokens back to the
			// brace, collect their values, and replace the lot with a character
			// class. The pop is unconditional and runs *before* the type test,
			// so the brace token is removed too — which is why no recorded token
			// ever carries the `dots` flag this arm reads.
			if brace.dots {
				arr := append([]*token(nil), s.tokens...)
				var rng []units
				for i := len(arr) - 1; i >= 0; i-- {
					s.tokens = s.tokens[:len(s.tokens)-1]
					if arr[i].typ == "brace" {
						break
					}
					if arr[i].typ != "dots" {
						rng = append([]units{arr[i].value}, rng...)
					}
				}

				// s.prev is deliberately left pointing at a token that is no
				// longer in the list: upstream pops from `tokens` and never
				// reassigns `prev`, so the push below reads the popped token for
				// its globstar lookbehind and links the new brace to it.
				output = expandRange(rng)
				s.backtrack = true
			}

			// parse.js:925-934. A brace with neither a comma nor a range was
			// never an alternation, so both delimiters become literal and the
			// output is rebuilt from the brace's own outputIndex — the deepest
			// rewrite in the file after extglobClose's, and like that one it is
			// not decided until the closing character.
			if !brace.comma && !brace.dots {
				// slice(0, n) clamps rather than panicking, and n can exceed the
				// output: the globstar arms and extglobClose both *assign*
				// state.output, so it can be shorter than it was at the "{".
				oi := *brace.outputIndex
				if oi > len(s.output) {
					oi = len(s.output)
				}
				ti := *brace.tokensIndex
				if ti > len(s.tokens) {
					ti = len(s.tokens)
				}
				toks := append([]*token(nil), s.tokens[ti:]...)

				brace.value = encode(`\{`)
				brace.output = out(`\{`)
				value = encode(`\}`)
				output = encode(`\}`)

				// Cloned rather than resliced: appending into s.output's spare
				// capacity would write past the prefix that is being kept, and
				// the replay below appends immediately.
				s.output = s.output[:oi].clone()
				for _, t := range toks {
					// parse.js:932 is `t.output || t.value` — JavaScript
					// truthiness, so an *empty* output falls back to the value.
					// The post-loop rebuild at :1313 spells the same fallback as
					// `!= null` and keeps the empty string. Two replays, two
					// different rules, ninety lines apart.
					if t.output != nil && len(*t.output) > 0 {
						s.output = append(s.output, *t.output...)
					} else {
						s.output = append(s.output, t.value...)
					}
				}
			}

			s.push(&token{typ: "brace", value: value, output: &output})
			if s.err != nil {
				return s.err
			}
			s.decrement("braces")
			s.braceStack = s.braceStack[:len(s.braceStack)-1]
			continue
		}

		// Pipes. parse.js:946-952. Note the token type is "text", not "pipe" —
		// "pipe" is named in two lookbehinds (:496, :1162) and pushed nowhere.
		if c == '|' {
			if n := len(s.extglobs); n > 0 {
				s.extglobs[n-1].conditions++
			}
			s.push(&token{typ: "text", value: value})
			if s.err != nil {
				return s.err
			}
			continue
		}

		// Commas. parse.js:958-969. Outside braces the output is the comma
		// itself; inside, it becomes the alternation bar.
		if c == ',' {
			output := value.clone()

			// parse.js:961-962. Two tests, not one: a brace must be open *and*
			// be the innermost open construct. "{a(b,c)}" has state.braces at 1
			// and "parens" on top of the stack, so its comma stays a comma.
			// This is the only reader of `stack` in the file.
			if n := len(s.braceStack); n > 0 && len(s.stack) > 0 && s.stack[len(s.stack)-1] == "braces" {
				s.braceStack[n-1].comma = true
				output = encode("|")
			}

			s.push(&token{typ: "comma", value: value, output: &output})
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

			s.push(&token{typ: "slash", value: value, output: out(s.chars.slashLiteral)})
			if s.err != nil {
				return s.err
			}
			continue
		}

		// Dots. parse.js:997-1015.
		if c == '.' {
			// parse.js:998-1006. A second dot inside a brace turns the first
			// one's token into a "dots" — a type that exists only between here
			// and the closing "}", which pops it.
			//
			// The test is prev.type === "dot", not "dots", so a third dot does
			// not extend the range: "{a...b}" is a dots token and then a dot.
			if s.braces > 0 && s.prev.typ == "dot" {
				// parse.js:999. A dot token is pushed with DOT_LITERAL already,
				// so this only matters where something has since rewritten the
				// output — starGuard (:1264) appends the dot guard to a prev of
				// type "dot".
				if isUnit(s.prev.value, '.') {
					s.prev.output = out(s.chars.dotLiteral)
				}
				if s.prev.output == nil {
					// JavaScript's `undefined + "."` is the string "undefined.",
					// not "."; there is nothing sensible to guess. Every pusher
					// of a "dot" token gives it an output, so this cannot fire.
					s.fail(&UnsupportedError{Construct: "brace range on a dot token with no output", Site: "parse.js:1002", Index: s.index})
					return s.err
				}
				brace := s.braceStack[len(s.braceStack)-1]
				s.prev.typ = "dots"
				po := append(s.prev.output.clone(), value...)
				s.prev.output = &po
				s.prev.value = append(s.prev.value, value...)
				brace.dots = true
				continue
			}

			if s.braces+s.parens == 0 && s.prev.typ != "bos" && s.prev.typ != "slash" {
				s.push(&token{typ: "text", value: value, output: out(s.chars.dotLiteral)})
				if s.err != nil {
					return s.err
				}
				continue
			}
			s.push(&token{typ: "dot", value: value, output: out(s.chars.dotLiteral)})
			if s.err != nil {
				return s.err
			}
			continue
		}

		// Question marks. parse.js:1021-1047. Four arms, and the first three
		// test three different things about the same token: :1022 reads
		// prev.value, :1028 and :1040 read prev.type.
		if c == '?' {
			isGroup := s.prev != nil && s.prev.value.equal(encode("("))
			if !isGroup && !s.noextglob && s.peekIs(1, '(') && !s.peekIs(2, '?') {
				s.extglobOpen("qmark", value)
				if s.err != nil {
					return s.err
				}
				continue
			}

			// parse.js:1028-1038. Directly after a paren token the "?" is part
			// of the group's own syntax rather than a glob, so it is emitted
			// bare — unless doing so would open a group upstream does not mean,
			// in which case it is escaped. The token type is "text", not
			// "qmark", so it merges with adjacent text.
			if s.prev != nil && s.prev.typ == "paren" {
				next, hasNext := s.peek(1)
				output := value.clone()

				// parse.js:1032. Both halves are regexp tests on values that
				// can be undefined, and JavaScript coerces before testing:
				// /[!=<:]/.test(undefined) tests the string "undefined", which
				// contains none of them, so a "?" that ends the input after a
				// "(" takes the escape. The second half is a *search* over the
				// whole of remaining(), not a test of the two units after the
				// "<".
				if (s.prev.value.equal(encode("(")) && !isLookaroundIntro(next, hasNext)) ||
					(hasNext && next == '<' && !hasAngleGroupIntro(s.remaining())) {
					output = escapePrefix(value)
				}

				s.push(&token{typ: "text", value: value, output: &output})
				if s.err != nil {
					return s.err
				}
				continue
			}

			// parse.js:1040-1043. A "?" that opens a path segment must not
			// match a leading dot, and the guard is a class that excludes both
			// the dot and the separator rather than a lookahead in front of
			// QMARK.
			if !s.opts.Dot && (s.prev.typ == "slash" || s.prev.typ == "bos") {
				s.push(&token{typ: "qmark", value: value, output: out(s.chars.qmarkNoDot)})
				if s.err != nil {
					return s.err
				}
				continue
			}

			// parse.js:1045. QMARK is `[^/]` — exactly one UTF-16 code unit, so
			// an astral character is matched by two "?" and not by one. The
			// second of the three sites that force units over a Go string, and
			// the one no fixture in testdata/original can see. DECISIONS.md §8.
			s.push(&token{typ: "qmark", value: value, output: out(s.chars.qmark)})
			if s.err != nil {
				return s.err
			}
			continue
		}

		// Exclamation. parse.js:1053-1065. Neither arm is a fallthrough guard:
		// a "!" that is neither an extglob opener nor at index 0 drops out of
		// this branch and is picked up as plain text below.
		if c == '!' {
			if !s.noextglob && s.peekIs(1, '(') {
				if !s.peekIs(2, '?') || !isLookaroundIntro(s.peek(3)) {
					s.extglobOpen("negate", value)
					if s.err != nil {
						return s.err
					}
					continue
				}
			}
			if s.index == 0 { // opts.nonegate
				s.negate()
				continue
			}
		}

		// Plus. parse.js:1071-1089. Three arms, three shapes: the first sets
		// value and output, the second neither, and the third puts the escape in
		// value and sets no output at all. Trap #3.
		if c == '+' {
			if !s.noextglob && s.peekIs(1, '(') && !s.peekIs(2, '?') {
				s.extglobOpen("plus", value)
				if s.err != nil {
					return s.err
				}
				continue
			}
			// parse.js:1077. `opts.regex === false` is the *second* half of an ||
			// whose first half is live by default, so it widens this arm rather
			// than opening a new one: every "+" emits PLUS_LITERAL, not just the
			// one directly after a "(".
			if (s.prev != nil && s.prev.value.equal(encode("("))) || s.regexFalse {
				s.push(&token{typ: "plus", value: value, output: out(s.chars.plusLiteral)})
				if s.err != nil {
					return s.err
				}
				continue
			}
			if (s.prev != nil && (s.prev.typ == "bracket" || s.prev.typ == "paren" || s.prev.typ == "brace")) || s.parens > 0 {
				s.push(&token{typ: "plus", value: value})
				if s.err != nil {
					return s.err
				}
				continue
			}
			s.push(&token{typ: "plus", value: encode(s.chars.plusLiteral)})
			if s.err != nil {
				return s.err
			}
			continue
		}

		// At. parse.js:1095-1103.
		if c == '@' {
			if !s.noextglob && s.peekIs(1, '(') && !s.peekIs(2, '?') {
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

		// parse.js:1128-1137 folds a star into a preceding globstar, or into a
		// token an earlier star was already folded into. It is the only site in
		// parse() that sets state.backtrack, and therefore the only thing that
		// makes the post-loop rebuild at :1309-1319 run.
		if s.prev != nil && (s.prev.typ == "globstar" || s.prev.star) {
			s.prev.typ = "star"
			s.prev.star = true
			s.prev.value = append(s.prev.value, value...)
			s.prev.output = out(s.star)
			s.backtrack = true
			s.globstar = true
			s.consume(value, 0)
			continue
		}

		rest := s.remaining()

		// parse.js:1140, /^\([^?]/ — "*(" opens an extglob. Note this is spelled
		// differently from its four siblings: it needs two characters, so a
		// trailing "*(" falls through to the globstar arms where "+(" at the end
		// of input still opens.
		if !s.noextglob && isExtglobOpen(rest) {
			s.extglobOpen("star", value)
			if s.err != nil {
				return s.err
			}
			continue
		}

		// parse.js:1145-1244, the globstar arms. Every one of them rewrites the
		// star token that is already on the stack rather than pushing a new one,
		// so `prev` here is the first star of the pair and `value` is the second.
		if s.prev.typ == "star" {
			// parse.js:1146, opts.noglobstar: consume the second star and emit
			// nothing. Marked, not written.

			// parse.js:1151-1154. prior is two tokens back — the token before
			// the first star — and `before` is three. Neither lookup can be nil
			// here: prev is a pushed token so it carries prev, and prior is only
			// dereferenced further down behind a `prior.type === "slash"` test,
			// which excludes bos, the one token whose prev is nil.
			prior := s.prev.prev
			before := prior.prev
			isStart := prior.typ == "slash" || prior.typ == "bos"
			afterStar := before != nil && (before.typ == "star" || before.typ == "globstar")

			// parse.js:1156. Under opts.bash a star that either does not start a
			// path segment or is followed by more non-slash text stays a plain
			// star with an empty output, same shape as the isStart/isBrace/
			// isExtglob arm three lines down but a different — and wider —
			// condition, so it has to be checked first and separately.
			if s.opts.Bash && (!isStart || (len(rest) > 0 && rest[0] != '/')) {
				s.push(&token{typ: "star", value: value, output: out("")})
				if s.err != nil {
					return s.err
				}
				continue
			}

			// parse.js:1161-1166. A star that does not start a path segment
			// stays a plain star, and its token carries an output that is
			// present and *empty* rather than absent — it is still pushed, and
			// push() still consumes its value. Trap #21.
			//
			// isExtglob (:1162) tests prior.type against "pipe", a type upstream
			// never pushes, so only the "paren" half can fire — and prior.type
			// !== "paren" is already tested beside it.
			isBrace := s.braces > 0 && (prior.typ == "comma" || prior.typ == "brace")
			isExtglob := len(s.extglobs) > 0 && (prior.typ == "pipe" || prior.typ == "paren")
			if !isStart && prior.typ != "paren" && !isBrace && !isExtglob {
				s.push(&token{typ: "star", value: value, output: out("")})
				if s.err != nil {
					return s.err
				}
				continue
			}

			// parse.js:1168-1176, strip consecutive "/**". consume's second
			// argument advances the index past the three units this accounts
			// for; it is the one call site of the nine that passes it (trap #6).
			for len(rest) >= 3 && rest[0] == '/' && rest[1] == '*' && rest[2] == '*' {
				// parse.js:1170 peeks index+4, not index+3. rest is
				// remaining(), which already starts at index+1, so the unit
				// after the three matched here sits four ahead of the index.
				// Trap #8.
				if after, ok := s.peek(4); ok && after != '/' {
					break
				}
				rest = rest[3:]
				s.consume(encode("/**"), 3)
			}

			// parse.js:1178-1186. A whole-pattern "**": state.output is
			// *assigned*, not appended to, so the leading dot guard the star
			// branch already wrote is dropped from the output while bos keeps
			// it. Trap #18.
			if prior.typ == "bos" && s.eos() {
				s.prev.typ = "globstar"
				s.prev.value = append(s.prev.value, value...)
				s.prev.output = out(s.globstarBody)
				s.output = s.prev.output.clone()
				s.globstar = true
				s.consume(value, 0)
				continue
			}

			// parse.js:1188-1199, a trailing "/**".
			if prior.typ == "slash" && prior.prev.typ != "bos" && !afterStar && s.eos() {
				// parse.js:1189. The truncation is by the sum of *two* token
				// outputs, and prior.output is read here before :1190 rewrites
				// it. Trap #19.
				s.output = dropLast(s.output, len(*prior.output)+len(*s.prev.output))
				// Built on units rather than through String(): the boundary
				// conversion folds an unpaired surrogate to U+FFFD, and prior's
				// output is not the scanner's to launder. DECISIONS.md §10.
				po := append(encode(`(?:`), *prior.output...)
				prior.output = &po

				s.prev.typ = "globstar"
				// parse.js:1193. opts.strictSlashes emits ")" in place of "|$)".
				closer := `|$)`
				if s.opts.StrictSlashes {
					closer = `)`
				}
				s.prev.output = out(s.globstarBody + closer)
				s.prev.value = append(s.prev.value, value...)
				s.globstar = true
				s.output = append(s.output, *prior.output...)
				s.output = append(s.output, *s.prev.output...)
				s.consume(value, 0)
				continue
			}

			// parse.js:1201-1218, a "/**/" in the middle of a pattern.
			if prior.typ == "slash" && prior.prev.typ != "bos" && len(rest) > 0 && rest[0] == '/' {
				end := ""
				if len(rest) > 1 { // rest[1] !== void 0
					end = `|$`
				}

				s.output = dropLast(s.output, len(*prior.output)+len(*s.prev.output))
				// Built on units rather than through String(): the boundary
				// conversion folds an unpaired surrogate to U+FFFD, and prior's
				// output is not the scanner's to launder. DECISIONS.md §10.
				po := append(encode(`(?:`), *prior.output...)
				prior.output = &po

				s.prev.typ = "globstar"
				s.prev.output = out(s.globstarBody + s.chars.slashLiteral + `|` + s.chars.slashLiteral + end + `)`)
				s.prev.value = append(s.prev.value, value...)

				s.output = append(s.output, *prior.output...)
				s.output = append(s.output, *s.prev.output...)
				s.globstar = true

				// parse.js:1214. The slash is consumed here *and* again by the
				// push below, which is how state.consumed grows a slash the
				// input never had: "a/**/b" consumes "a/**//b". Trap #5.
				slash := s.advance()
				s.consume(append(value.clone(), slash), 0)

				s.push(&token{typ: "slash", value: encode("/"), output: out("")})
				if s.err != nil {
					return s.err
				}
				continue
			}

			// parse.js:1220-1229, a leading "**/".
			if prior.typ == "bos" && len(rest) > 0 && rest[0] == '/' {
				s.prev.typ = "globstar"
				s.prev.value = append(s.prev.value, value...)
				s.prev.output = out(`(?:^|` + s.chars.slashLiteral + `|` + s.globstarBody + s.chars.slashLiteral + `)`)
				s.output = s.prev.output.clone() // assigned, not appended — trap #18
				s.globstar = true

				slash := s.advance()
				s.consume(append(value.clone(), slash), 0)

				s.push(&token{typ: "slash", value: encode("/"), output: out("")})
				if s.err != nil {
					return s.err
				}
				continue
			}

			// parse.js:1231-1243. Remove the single star from the output and
			// put the globstar body in its place.
			s.output = dropLast(s.output, len(*s.prev.output))
			s.prev.typ = "globstar"
			s.prev.output = out(s.globstarBody)
			s.prev.value = append(s.prev.value, value...)
			s.output = append(s.output, *s.prev.output...)
			s.globstar = true
			s.consume(value, 0)
			continue
		}

		// parse.js:1246. out() encodes a fresh slice per call; a shared package
		// slice would be appended to in place by the guard below.
		tok := &token{typ: "star", value: value, output: out(s.star)}

		// parse.js:1248. Under opts.bash the star's output is always ".*?"
		// rather than `star`, gaining the nodot prefix when it opens a path
		// segment — a *different* guard from the one at :1263 below (that one
		// tests state.index === state.start too, not just bos/slash), and this
		// arm returns before reaching it.
		if s.opts.Bash {
			bashOutput := ".*?"
			if s.prev.typ == "bos" || s.prev.typ == "slash" {
				bashOutput = s.nodot + bashOutput
			}
			tok.output = out(bashOutput)
			s.push(tok)
			if s.err != nil {
				return s.err
			}
			continue
		}

		// parse.js:1257. Under `regex: true` a star directly after a bracket or
		// paren token gets its own raw value as output — the "*" is the caller's
		// quantifier over the construct just closed, not a glob. Placed after
		// the opts.bash arm above, as upstream places it, so bash wins where
		// both are set.
		if s.prev != nil && (s.prev.typ == "bracket" || s.prev.typ == "paren") && s.regexTrue {
			output := value.clone()
			tok.output = &output
			s.push(tok)
			if s.err != nil {
				return s.err
			}
			continue
		}

		// parse.js:1263. The test is state.index === state.start, not === 0:
		// start moves past a negation prologue and past a "./" that survived
		// behind one, so "!*" and "!./*" take this arm at index 1 and 3.
		if s.index == s.start || s.prev.typ == "slash" || s.prev.typ == "dot" {
			var guard string
			switch {
			case s.prev.typ == "dot":
				guard = s.chars.noDotSlash
			case s.opts.Dot:
				guard = s.chars.noDotsSlash
			default:
				guard = s.nodot
			}
			s.starGuard(guard)

			// parse.js:1279. peek() is the unit after the star, so "**"
			// suppresses the one-character guard even though the second star is
			// not consumed here.
			if !s.peekIs(1, '*') {
				s.starGuard(s.chars.oneChar)
			}
		}

		// parse.js:1283.
		s.push(tok)
		if s.err != nil {
			return s.err
		}
	}

	if s.err != nil {
		return s.err
	}

	// Unclosed brackets, parens and braces. parse.js:1286-1302. All three loops
	// are live. opts.strictBrackets throws in each instead — marked, not
	// written.
	//
	// The brace loop does not pop braceStack, exactly as upstream does not pop
	// `braces`: the scan is over and nothing reads either again.
	for s.brackets > 0 {
		s.output = escapeLast(s.output, '[', len(s.output))
		s.decrement("brackets")
	}

	for s.parens > 0 {
		s.output = escapeLast(s.output, '(', len(s.output))
		s.decrement("parens")
	}

	for s.braces > 0 {
		s.output = escapeLast(s.output, '{', len(s.output))
		s.decrement("braces")
	}

	// parse.js:1304-1306, opts.strictSlashes. Both producers of those types are
	// now built, so both arms are live.
	if !s.opts.StrictSlashes && (s.prev.typ == "star" || s.prev.typ == "bracket") {
		s.push(&token{typ: "maybe_slash", value: units{}, output: out(s.chars.slashLiteral + "?")})
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

// bracketJustOpened is `prev.value === '[' || prev.value === '[^'`, the test
// spelled twice in the character-class body (parse.js:718 and :747).
//
// It is what makes a "]" the first member of an empty class rather than its
// close, so "[]]" is one bracket token holding "[\]]" and not a class followed
// by stray text.
func bracketJustOpened(u units) bool {
	return isUnit(u, '[') || (len(u) == 2 && u[0] == '[' && u[1] == '^')
}

// isExtglobOpen is the /^\([^?]/ test at parse.js:1140, applied to remaining().
// It needs two units, so a trailing "(" does not match it.
func isExtglobOpen(rest units) bool {
	return len(rest) >= 2 && rest[0] == '(' && rest[1] != '?'
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

// hasAngleGroupIntro is the /<([!=]|\w+>)/ test at parse.js:1032, applied to
// remaining().
//
// It is the second, wider half of that line's pair; isLookaroundIntro above is
// the first. Three things about it do not survive being paraphrased:
//
//   - it is a *search*, not an anchored test. The caller has already
//     established that remaining() starts with "<", so the natural reading is
//     "is this a lookbehind or a named group" — but the regexp is unanchored,
//     so a "<...>" anywhere later in the pattern satisfies it just as well.
//   - "\w" in a non-unicode JavaScript regexp is exactly [A-Za-z0-9_]. It is
//     not Unicode-aware, so it is a unit test rather than a rune test.
//   - "\w+" is greedy with backtracking, but only the maximal run can be
//     followed by ">": a shorter run would have to be followed by a word
//     character. So one forward scan per "<" is the whole of it.
func hasAngleGroupIntro(u units) bool {
	for i := 0; i < len(u); i++ {
		if u[i] != '<' {
			continue
		}
		if i+1 < len(u) && (u[i+1] == '!' || u[i+1] == '=') {
			return true
		}
		j := i + 1
		for j < len(u) && isWordUnit(u[j]) {
			j++
		}
		if j > i+1 && j < len(u) && u[j] == '>' {
			return true
		}
	}
	return false
}

// isWordUnit is JavaScript's \w without the /u flag: [A-Za-z0-9_], ASCII only.
func isWordUnit(c uint16) bool {
	return c == '_' ||
		(c >= '0' && c <= '9') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= 'a' && c <= 'z')
}

// prefixTokens is gone with the brace branch, and its removal is the prediction
// DECISIONS.md §14 made rather than an unrelated tidy-up.
//
// It truncated a *declined* parse back to the last open "+(" or "*(", because
// extglobClose's risky path (parse.js:544-566) can rewrite every token from
// there onwards and does not decide until the closing ")". That mattered only
// while some construct inside an extglob body could still decline. With "{"
// built there is none: no branch of the loop returns an UnsupportedError for a
// construct any more, so no parse can stop mid-extglob and there is nothing
// unsettled to hand back.
//
// The three UnsupportedErrors left in this file are guards on a token that has
// no output — push()'s globstar lookbehind at parse.js:499, starGuard at :1264,
// and the brace-range arm at :1002 — and all three are unreachable: every token
// type any of those arms can meet carries an output. They are kept because a
// guess about which arm is dead is not worth the line it saves, not because a
// construct is missing. (extglob.go re-wraps an error from the recursive parse
// at :588, which can no longer produce one; it constructs nothing new.)
func (s *scanner) export() *State { return s.exportTokens(s.tokens) }

func (s *scanner) exportTokens(toks []*token) *State {
	st := &State{
		Consumed:       s.consumed.String(),
		Output:         s.output.String(),
		Negated:        s.negated,
		Backtrack:      s.backtrack,
		Globstar:       s.globstar,
		NegatedExtglob: s.negatedExtglob,
		Tokens:         make([]Token, 0, len(toks)),
	}
	for _, t := range toks {
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
