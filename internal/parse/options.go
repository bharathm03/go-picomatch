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

	// NoExtglob turns off every extglob opener. Five sites test it, all of them
	// `opts.noextglob !== true` and all of them the *first* arm of their branch:
	// parse.js:1023 ("?("), :1054 ("!("), :1072 ("+("), :1096 ("@(") and :1140
	// ("*(", spelled as a regexp over two characters rather than a peek pair).
	// Turning it on does not error — each site falls through to the arm that
	// treats the character as itself.
	NoExtglob bool

	// NoExt is minimatch's spelling of NoExtglob, and it is not an alias. The
	// merge at parse.js:408 is guarded by `typeof opts.noext === 'boolean'`, so
	// `{NoExtglob: true, NoExt: &false}` turns extglobs back *on* where leaving
	// NoExt unset leaves them off. Unset is a third state, hence the pointer.
	NoExt *bool

	// Posix is read twice, with two different tests, so unset is a third state
	// here too — and the two defaults point opposite ways:
	//
	//	:719  `opts.posix !== false` gates the POSIX character-class rewrite
	//	      ("[[:alpha:]]" -> its source), which is therefore live unless the
	//	      caller explicitly passes false.
	//	:751  `opts.posix === true` rewrites a "!" directly after "[" into "^",
	//	      which is therefore dead unless the caller explicitly passes true.
	//
	// Both sites are inside the character-class body branch, twenty lines apart.
	Posix *bool

	// Regex is the same shape as Posix: two reads, two tests, two defaults.
	//
	//	:1077  `opts.regex === false` makes "+" emit PLUS_LITERAL even where the
	//	       preceding token is not "(" — it joins an || whose other half is
	//	       live by default, so false widens an existing arm rather than
	//	       opening a new one.
	//	:1257  `opts.regex === true` gives a star directly after a bracket or
	//	       paren token its own raw value as output, so the "*" reads as the
	//	       caller's quantifier rather than as a glob.
	Regex *bool
}

// isDefault reports whether o selects every upstream default, so a caller can
// tell "the zero value" from "explicitly configured" without comparing fields.
func (o Options) isDefault() bool { return o == Options{} }
