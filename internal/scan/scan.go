// Package scan is the port of tests/original/lib/scan.js: it splits a glob into
// the literal path prefix that precedes any glob syntax and the pattern that
// follows, without compiling anything.
//
// It is a separate upstream entry point with its own state machine, and it
// shares no code with parse(). The two disagree by design in places — scan()
// stops at the first glob character unless asked not to, and its notion of a
// brace or an extglob is a cheap approximation of the parser's — so nothing
// here is a shortcut to internal/parse and nothing there should be reused to
// answer a question about scanning.
//
// # This is internal on purpose
//
// The exported shape a caller sees is picomatch.ScanResult. This package exists
// so the scanner can be measured against its own 586 recorded cases without the
// root package depending on it before the matcher is ready to be wired up.
package scan

// Character codes, from tests/original/lib/constants.js:122-154. Named after
// their upstream constants so a branch can be read against the source.
const (
	charLeftParen         = 40  // (
	charRightParen        = 41  // )
	charAsterisk          = 42  // *
	charPlus              = 43  // +
	charComma             = 44  // ,
	charDot               = 46  // .
	charAt                = 64  // @
	charBackwardSlash     = 92  // \
	charExclamationMark   = 33  // !
	charForwardSlash      = 47  // /
	charLeftCurlyBrace    = 123 // {
	charLeftSquareBracket = 91  // [
	charQuestionMark      = 63  // ?
	charRightCurlyBrace   = 125 // }
	charRightSquareBrack  = 93  // ]
)

// Options are the keys lib/scan.js reads, and only those.
//
// scan() also reads `opts.tokens`, which is absent here: it asks for the
// per-segment token list, and picomatch.ScanResult has nowhere to put one.
// DECISIONS.md §13.
type Options struct {
	// NoExt disables extglob detection (scan.js:171), and additionally clears
	// isGlob and isExtglob after the loop (scan.js:288).
	NoExt bool
	// NoNegate disables the leading-"!" prologue (scan.js:250).
	NoNegate bool
	// NoParen disables the bare-parenthesis branch (scan.js:256).
	NoParen bool
	// Unescape strips backslashes from base and glob (scan.js:319).
	Unescape bool
	// ScanToEnd consumes the whole input rather than stopping at the first glob
	// character (scan.js:53).
	ScanToEnd bool
	// Parts returns the input's segments, and implies ScanToEnd (scan.js:53).
	Parts bool
}

// Result mirrors picomatch.ScanResult field for field, so wiring the two
// together is an assignment rather than a translation.
type Result struct {
	Input  string
	Prefix string
	Start  int
	Base   string
	Glob   string

	IsBrace    bool
	IsBracket  bool
	IsGlob     bool
	IsGlobstar bool
	IsExtglob  bool

	Negated        bool
	NegatedExtglob bool

	// Parts and Slashes are populated only under Options.Parts, matching
	// upstream, which attaches them to the returned state only under
	// `opts.parts` or `opts.tokens` (scan.js:350, :384).
	Parts   []string
	Slashes []int
}

// isPathSeparator is scan.js:22.
func isPathSeparator(code int) bool {
	return code == charForwardSlash || code == charBackwardSlash
}

// Scan is lib/scan.js's exported function, transcribed arm for arm in upstream's
// order.
//
// Upstream never throws from here — there is no length guard and no syntax it
// rejects — so there is no error return. Every input has an answer.
func Scan(input string, opts Options) Result {
	in := encode(input)
	str := in

	// length is the index of the last code unit, not the count: upstream's loop
	// is `while (index < length)` with `length = input.length - 1`, and eos() is
	// `index >= length`. Both stay in those terms here.
	length := len(str) - 1
	scanToEnd := opts.Parts || opts.ScanToEnd

	var slashes []int
	var parts []string

	index := -1
	start := 0
	lastIndex := 0

	var isBrace, isBracket, isGlob, isExtglob, isGlobstar bool
	var braceEscaped, backslashes, negated, negatedExtglob, finished bool
	braces := 0

	prev := charNone
	code := charNone

	eos := func() bool { return index >= length }
	peek := func() int { return str.at(index + 1) }
	advance := func() int {
		prev = code
		index++
		return str.at(index)
	}

	for index < length {
		code = advance()

		if code == charBackwardSlash {
			backslashes = true
			// Not guarded by eos(): a trailing backslash advances past the end
			// and leaves code as NaN, which matches nothing below.
			code = advance()

			if code == charLeftCurlyBrace {
				braceEscaped = true
			}
			continue
		}

		if braceEscaped || code == charLeftCurlyBrace {
			braces++

			for !eos() {
				code = advance()
				if !truthy(code) {
					break
				}

				if code == charBackwardSlash {
					backslashes = true
					advance()
					continue
				}

				if code == charLeftCurlyBrace {
					braces++
					continue
				}

				// `code === CHAR_DOT && (code = advance()) === CHAR_DOT`: the
				// second dot is consumed by the test itself, and code keeps the
				// character that was read whether or not it was a dot. When it
				// was not, the comma test below sees that new character — so
				// "{a.,b}" is a brace, decided by the comma the dot pulled in.
				if !braceEscaped && code == charDot {
					code = advance()
					if code == charDot {
						isBrace = true
						isGlob = true
						finished = true

						if scanToEnd {
							continue
						}
						break
					}
				}

				if !braceEscaped && code == charComma {
					isBrace = true
					isGlob = true
					finished = true

					if scanToEnd {
						continue
					}
					break
				}

				if code == charRightCurlyBrace {
					braces--

					if braces == 0 {
						braceEscaped = false
						isBrace = true
						finished = true
						break
					}
				}
			}

			if scanToEnd {
				continue
			}
			break
		}

		if code == charForwardSlash {
			slashes = append(slashes, index)

			if finished {
				continue
			}
			if prev == charDot && index == start+1 {
				start += 2
				continue
			}

			lastIndex = index + 1
			continue
		}

		if !opts.NoExt {
			isExtglobChar := code == charPlus ||
				code == charAt ||
				code == charAsterisk ||
				code == charQuestionMark ||
				code == charExclamationMark

			if isExtglobChar && peek() == charLeftParen {
				isGlob = true
				isExtglob = true
				finished = true
				if code == charExclamationMark && index == start {
					negatedExtglob = true
				}

				if scanToEnd {
					for !eos() {
						code = advance()
						if !truthy(code) {
							break
						}

						if code == charBackwardSlash {
							backslashes = true
							code = advance()
							continue
						}

						if code == charRightParen {
							isGlob = true
							finished = true
							break
						}
					}
					continue
				}
				break
			}
		}

		if code == charAsterisk {
			if prev == charAsterisk {
				isGlobstar = true
			}
			isGlob = true
			finished = true

			if scanToEnd {
				continue
			}
			break
		}

		if code == charQuestionMark {
			isGlob = true
			finished = true

			if scanToEnd {
				continue
			}
			break
		}

		if code == charLeftSquareBracket {
			// The bracket loop reads into `next`, leaving `code` on the "["
			// itself. `prev` still moves, so a "*" after "[a]" does not see one.
			for !eos() {
				next := advance()
				if !truthy(next) {
					break
				}

				if next == charBackwardSlash {
					backslashes = true
					advance()
					continue
				}

				if next == charRightSquareBrack {
					isBracket = true
					isGlob = true
					finished = true
					break
				}
			}

			if scanToEnd {
				continue
			}
			break
		}

		if !opts.NoNegate && code == charExclamationMark && index == start {
			negated = true
			start++
			continue
		}

		if !opts.NoParen && code == charLeftParen {
			isGlob = true

			if scanToEnd {
				for !eos() {
					code = advance()
					if !truthy(code) {
						break
					}

					// Upstream tests for "(" here, not "\\" as the three
					// sibling loops do, and sets backslashes on it. Faithful:
					// "(a(b)" leaves finished false where the symmetrical
					// reading would set it.
					if code == charLeftParen {
						backslashes = true
						code = advance()
						continue
					}

					if code == charRightParen {
						finished = true
						break
					}
				}
				continue
			}
			break
		}

		if isGlob {
			finished = true

			if scanToEnd {
				continue
			}
			break
		}
	}

	if opts.NoExt {
		isExtglob = false
		isGlob = false
	}

	// base starts as the whole input and is used for its emptiness alone in the
	// test below; str is what the slices are taken from, and has had the prefix
	// removed by then.
	base := str
	prefix := units{}
	glob := units{}

	if start > 0 {
		prefix = str[:start]
		str = str[start:]
		lastIndex -= start
	}

	switch {
	case len(base) > 0 && isGlob && lastIndex > 0:
		base = str[:lastIndex]
		glob = str[lastIndex:]
	case isGlob:
		base = units{}
		glob = str
	default:
		base = str
	}

	if len(base) > 0 && !base.equalString("/") && !equalUnits(base, str) {
		if isPathSeparator(base.at(len(base) - 1)) {
			base = base[:len(base)-1]
		}
	}

	if opts.Unescape {
		if len(glob) > 0 {
			glob = removeBackslashes(glob)
		}
		if len(base) > 0 && backslashes {
			base = removeBackslashes(base)
		}
	}

	state := Result{
		Prefix:         prefix.String(),
		Input:          input,
		Start:          start,
		Base:           base.String(),
		Glob:           glob.String(),
		IsBrace:        isBrace,
		IsBracket:      isBracket,
		IsGlob:         isGlob,
		IsExtglob:      isExtglob,
		IsGlobstar:     isGlobstar,
		Negated:        negated,
		NegatedExtglob: negatedExtglob,
	}

	if opts.Parts {
		// prevIndex is tested for truthiness, not for presence, so a leading
		// slash — index 0 — reads as "no previous slash" on the next iteration
		// and after the loop. "/a" therefore yields no parts at all.
		prevIndex := 0

		for idx := 0; idx < len(slashes); idx++ {
			n := start
			if prevIndex != 0 {
				n = prevIndex + 1
			}
			i := slashes[idx]

			// Segments are cut from the original input, prefix included, which
			// is why n starts at start rather than 0. String.prototype.slice
			// yields "" when n > i; a Go slice expression would panic.
			value := units{}
			if n < i {
				value = in[n:i]
			}

			if idx != 0 || len(value) != 0 {
				parts = append(parts, value.String())
			}
			prevIndex = i
		}

		if prevIndex != 0 && prevIndex+1 < len(in) {
			parts = append(parts, in[prevIndex+1:].String())
		}

		// Both are declared as arrays upstream (scan.js:53-55) and attached
		// whatever the loop found, so a pattern with no separator reports `[]`
		// and not a missing field. A nil Go slice is the idiomatic reading and
		// the wrong one: it marshals to null rather than [], and it makes
		// `Parts != nil` — the obvious "was segmentation run" test — false for
		// every glob without a "/". Nothing catches the difference, because
		// sameSlice in scan_conformance_test.go compares by length and no
		// recorded case has an empty parts array.
		if slashes == nil {
			slashes = []int{}
		}
		if parts == nil {
			parts = []string{}
		}

		state.Slashes = slashes
		state.Parts = parts
	}

	return state
}
