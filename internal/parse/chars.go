package parse

// The platform constant table — constants.globChars(win32) at constants.js:181,
// which returns POSIX_CHARS (:29) or WINDOWS_CHARS (:52) whole.
//
// `opts.windows` is read at exactly two sites in lib/, and both do nothing with
// it but pick one of these two tables:
//
//	parse.js:377   const PLATFORM_CHARS = constants.globChars(opts.windows);
//	parse.js:1351  } = constants.globChars(opts.windows);   // fastpaths
//
// So the key is a table swap and not a branch anywhere. Re-check:
//
//	grep -n "opts.windows" tests/original/lib/*.js
//
// # Four leaves, twelve derivations
//
// WINDOWS_CHARS is spelled in constants.js as a spread of POSIX_CHARS with
// twelve keys overridden, which reads as twelve independent strings. It is not:
// four of them are leaves and the rest fall out of the same expressions
// constants.js:18-26 uses to build the POSIX set. globCharsFor takes the leaves
// and re-derives, so a change to one is not twelve edits.
//
// The leaves are SLASH_LITERAL, QMARK, QMARK_NO_DOT and SEP. The third is the
// trap: it looks derivable from SLASH_LITERAL and is not.
//
//	POSIX    QMARK_NO_DOT = `[^.${SLASH_LITERAL}]`    -> `[^.\/]`
//	Windows  QMARK_NO_DOT = `[^.${WIN_SLASH}]`        -> `[^.\\/]`
//
// Windows SLASH_LITERAL is `[\\/]`, a whole character class, so substituting it
// into the POSIX expression yields `[^.[\\/]]` — a class containing a `[`. The
// POSIX spelling coincides with the derivation only because its SLASH_LITERAL
// happens to be a bare escaped character. QMARK is the same shape of leaf
// (`[^/]`, not `[^` + SLASH_LITERAL + `]`), and SEP is unrelated to the others.
//
// # What actually checks these values
//
// Recorded output, not a hand-written table. testdata/emit carries 324 pairs
// whose only option is `windows`, and the emitter gate replays every one against
// upstream's own `state.output`: 648 fields, 0 wrong. That is the evidence, and
// it is why no test in this package restates the sixteen strings — doing so would
// be authoring the answer the fixture already records.
//
// It does not cover all of them, and the shortfall is worth stating rather than
// leaving to be discovered. Eleven values differ between the two tables; seven
// appear in some recorded Windows output (SLASH_LITERAL 117, QMARK 179, STAR 168,
// START_ANCHOR 25, END_ANCHOR 4, NO_DOT_SLASH 4, QMARK_NO_DOT 4). The other four
// — DOTS_SLASH, NO_DOTS, NO_DOTS_SLASH and SEP — appear in none, and not by
// accident: the first three are reachable only under opts.dot or inside
// fastpaths, neither of which is built, and SEP is read nowhere in lib/ at all.
// So there is no untested branch hiding behind them today; they go live the day
// opts.dot lands, and that is the day to re-run the census:
//
//	node -e "const C=require('./tests/original/lib/constants.js'),fs=require('fs');\
//	  const P=C.globChars(false),W=C.globChars(true);\
//	  const recs=fs.readFileSync('testdata/emit/cases.jsonl','utf8').split('\n')\
//	    .filter(Boolean).map(JSON.parse)\
//	    .filter(r=>r.options&&r.options.windows===true&&Object.keys(r.options).length===1&&r.scannerOutput!=null);\
//	  for(const k of Object.keys(P).filter(k=>P[k]!==W[k]))\
//	    console.log(recs.filter(r=>r.scannerOutput.includes(W[k])).length,k)"
//
// The one thing a Go test does pin is the trap above — see
// TestQmarkNoDotIsALeaf, which fails on the derivation that looks correct.
const winSlash = `\\/`

// globChars is one platform's half of the constant set: the values that differ
// between POSIX_CHARS and WINDOWS_CHARS, plus the ones parse() reads off the same
// table and that happen to be equal on both.
//
// Field names are constants.js's, lowercased. Where parse() rebinds one under an
// option — `qmarkNoDot` at parse.js:400, `star` at :401 — the rebinding lives on
// the scanner under the same name upstream gives it, so `s.chars.qmarkNoDot` is
// the constant and `s.qmarkNoDot` is the binding, exactly as upstream shadows it.
//
// Two globChars keys are absent, both because nothing in lib/ reads them:
// QMARK_LITERAL and SEP. `grep -rn "QMARK_LITERAL\|SEP\b" tests/original/lib/ |
// grep -v constants.js` is empty. SEP is the one worth naming — it is the only
// key whose Windows value is a *path* separator (`\`) rather than a regex
// fragment, so an emitter that grew a use for it would be doing something no
// upstream emitter does.
type globChars struct {
	dotLiteral  string
	plusLiteral string

	// slashLiteral, qmark and qmarkNoDot are three of the four leaves; see the
	// package comment above for why the third is not derivable from the first.
	slashLiteral string
	qmark        string
	qmarkNoDot   string

	oneChar     string
	endAnchor   string
	startAnchor string
	dotsSlash   string
	noDot       string
	noDots      string
	noDotSlash  string
	noDotsSlash string
	star        string
}

// globCharsFor is constants.globChars(win32).
//
// Upstream returns one of two frozen module-level objects; this builds the table
// per call, which costs sixteen string concatenations once per parse and removes
// the question of whether anything downstream may write to it.
func globCharsFor(windows bool) globChars {
	c := globChars{
		dotLiteral:   `\.`,
		plusLiteral:  `\+`,
		oneChar:      `(?=.)`,
		slashLiteral: `\/`,
		qmark:        `[^/]`,
		qmarkNoDot:   `[^.\/]`,
	}
	if windows {
		c.slashLiteral = `[` + winSlash + `]`
		c.qmark = `[^` + winSlash + `]`
		c.qmarkNoDot = `[^.` + winSlash + `]`
	}

	// constants.js:18-26, in that order. Every one of these is spelled as the
	// expression upstream uses rather than as a literal, so the two tables cannot
	// drift apart in the port the way they can be made to in constants.js.
	c.endAnchor = `(?:` + c.slashLiteral + `|$)`
	c.startAnchor = `(?:^|` + c.slashLiteral + `)`
	c.dotsSlash = c.dotLiteral + `{1,2}` + c.endAnchor
	c.noDot = `(?!` + c.dotLiteral + `)`
	c.noDots = `(?!` + c.startAnchor + c.dotsSlash + `)`
	c.noDotSlash = `(?!` + c.dotLiteral + `{0,1}` + c.endAnchor + `)`
	c.noDotsSlash = `(?!` + c.dotsSlash + `)`
	c.star = c.qmark + `*?`
	return c
}

// extglobChars is constants.extglobChars(PLATFORM_CHARS) at constants.js:167.
//
// It takes the platform table rather than the scanner because the negate close
// embeds `chars.STAR` — the platform constant, not parse()'s rebound `star`,
// which opts.bash and opts.capture also feed. The two coincide under default
// options and the distinction is upstream's, so it is spelled with the constant.
func (c globChars) extglobChars(ch uint16) (typ, open, closing string, ok bool) {
	switch ch {
	case '!':
		return "negate", `(?:(?!(?:`, `))` + c.star + `)`, true
	case '?':
		return "qmark", `(?:`, `)?`, true
	case '+':
		return "plus", `(?:`, `)+`, true
	case '*':
		return "star", `(?:`, `)*`, true
	case '@':
		return "at", `(?:`, `)`, true
	}
	return "", "", "", false
}
