# Decisions

Every non-trivial divergence from upstream picomatch v4.0.5, with the evidence
behind it. A decision belongs here if a reader comparing the two implementations
side by side would otherwise think the port had simply got it wrong.

Each entry states what upstream does, what this port does, and how to re-check
the claim rather than take it on trust.

---

## 1. No compiled regular expression is exposed

**Upstream.** `picomatch.makeRe(pattern, options)` compiles a pattern to a
JavaScript `RegExp` and returns it. The matcher itself is a closure over that
`RegExp`, and `.source` is inspectable.

**This port.** There is no `MakeRe`, no `Pattern.Regexp()`, and no
`*regexp.Regexp` anywhere in the public API. Matching goes through `Pattern` and
nothing else.

**Why.** Go's `regexp` is RE2, which has no lookaround by construction — it
guarantees linear time, and lookaround is what buys backtracking engines their
exponential worst case. picomatch's output depends on lookaround in almost every
non-trivial pattern:

| Construct | Emitted by picomatch | RE2 |
| --- | --- | --- |
| leading `*` dot guard | `(?!\.)(?=.)` | rejected |
| globstar body | `(?:(?!(?:^\|\/)\.).)*?` | rejected |
| negated extglob `!(a)` | `(?:(?!(?:a)$))` | rejected |

Six of seven representative patterns fail `regexp.Compile` outright:

```
[a-z]*     ok
+(a|b)     FAIL  invalid or unsupported Perl syntax: `(?=`
*          FAIL  invalid or unsupported Perl syntax: `(?!`
a/*        FAIL  ...
!(a)       FAIL  ...
a/**/b     FAIL  ...
**/*.js    FAIL  ...
```

`[a-z]*` compiles only because its segment begins with `[` rather than `*`, so no
dot guard is emitted — an artefact of picomatch's guard being decided by the
pattern's *lexical* left neighbour, not by match position.

Returning a `*regexp.Regexp` would therefore be a type the matcher can never
populate. A port is free to reimplement; it is not free to advertise a value it
cannot produce. The matcher this port is being built towards is a memoised
backtracking AST walker, which reproduces the semantics without pretending to be
an RE2 program.

**Not a scoring dodge.** The conformance harness already scores `makeRe` (72
cases) and `parse` (32 cases) as *unsupported*, not as passes — they leave both
the numerator and the denominator. Removing the API changes no parity number.

**Reversible, at a price.** The recorded `makeRe` fixtures hold upstream's exact
ECMAScript source strings, so a `MakeRe(pattern, opts) (string, error)` emitting
that text is implementable and would make those 72 cases replayable. That is a
second full backend — an ECMAScript emitter beside the AST matcher — and it is
deliberately not in scope. It is an addition, not a change: nothing here would
have to be undone.

**Re-check.** `go doc ./... | grep -i regexp` should find nothing.

---

## 2. `Options` is reconciled against two sources, not one

**Upstream.** Options are a plain object; anything may be passed and unknown keys
are ignored.

**This port.** `Options` is a struct, so every field is a promise. The field set
is the *intersection of two independent observations*, not either one alone:

- what the upstream Mocha suite passes in — the 30 keys `tools/extract` recorded
  in `testdata/original/summary.json` under `optionSurface`;
- what upstream actually reads — every `opts.X` / `options.X` in
  `tests/original/lib` and the two entry points.

The two sets differ, in both directions, and each direction needed a rule.

**Passed but never read → no field.** `relaxSlashes` appears once, in
`test/slashes-posix.js:211`, and upstream reads it nowhere. It is inert:

```js
pm.makeRe('*').source === pm.makeRe('*', {relaxSlashes: true}).source  // true
pm.isMatch('a/', '*') === pm.isMatch('a/', '*', {relaxSlashes: true})  // true
```

A `RelaxSlashes bool` would promise behaviour picomatch does not have. The key is
still recognised by the harness — see `inertOptions` in `conformance_test.go` —
so the two fixtures using it stay in the parity denominator rather than being
dropped as unsupported over a flag that provably cannot change the answer.

**Read but never passed → field, marked.** `contains`, `fastpaths`,
`literalBrackets` and `prepend` are real options upstream reads and the suite
never exercises. They get fields, each with a doc comment saying so, because the
fixtures cannot vouch for behaviour they never trigger — the absence of a fixture
is not evidence of absence of behaviour.

**Neither → deleted.** `Options.Literal` was neither passed by the suite nor read
by upstream. `opts.literal` does not exist; `opts.literalBrackets` does, and is a
different option. The field was removed.

**Two spellings that could not survive translation:**

- `fastpaths` defaults to *on* (`opts.fastpaths !== false`). A `Fastpaths bool`
  would make the Go zero value mean "disabled" and invert the default, so the
  field is `NoFastpaths`, matching the existing `NoBrace` / `NoExtglob` idiom.
- `literalBrackets` is tested against `=== false` at `lib/parse.js:856` *and*
  `=== true` at `:865`, so unset is a third state a `bool` cannot express. It is
  `*bool`.
- `maxExtglobRecursion` accepts a number or `false` (unlimited). Kept as
  `*int` plus `UnlimitedExtglobRecursion`, so a requested cap of zero cannot
  silently become "no cap" — the cap is a denial-of-service guard
  (`test/malicious.js`).

**Re-check.** `grep -ohE "(opts|options)\.[a-zA-Z]+" tests/original/lib/*.js
tests/original/index.js tests/original/posix.js | sort -u` against
`optionSurface` in the summary.

---

## 3. Errors carry upstream's message verbatim

**Upstream.** Throws `TypeError` and `SyntaxError` with specific messages —
8 distinct ones across the 22 recorded throwing cases.

**This port.** `*picomatch.Error` carries `Name` (the JavaScript constructor) and
`Message` (byte-for-byte upstream text). `Error()` prefixes `picomatch: ` for
Go's convention that error strings are lower-case and self-identifying, while
`Message` stays capitalised because it is evidence rather than prose.

**Why not idiomatic sentinel errors alone.** The conformance harness has to
decide whether a recorded throw was reproduced. Comparing only "did an error
occur" scores `Missing closing: ")"` as equivalent to
`exceeds maximum allowed length`, and lets a port pass the throw cases by failing
for entirely unrelated reasons. The fixtures record name and message; not
comparing them would be discarding evidence already on disk.

`ErrNotImplemented` is deliberately *not* an `*Error` and never matches a
recorded throw: it is a placeholder, not a behavioural answer.

**Re-check.** Change the message in `errors.go` and re-run `make conformance`;
the two passing cases must drop to zero. (Verified: both a wrong message and a
wrong `Name` take parity from 2 to 0.)

---

## 4. `IsMatch` returns `(bool, error)`

**Upstream.** `picomatch.isMatch(str, patterns, options)` returns a bool and
throws on a bad pattern.

**This port.** Returns `(bool, error)`. Go has no exceptions, and collapsing a
compilation failure into `false` would make an invalid pattern indistinguishable
from a valid one that did not match.

An empty pattern *list* is not an error — it returns `(false, nil)`. Matching
nothing is an answer.

---

## 5. `Options.Windows` is explicit, never inferred

**Upstream.** `lib/picomatch.js` picks slash semantics from `process.platform` at
the entry point, so `a/*` has two legitimate answers depending on the host.

**This port.** `Options.Windows` is a field with no OS default. The behaviour is
part of the contract, and 1,785 of 10,465 paired fixtures (17.1%) genuinely
diverge between the two modes — mostly `[^/]` versus `[^\\/]`. Inferring it would
make the port's answer depend on the machine it runs on, and would let a
single-platform test run appear to prove both.

---

## 6. Parser state and match objects are not reproduced

`lib/picomatch.parse` returns a 17-field internal state object
(`advance`, `backtrack`, `tokens`, …) and `matcher(input, true)` returns the
JavaScript `RegExp` match array. Both are implementation details of a
regex-backed JavaScript parser.

They are excluded from parity explicitly, not silently: `parse` cases score
unsupported, and the matcher keys `match` and `regex` are listed in
`matcherFieldsNotCompared` with a stated reason. Any *other* recorded key the
harness does not compare fails the case as unsupported — see
`TestCompareFieldsRejectsUncomparedKeys`.

---

## 7. Parity is reported against two fixture sets, measured separately

**The problem.** `testdata/original` is recorded from picomatch's own unmodified
suite, and that is exactly what makes it worth quoting — nobody chose its
contents. But a fixture set only proves a port correct for behaviour it
exercises, and a percentage never says what it left out.

**What the measurement showed.** `tools/mutate/run.js` applies a mutation to
upstream, replays every fixture against the mutant, and counts how many detect
it. Five plausible Go-port choices survive all 18,792 comparable fixtures:

| Mutation | detected by |
| --- | ---: |
| walk by rune, not UTF-16 code unit | **0** |
| `nocase` via Unicode folding rather than JS `Canonicalize` | **0** |
| globstar body as "any character" | **0** |
| `maxLength` counted in code points | **0** |
| no inline fastpaths | **0** |

Two controls (dropping the `?` dot guard, dropping the `input === glob`
shortcut) are detected by 29 and 34, so the instrument is not dead.

The cause is not weak testing upstream. The suite is exhaustive *structurally* —
braces, extglobs, globstars, brackets, negation, slashes — and thin
*alphabetically*: 91 distinct code points across every pattern and input it uses,
U+0009 to U+30EB, four of them non-ASCII. Nothing in a structural corpus has a
reason to contain an astral character or a newline.

Every hole is a case where the idiomatic Go code is the wrong code. `for i, r :=
range s` decodes runes. `len(s)` is bytes, `len([]rune(s))` is code points, and
picomatch counts UTF-16 units — for `"😀"`, 4, 1 and 2. `(?i)` and
`unicode.ToLower` fold U+212A onto `k`; JavaScript's non-unicode `Canonicalize`
refuses to fold non-ASCII onto ASCII.

**The decision.** A second fixture set, `testdata/charaxis`, generated by
`tools/charaxis/generate.js`, covering the character domain and the one axis
upstream tests with a single test (the dot guard being lexical rather than
positional — `a{a,b/}*.txt` vs `ab/.txt` in `test/braces.js`, 2 records of
18,792).

It is kept in a separate directory and reported by a separate test
(`TestCharacterAxis`, its own `charaxis-report.json`, its own
`PICOMATCH_CHARAXIS_MIN` floor). The headline parity figure stays derived purely
from upstream's own tests. Folding the two together would mix a measurement
nobody chose with a target chosen deliberately, and the first number's whole
value is that nobody chose it.

**Why it is not grading its own homework.** The inputs are chosen — that is the
point, since upstream never chose them. The answers are not: every expected value
is recorded by running upstream picomatch, exactly as `tools/extract` does.
Nothing in the generator states what picomatch ought to do, and CI regenerates
the file and fails on any diff, so a hand-edited expectation cannot survive.

**Re-check.** `make mutate`. It fails if a mutation the suite used to detect now
survives, if a mutation is a no-op (its result would be meaningless), or if any
mutation escapes both fixture sets. The `charaxis` column killing all five holes
is verified on every run, not asserted here.

---

## Escape hatches

None. No `unsafe`, no cgo, no `any` in non-generic positions in the port itself.
`any` appears only in the conformance harness, where it decodes arbitrary
recorded JSON values, and in `internal/testcase`, for the same reason.
