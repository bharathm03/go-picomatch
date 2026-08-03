package parse

// Options are the keys lib/parse.js reads *and this package answers*, and only
// those.
//
// That is a narrower rule than internal/scan.Options follows, and deliberately.
// scan.js is ported in full, so listing its read keys and listing its answered
// keys give the same set. parse.js is not: roughly forty `opts.` sites are
// transcribed but marked rather than written, and a field here for one of them
// would be a field the caller may set and this package will silently ignore.
//
// An ignored option is the failure mode the whole repo is built to rule out. It
// does not error, it does not decline — it returns a plausible token stream for
// the wrong configuration, which scores as a pass wherever the two configurations
// happen to agree. So a key earns a field on the day its branch is written, not
// before, and until then a caller that needs it has no way to ask, which is the
// correct answer. DECISIONS.md §9.
//
// `grep -n "opts\." internal/parse/*.go` lists the sites still waiting.
type Options struct {
	// Windows selects WINDOWS_CHARS over POSIX_CHARS — constants.globChars at
	// constants.js:181, read at parse.js:377 and :1351 and nowhere else.
	//
	// It is never inferred from the host. Upstream defaults it from the running
	// platform at its package entry point, which this port does not do: 17% of
	// paired fixtures genuinely diverge between platforms and both sides are
	// contract, so the axis has to be an input rather than an accident of where
	// the tests run. DECISIONS.md §16.
	Windows bool

	// Bash selects globstar(opts) over STAR for the star binding — parse.js:401
	// — and gates three more branches read nowhere else: the escaped-slash
	// arm at :675 (a backslash before "/" is not silently dropped), the
	// globstar-arms early-out at :1156 (a star that does not open a path
	// segment, or is followed by more non-slash text, stays a plain star with
	// an empty output), and the star-token arm at :1248 (the token's output is
	// always ".*?", gaining the nodot prefix at bos/slash).
	Bash bool

	// StrictSlashes gates two sites read nowhere else: the trailing "/**"
	// globstar arm at parse.js:1193 (the closing alternative is ")" rather
	// than "|$)", dropping the end-of-string escape hatch), and the
	// unclosed-bracket/star cleanup at parse.js:1304 (no synthetic
	// maybe_slash token is pushed when strictSlashes is true).
	StrictSlashes bool

	// Dot reshapes two of the three pre-loop bindings and gates two more sites:
	//
	//	globstarBody :396  opts.dot selects DOTS_SLASH over DOT_LITERAL — every
	//	                    globstar arm reads this binding, so the effect is
	//	                    not local to any one of them.
	//	nodot        :399  opts.dot selects "" over NO_DOT.
	//	":1041"      the leading-"?" guard (prev.type slash/bos) only takes the
	//	             QMARK_NO_DOT arm when opts.dot !== true; under opts.dot it
	//	             is skipped entirely and falls through to plain QMARK.
	//	":1270"      the star-token leading-position guard gains a middle arm:
	//	             prev.type === "dot" still takes NO_DOT_SLASH unconditionally,
	//	             but opts.dot === true now takes NO_DOTS_SLASH ahead of the
	//	             plain nodot fallback.
	//
	// qmarkNoDot (parse.js:400) is still deliberately absent — see the doc
	// comment on scanner's star/nodot/globstarBody fields — because its only
	// reader (:620) is the inline fast path this package does not have.
	Dot bool
}

// isDefault reports whether o selects every upstream default, so a caller can
// tell "the zero value" from "explicitly configured" without comparing fields.
func (o Options) isDefault() bool { return o == Options{} }
