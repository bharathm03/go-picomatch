package scan

import "strings"

// The parts of tests/original/lib/utils.js that lib/scan.js and the scan entry
// point need. Nothing else from that file is ported here: the regex helpers
// belong to the emitter, and removePrefix/escapeLast are parse()'s.

// Basename is utils.basename (utils.js:63).
//
//	const segs = path.split(windows ? /[\\/]/ : '/');
//	const last = segs[segs.length - 1];
//	if (last === '') return segs[segs.length - 2];
//	return last;
//
// The trailing-separator case is a lookback, not a trim: "/a/b/c/" splits to
// ["", "a", "b", "c", ""] and the answer is the second-to-last segment. Two
// trailing separators therefore give "" — segs[length - 2] is itself empty —
// and upstream has no special case for it.
//
// Splitting is on ASCII separators only, so byte, rune and code-unit readings
// cannot disagree and this one works on the Go string directly. Doing it on
// units would be worse rather than merely equivalent: it would round-trip the
// segments through encode/String and lose any invalid UTF-8 the caller passed.
//
// An empty path is the one input with no Go answer: it splits to a single empty
// segment, so upstream indexes position -1 and returns undefined. The recorded
// cases (12 of them, all absolute paths) never reach it. DECISIONS.md §13.
func Basename(path string, windows bool) string {
	segs := splitSeparators(path, windows)

	last := segs[len(segs)-1]
	if last == "" {
		if len(segs) < 2 {
			return ""
		}
		return segs[len(segs)-2]
	}
	return last
}

// splitSeparators is String.prototype.split with upstream's two separator sets.
// Empty segments are kept, which is what makes the trailing-separator lookback
// above work.
func splitSeparators(path string, windows bool) []string {
	if !windows {
		return strings.Split(path, "/")
	}

	segs := []string{}
	start := 0
	for i := 0; i < len(path); i++ {
		if path[i] == '/' || path[i] == '\\' {
			segs = append(segs, path[start:i])
			start = i + 1
		}
	}
	return append(segs, path[start:])
}

// removeBackslashes is utils.removeBackslashes (utils.js:30):
//
//	str.replace(/(?:\[.*?[^\\]\]|\\(?=.))/g, m => (m === '\\' ? '' : m))
//
// Two alternatives, and only the second one is ever rewritten. A bracket
// expression matches as a whole and is put back unchanged, so backslashes
// inside `[...]` survive; anywhere else a backslash followed by a character is
// deleted. A trailing backslash has nothing after it for the lookahead, so it
// stays.
//
// It is hand-matched rather than handed to regexp for two reasons. `(?=.)` is
// lookahead, which RE2 does not have, and `.` excludes JavaScript's four line
// terminators while the negated class `[^\\]` in the same pattern does not — a
// distinction `regexp`'s flags express in neither direction at once.
func removeBackslashes(u units) units {
	out := make(units, 0, len(u))

	for i := 0; i < len(u); {
		// The alternatives cannot both apply at one position — one needs "[",
		// the other "\\" — so JavaScript's leftmost-first ordering is not
		// observable here, and neither is the order of these two tests.
		if end, ok := bracketRun(u, i); ok {
			out = append(out, u[i:end]...)
			i = end
			continue
		}
		if u[i] == '\\' && i+1 < len(u) && !isLineTerminator(u[i+1]) {
			// The match is the backslash alone; the lookahead consumes nothing.
			i++
			continue
		}
		out = append(out, u[i])
		i++
	}
	return out
}

// bracketRun matches /\[.*?[^\\]\]/ anchored at i, returning the index one past
// the match. `.*?` is lazy, so the shortest run wins.
func bracketRun(u units, i int) (int, bool) {
	if u[i] != '[' {
		return 0, false
	}

	for j := i + 1; j+1 < len(u); j++ {
		// `[^\\]` matches anything but a backslash, line terminators included.
		if u[j] != '\\' && u[j+1] == ']' {
			return j + 2, true
		}
		// Failing that, u[j] has to be swallowed by `.*?`, which is `.` and so
		// cannot take a line terminator.
		if isLineTerminator(u[j]) {
			return 0, false
		}
	}
	return 0, false
}
