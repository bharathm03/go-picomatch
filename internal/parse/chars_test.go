package parse

import (
	"strings"
	"testing"
)

// TestQmarkNoDotIsALeaf pins the one place where the obvious simplification of
// globCharsFor is wrong.
//
// constants.js:25 spells POSIX QMARK_NO_DOT as `[^.${SLASH_LITERAL}]`, which
// invites folding it into the derivation block alongside END_ANCHOR and
// START_ANCHOR. On POSIX that is correct and stays correct. On Windows it is not:
// WINDOWS_CHARS (constants.js:61) spells it `[^.${WIN_SLASH}]`, using the raw
// two-character class *body*, where SLASH_LITERAL is the whole class `[\\/]`.
// Substituting one into the other nests a bracket inside a bracket.
//
// The emitter gate would catch it, but only just — four of the 2,038 recorded
// pairs contain the value at all, so a regression here reads as a rounding error
// in a percentage rather than as the structural mistake it is. That is why this
// is a named test and not left to the fixture.
func TestQmarkNoDotIsALeaf(t *testing.T) {
	win := globCharsFor(true)

	naive := `[^.` + win.slashLiteral + `]`
	if win.qmarkNoDot == naive {
		t.Fatalf("qmarkNoDot was derived from slashLiteral (%q); constants.js:61 uses WIN_SLASH, not SLASH_LITERAL", naive)
	}
	if want := `[^.` + winSlash + `]`; win.qmarkNoDot != want {
		t.Errorf("windows qmarkNoDot = %q, want %q", win.qmarkNoDot, want)
	}

	// The failure the naive derivation produces, named so the next reader does not
	// have to reconstruct it: a "[" anywhere but position 0 is a class opened
	// inside a class.
	if strings.Contains(win.qmarkNoDot[1:], "[") {
		t.Errorf("windows qmarkNoDot = %q opens a character class inside a character class", win.qmarkNoDot)
	}
}

// TestGlobCharsVaryWherePlatformsDo checks that every value constants.js gives a
// Windows override actually changes when the flag flips, and that the ones it
// does not override do not.
//
// A leaf left out of globCharsFor's `if windows` block is invisible in isolation:
// it keeps its POSIX value, the table still builds, and every derivation that
// does not read it stays correct. What surfaces is a handful of wrong outputs
// somewhere downstream. The two lists below are transcribed from constants.js:52
// — the keys WINDOWS_CHARS assigns, minus the two globChars keys this port does
// not carry (QMARK_LITERAL and SEP; see chars.go) — and the keys it inherits from
// the spread.
func TestGlobCharsVaryWherePlatformsDo(t *testing.T) {
	posix, win := globCharsFor(false), globCharsFor(true)

	// constants.js:55-65, less SEP.
	varying := map[string][2]string{
		"slashLiteral": {posix.slashLiteral, win.slashLiteral},
		"qmark":        {posix.qmark, win.qmark},
		"star":         {posix.star, win.star},
		"dotsSlash":    {posix.dotsSlash, win.dotsSlash},
		"noDots":       {posix.noDots, win.noDots},
		"noDotSlash":   {posix.noDotSlash, win.noDotSlash},
		"noDotsSlash":  {posix.noDotsSlash, win.noDotsSlash},
		"qmarkNoDot":   {posix.qmarkNoDot, win.qmarkNoDot},
		"startAnchor":  {posix.startAnchor, win.startAnchor},
		"endAnchor":    {posix.endAnchor, win.endAnchor},
	}
	for name, v := range varying {
		if v[0] == v[1] {
			t.Errorf("%s is %q on both platforms; constants.js:52 overrides it", name, v[0])
		}
	}

	// NO_DOT is in the WINDOWS_CHARS literal but its value is identical to the
	// POSIX one — it reads DOT_LITERAL and nothing else. Listing it above would
	// be wrong, so it is asserted the other way round, which also documents why
	// it is missing from that map.
	same := map[string][2]string{
		"dotLiteral":  {posix.dotLiteral, win.dotLiteral},
		"plusLiteral": {posix.plusLiteral, win.plusLiteral},
		"oneChar":     {posix.oneChar, win.oneChar},
		"noDot":       {posix.noDot, win.noDot},
	}
	for name, v := range same {
		if v[0] != v[1] {
			t.Errorf("%s differs between platforms (%q vs %q); constants.js builds it from DOT_LITERAL alone", name, v[0], v[1])
		}
	}
}

// TestExtglobCloseUsesThePlatformStar pins constants.js:169, where the negate
// close embeds chars.STAR.
//
// parse() also has a local `star` that starts life equal to STAR and is then
// rebound by opts.bash and opts.capture (parse.js:401-405). Reading that one here
// would be invisible under default options on either platform, and wrong the day
// opts.bash lands. The test states the dependency that makes it wrong: the close
// has to track the platform, not the binding.
func TestExtglobCloseUsesThePlatformStar(t *testing.T) {
	for _, windows := range []bool{false, true} {
		c := globCharsFor(windows)
		_, _, closing, ok := c.extglobChars('!')
		if !ok {
			t.Fatalf("windows=%v: extglobChars('!') not recognised", windows)
		}
		if !strings.HasSuffix(closing, c.star+")") {
			t.Errorf("windows=%v: negate close %q does not end in the platform STAR %q", windows, closing, c.star)
		}
	}
}
