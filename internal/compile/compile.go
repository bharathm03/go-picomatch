// Package compile ports picomatch.js's compile layer: the wrap compileRe puts
// round a parser's output, the RegExp construction toRegex does with it, and the
// `source` and `flags` strings that come back out.
//
// # This is a string layer, not a regex backend
//
// DECISIONS.md §1 rules Go's regexp out as a *matching* backend — it is RE2, and
// picomatch's output relies on lookaround in almost every non-trivial pattern.
// §17 draws the other half of that line: the emitted source is a recorded value
// like any other, producing it is concatenation, and nothing here compiles or
// matches anything. [ToRegex] is the one function that asks a question a regex
// engine would normally answer, and it asks internal/ecmaregexp — a transcribed
// grammar — rather than regexp.Compile, which would answer for the wrong
// language.
//
// # Three rules cover all 2,028 recorded compiled records
//
//	^(?:output)$, or ^(?!^(?:output)$).*$ when negated      2,020   picomatch.js:273-276
//	the same, then EscapeRegExpPattern ("/" -> "\/")            5   trap #52
//	$^, because the RegExp constructor rejected the source      3   trap #53
//
// # What this package cannot do on its own
//
// compileRe's input is `state.output`, and which state that is depends on which
// of upstream's three parsers makeRe used (picomatch.js:312-318). Two of the
// three do not exist in the port yet, so [PathFullScanner] exists to say when
// the answer is decidable *without* them, and callers must not compile output
// they had to guess at. That is the whole reason `source` is answerable on 1,134
// of the 2,028 records rather than all of them: the missing 894 are blocked on
// parse.fastpaths and the inline fast path, not on anything here.
package compile

import (
	"strings"
	"unicode/utf16"

	"github.com/bharathm03/go-picomatch/internal/ecmaregexp"
)

// Options are the keys upstream reads below the matcher and at or after
// compileRe. There are four, and every one of them is read exactly once.
//
// The set is this small because the compile layer is downstream of every
// decision the scanner makes: by the time compileRe runs, the ~40 keys
// lib/parse.js reads have already been spent on producing `output`.
type Options struct {
	// Contains drops both anchors at picomatch.js:270-271. It does NOT drop the
	// negation wrap at :274 — that one is unconditional — so `contains` with a
	// negated pattern still yields `^(?!(?:X)).*$`.
	//
	// No corpus record sets it. It has a field anyway because the branch that
	// reads it is written here; leaving it out would hard-code the arm rather
	// than choose it.
	Contains bool

	// NoCase is picomatch.js:343's fallback flag, and Flags is what overrides it.
	// The precedence is JavaScript truthiness — `opts.flags || (opts.nocase ? 'i'
	// : '')` — so an empty Flags falls through to NoCase exactly as an absent one
	// does. That is why Flags is a plain string where nearly every other option in
	// this port is a pointer: "" and absent really do behave identically here, so
	// there is no third state to preserve.
	NoCase bool
	Flags  string

	// NoFastpaths is `opts.fastpaths === false`, read at picomatch.js:312 and
	// again at parse.js:606. Upstream defaults fast paths ON, so the Go zero value
	// has to mean "on" and the field is spelled negatively — the same reason
	// internal/parse.Options has NoExtglob rather than Extglob.
	//
	// Only [PathFullScanner] reads it here. No corpus record sets the key, which
	// is why the two sites can be trusted to agree; docs/transcription-traps.md
	// #50 and #51 are the places where reading the *same* option twice does not.
	NoFastpaths bool
}

// Path values, matching internal/emitcase's recorded vocabulary.
const (
	// PathNone means the full scanner ran and its output was compiled.
	PathNone = "none"
	// PathTop means parse.fastpaths() returned output and parse() never ran.
	PathTop = "top"
	// PathInline means parse() returned from its own fast path at parse.js:606.
	PathInline = "inline"
)

// PathFullScanner reports whether neither of upstream's two fast paths can be
// entered for this input, so makeRe must reach the full scanner and the path is
// [PathNone].
//
// It answers only in that direction, and the asymmetry is the point. Both fast
// paths are *guarded* by cheap syntactic predicates, but only one of the two
// outcomes follows from a predicate alone:
//
//   - false here means a fast path was entered, and which path makeRe ends up
//     reporting depends on what that path RETURNED. parse.fastpaths is called at
//     picomatch.js:313 and its result tested at :316, so an eligible pattern
//     whose fastpaths output is falsy still falls through to the scanner. 382
//     corpus patterns are eligible and 25 actually take it. Nothing short of
//     running parse.fastpaths can tell those apart.
//   - true means neither guard opened, so neither path ran, so there is nothing
//     to have returned. That is decidable from the input and the options alone,
//     and it holds on 1,134 of the 2,028 compiled records — every one of which
//     records path "none".
//
// A caller that treated false as "some other path" would be guessing between
// top and inline on exactly the records where the difference is structural:
// the inline path's output is ALREADY wrapped by utils.wrapOutput inside
// parse() (parse.js:653), so compileRe wraps it a second time and the source
// reads ^(?:^(?:foo)$)$.
func PathFullScanner(input string, opts Options) bool {
	if opts.NoFastpaths {
		// Both guards are off, so parse() runs its full scanner either way.
		return true
	}
	return !fastpathEligible(input) && !inlineEligible(input)
}

// fastpathEligible is picomatch.js:312's half of the guard: the first code unit
// is "." or "*".
//
// Byte indexing is safe for exactly this test and not in general. A UTF-8 lead
// byte is >= 0xC0 and a continuation byte >= 0x80, so no byte of a non-ASCII
// character can equal '.' or '*'; the first byte is the first code unit whenever
// either matches. The same is NOT true of anything that counts positions —
// DECISIONS.md §8 — which is why internal/parse holds units and this file only
// ever asks membership questions.
func fastpathEligible(input string) bool {
	return strings.HasPrefix(input, ".") || strings.HasPrefix(input, "*")
}

// inlineEligible is parse.js:606's half: `!/(^[*!]|[/()[\]{}"])/.test(input)`.
//
// Transcribed as an alternation of two independent tests rather than as a regex,
// because that is what it is: a leading "*" or "!", OR any one of seven
// characters anywhere. Reading it as a single character-set test over the whole
// string — the obvious simplification — would wrongly admit "a*b", where the "*"
// is not leading and none of the seven appear.
func inlineEligible(input string) bool {
	if strings.HasPrefix(input, "*") || strings.HasPrefix(input, "!") {
		return false
	}
	return !strings.ContainsAny(input, `/()[]{}"`)
}

// Source is compileRe's wrap, picomatch.js:270-276.
//
// The two wraps are applied in that order and the outer one is NOT gated on
// Contains, so a negated pattern under `contains` still gets its anchors from
// the negation and not from the inner wrap. The order also means the recorded
// source for a negated pattern nests: ^(?!^(?:X)$).*$, with the inner anchors
// intact.
//
// This is a different wrap from utils.wrapOutput's (?:^(?!X).*$), which runs
// inside parse() and reaches compileRe already applied. The two are reachable on
// different paths and are not interchangeable — internal/emitcase.Case.Negated
// says so at the fixture end.
func Source(output string, negated bool, opts Options) string {
	prepend, suffix := "^", "$"
	if opts.Contains {
		prepend, suffix = "", ""
	}

	source := prepend + "(?:" + output + ")" + suffix
	if negated {
		source = "^(?!" + source + ").*$"
	}
	return source
}

// ToRegex is picomatch.js:341-348: it reports the `source` and `flags` a
// compiled RegExp would carry, without compiling one.
//
// Two things here are not the sentence "wrap it and read it back", and both are
// recorded facts rather than deductions — docs/transcription-traps.md #52 and
// #53:
//
//   - A throw does not escape. toRegex catches the SyntaxError and returns /$^/,
//     a regex that matches nothing, so an uncompilable pattern answers false to
//     everything instead of failing. Its source is the literal "$^" and its flags
//     are "" — the requested flags are discarded with the regex that would have
//     carried them, which is why this function returns both or neither.
//   - RegExp.prototype.source is a SERIALISATION, not the string handed to the
//     constructor. See [escapeRegExpPattern].
//
// # opts.debug is not implemented
//
// picomatch.js:346 rethrows instead of returning /$^/ when `opts.debug === true`.
// That arm is deliberately absent rather than approximated: reproducing it means
// reproducing V8's SyntaxError message verbatim ("Invalid regular expression:
// /…/: Unterminated group"), which is an engine's diagnostic text and not
// something the grammar in internal/ecmaregexp knows. No corpus record sets
// `debug`, so nothing is scored on it; a record that did would have to be
// declined rather than answered.
func ToRegex(source string, opts Options) (src, flags string) {
	if !ecmaregexp.Valid(utf16.Encode([]rune(source))) {
		return "$^", ""
	}

	flags = opts.Flags
	if flags == "" && opts.NoCase {
		flags = "i"
	}
	return escapeRegExpPattern(source), flags
}

// escapeRegExpPattern is ECMAScript's EscapeRegExpPattern (ES2024 22.2.6.13.1)
// as V8 implements it, restricted to what the corpus can reach.
//
// RegExp.prototype.source has to round-trip through a /…/ literal, so every "/"
// that would end the literal early is escaped. Inside a character class a "/"
// cannot end the literal, so it is left alone — which is the whole difference:
//
//	new RegExp('foo/bar').source  === "foo\\/bar"
//	new RegExp('[/]').source      === "[/]"
//
// 5 of the 2,028 recorded sources differ from the wrapped string for this reason
// and no other, all of them containing "[/]". An emitter written from
// picomatch.js alone reproduces the other 2,023 and then disagrees on those 5,
// which is why this is trap #52 rather than a footnote.
//
// # What is left out, and why that is safe here
//
// The spec also requires LineTerminators to be escaped, so a literal newline in
// a pattern comes back as the two characters "\n". No recorded string in
// testdata/emit contains one:
//
//	node -e "const r=require('fs').readFileSync('testdata/emit/cases.jsonl','utf8').trim().split(String.fromCharCode(10)).map(JSON.parse);const LT=new RegExp('[' + String.fromCharCode(10,13,8232,8233) + ']');let n=0;for(const x of r)for(const s of [x.source,x.scannerOutput,x.output,x.fastpathOutput])if(typeof s==='string'&&LT.test(s))n++;console.log(n)"
//
// prints 0. The rule is omitted rather than written blind because there is no
// recorded case to check it against, and an unverifiable branch is the thing
// this repo exists not to ship. Add it with a fixture, not with a guess.
//
// The empty-pattern case (V8 returns "(?:)") is unreachable for the same kind of
// reason and a stronger one: [Source] always contributes at least "(?:)" itself.
func escapeRegExpPattern(src string) string {
	// The scan is over bytes, and safely so: every character it acts on is
	// ASCII, and no byte of a multi-byte UTF-8 sequence can collide with one.
	// A backslash consumes the byte after it, and inside a multi-byte sequence
	// that byte is a continuation byte which no branch below looks at.
	if !strings.Contains(src, "/") {
		return src
	}

	var b strings.Builder
	b.Grow(len(src) + 8)

	inClass := false
	for i := 0; i < len(src); i++ {
		switch c := src[i]; c {
		case '\\':
			// An escape covers whatever follows it, including a "]" that would
			// otherwise close the class and a "/" that would otherwise be
			// escaped again. A trailing backslash is copied alone.
			b.WriteByte(c)
			if i+1 < len(src) {
				i++
				b.WriteByte(src[i])
			}
		case '[':
			inClass = true
			b.WriteByte(c)
		case ']':
			inClass = false
			b.WriteByte(c)
		case '/':
			if !inClass {
				b.WriteByte('\\')
			}
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
