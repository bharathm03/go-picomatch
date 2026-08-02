package parse

import "sort"

// Braces: the "{a,b}" list and the "{a..b}" range.
//
// The main loop's three arms stay in scanner.go, in upstream's order. What is
// here is expandRange (parse.js:22-38), which is the only helper the brace
// branch has and the only place in parse() whose answer is decided by a
// *regular expression engine* rather than by the grammar — see ecmaregexp.go
// and DECISIONS.md §15.

// expandRange is parse.js:22-38 under default options.
//
// Three details do not survive paraphrase:
//
//   - the sort runs *before* the range is built, so "{z..a}" is "[a-z]" and not
//     an out-of-order range. It is JavaScript's default comparator, which
//     compares strings by UTF-16 code unit — the same order units already has.
//   - the class is built and then **compiled to find out whether it is legal**.
//     A range V8 rejects falls back to the escaped ends joined by "..", so the
//     brace token's output depends on what the host engine accepts.
//   - the fallback escapes each end with utils.escapeRegex and joins with "..",
//     which is two characters, not the one-character range separator.
//
// opts.expandRange (parse.js:23) is a caller-supplied *function* that replaces
// the whole helper. Options do not reach this package, and there is no
// Options field for it — DECISIONS.md §2 requires the key to be read by
// upstream, which it is, so the absence is a gap rather than a decision; it is
// recorded in DECISIONS.md §15 rather than assumed away here.
func expandRange(args []units) units {
	sortJS(args)

	value := units{'['}
	for i, a := range args {
		if i > 0 {
			value = append(value, '-')
		}
		value = append(value, a...)
	}
	value = append(value, ']')

	if ecmaRegExpValid(value) {
		return value
	}

	var fallback units
	for i, a := range args {
		if i > 0 {
			fallback = append(fallback, '.', '.')
		}
		fallback = append(fallback, escapeRegex(a)...)
	}
	return fallback
}

// sortJS is Array.prototype.sort with no comparator: elements are compared as
// strings, which for a sequence of UTF-16 code units is a plain lexicographic
// comparison of those units. ECMAScript has required the sort to be stable
// since ES2019, so SliceStable rather than Slice.
func sortJS(args []units) {
	sort.SliceStable(args, func(i, j int) bool { return unitsLess(args[i], args[j]) })
}

func unitsLess(a, b units) bool {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}
