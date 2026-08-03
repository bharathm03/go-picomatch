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
}

// isDefault reports whether o selects every upstream default, so a caller can
// tell "the zero value" from "explicitly configured" without comparing fields.
func (o Options) isDefault() bool { return o == Options{} }
