# Build order

Which scanner branch to write next, and what each one costs. Every number here
came from `make build-order`; **re-run it rather than trusting this file**, which
goes stale the moment a branch lands.

```bash
make build-order    # what each unbuilt branch would unblock
make tokens         # where the parser actually is
```

**The scanner's build order is finished.** All five stages have landed and
`make tokens` reports **1491 / 1491 (100.00%), 0 wrong**, so `make build-order`
now has nothing to rank. What the table below records is what each stage was
worth and where its cost is written down; what comes next is at the bottom, and
it is not another branch.

## Where the line numbers point

Every `parse.js:N` and `constants.js:N` below is **`tests/original/lib/`** — the
vendored upstream, hash-pinned by `MANIFEST.json` at picomatch v4.0.5
(`4f41a8edad`). That directory is read-only; `tools/mutate` copies it to a temp
dir before editing. There is no other copy of picomatch in this repo, and none is
needed — do not fetch one.

## Without `make` or Node

`make` is a convenience; the gates underneath it are plain `go` commands and need
**no Node at all**, because they replay committed fixtures:

```bash
go build ./...
go vet ./... && go vet -tags conformance ./...
go test ./...
go test -tags conformance -run TestTokenParity -v ./     # the token gate
gofmt -l .                                               # must print nothing
```

Node ≥ 18 is required only for `make build-order`
(`node tools/probes/build-order.js`), `make verify-original`
(`node tools/extract/verify.js`), and fixture regeneration. If Node is
unavailable, skip those and rely on the Go gates — CI runs the rest.

## The number to plan with is not the one `make tokens` prints

`make tokens` reports which construct each failing pattern tripped on **first**:

```
blocked on { (parse.js:881)       100
```

That is a diagnostic, not a plan. A pattern blocked on `*` may contain a bracket
too, so building `*` does not win it. The planning question is the other one —
how many patterns parse end to end once a branch exists and nothing else changes
— and `make build-order` answers it from the recorded token types. For `*` the
two numbers were 696 and **524**; the 172-pattern gap was patterns with a second
unbuilt construct behind the star.

The two columns converged as the corpus ran out of unbuilt constructs. Before
the qmark stage the split was 132 on `?` and 100 on `{`, and measured over those
232 declined patterns the two sets were **disjoint**: none of the 132 also
contained a `{`, and none of the 100 contained a `?`. So the diagnostic became
the plan, and both predictions held exactly — `?` came in at +132 and `{` at
+100, taking the corpus to 100.00%. Keep the distinction anyway: it was true for
the last two stages because the corpus had run out of alternatives, which is a
fact about the corpus rather than about the two numbers.

## Measured order

| Step | Branch | Corpus | Gain |
|---|---|---|---|
| — | text, slash, dot, escape, quote, negate, `./` | 176 (11.80%) | — |
| 1 | `*` — star, globstar, maybe_slash | 700 (46.95%) | +524 |
| 2 | extglobs `!( +( *( ?( @(` | **1058 (70.96%)** | +358 |
| 3 | `[` bracket and the character class | **1259 (84.44%)** | +201 |
| 4 | `?` qmark | **1391 (93.29%)** | +132 |
| 5 | `{` brace and `{a..b}` range | **1491 (100.00%)** | +100 |

All five have landed and every figure in bold is measured rather than estimated.
Steps 2 and 3 both came in **above** the probe's estimate — 7 and 10 respectively
— which is the opposite of the correction step 1 needed (see
[DECISIONS.md](../DECISIONS.md) §12: the probe over-counted there by 48). The gap
is the same mechanism read the other way: `tools/probes/build-order.js` estimates
from the recorded token *types*, and two upstream paths rewrite a token into a
different type after pushing it — `extglobClose`'s risky path
(`parse.js:544-566`), which turns an extglob's opening token into plain `text`,
and the POSIX-class rewrite at `:730`, which folds `[[:alpha:]]` into a single
`bracket` token whose recorded value no longer contains a `[`. Both make patterns
look to the probe like something else, blocked on something else.

Step 5 needed no probe at all: `{` was the only construct the report still
listed, so its gain was the whole of the failing column, and it came in at
exactly +100. That is the one prediction in this table that could not be wrong,
and it is worth noting *why* — the two columns had converged because the corpus
had run out of alternatives, not because the estimator got better.

`(` never earns its own step. Standalone it is worth +77, but after the extglobs
it adds **+0** — `extglobOpen` (`parse.js:523`) is what emits the paren tokens,
so step 2 subsumes it. That is confirmed: after step 2 the report had no
`( (parse.js:788)` or `) (parse.js:794)` row at all, and the three constructs
left were exactly `[`, `?` and `{`.

---

# Spent: every scanner branch

All five stages have landed and `internal/parse` is at **1491 / 1491
(100.00%), 0 wrong**. The staged plans that were here are removed rather than
kept — they described what to do next, and doing it is what made them wrong. What
each stage cost is recorded where it can still be checked:
[DECISIONS.md](../DECISIONS.md) §12 for the star branch's one divergence, §14 for
the extglob branch's, §11 for the second non-terminating input the bracket branch
turned up and §15 for the brace branch's, and `docs/transcription-traps.md`
#7-#12, #18-#27, #28-#35, #36-#41 and #42-#49 for the misreadings all five
produced, each with the gate figure it breaks.

The bracket stage's own summary: 1058 → **1259 (84.44%)**, `0 wrong`, eight new
traps, one new `NonTerminatingError` site (`parse.js:732`), and a differential of
1,178,803 patterns against `parse(p, {fastpaths: false})` with zero mismatches.
`opts.literalBrackets` stayed unbuilt as planned — both arms are marked at
`:856` and `:865` and the unset path is what runs, so `Options.LiteralBrackets`
is still a `*bool` with nothing reading it.

The qmark stage's: 1259 → **1391 (93.29%)**, `0 wrong`, six new traps
(#36-#41), **no** new `NonTerminatingError` site — all 33 hangs the differential
turned up are the `parse.js:689` backslash runs §11 already records — and a
differential of 618,242 distinct patterns with zero mismatches. Three of the six
traps score `0 wrong` on the gate, which is why the differential was run at all.
`opts.dot` stayed unbuilt: `:1040`'s `opts.dot !== true` is marked and the unset
path is the one that runs, so `QMARK_NO_DOT` is unconditional for now.

The brace stage's: 1391 → **1491 (100.00%)**, `0 wrong`, eight new traps
(#42-#49), **no** new `NonTerminatingError` site, and a differential of 2,555,964
patterns with zero mismatches and — the number that matters more — **zero
declined**. Five of the eight traps score `0 wrong`, because the corpus contains
three `{a..b}` ranges in total. It also brought one new divergence,
[DECISIONS.md](../DECISIONS.md) §15: `expandRange` decides what a range compiles
to by handing a character class to the *RegExp constructor*, so the port
transcribes the ECMAScript acceptance predicate
(`internal/parse/ecmaregexp.go`) rather than asking RE2, which answers
differently in both directions. That predicate carries its own differential —
1,222,753 enumerated sources against `new RegExp`, zero disagreements — and its
own table test.

Three things reverted or were deleted with it, all of them predicted rather than
discovered: `prefixTokens` (§14), the `unsupported` helper, and the last
`UnsupportedError` a construct could produce.

---

# Next: not another scanner branch

The scanner is done under default options. Two things are not, and neither is
sequenced here because neither is measured by `make tokens`.

**The option surface.** Every `opts.X` branch the defaults do not take is marked
at its site with the key that selects it, and none is written.
`grep -n "opts\." internal/parse/*.go` is the list; it is 40-odd sites over
`dot`, `bash`, `capture`, `posix`, `strictBrackets`, `strictSlashes`, `nobrace`,
`nobracket`, `noextglob`, `noglobstar`, `nonegate`, `unescape`, `keepQuotes`,
`regex`, `literalBrackets`, `maxExtglobRecursion`, `prepend` and `expandRange`.
Three of those need an answer before the code: `literalBrackets` is tested
against both `=== false` and `=== true` so unset is a third state
([DECISIONS.md](../DECISIONS.md) §2 already has it as a `*bool`);
`maxExtglobRecursion` takes a number or `false`; and `expandRange` is a
caller-supplied **function** with no `Options` field at all, which §15 records as
a gap rather than a decision. `Options.Windows` also has to reach
`constants.globChars` — the port currently spells the POSIX set as constants and
the Windows set is two leaves away
(`SLASH_LITERAL` and `QMARK`), by design.

**The emitter and the matcher.** `make tokens` is at 100% and `make conformance`
is at 0.01%, which is exactly the gap the three-oracle table in `CLAUDE.md`
predicts: tokens matching while behaviour does not localises the bug to the
emitter or the matcher, not to the scanner. The fast paths are the other half —
382 patterns are eligible, 25 take it, and 67 compile to different source
depending on the path — and the plan of record is unchanged: full-scanner
semantics as the AST, the fast path as a separate normalisation pass gated by
`Options.NoFastpaths`.

## Invariants every stage must preserve

Not optional, and CI enforces each one:

- `make tokens` reports **`0 wrong`**. A built branch that disagrees fails the
  run outright, regardless of the percentage. To turn the percentage into a hard
  gate for a stage, set the floor:
  `PICOMATCH_TOKENS_MIN=32 go test -tags conformance -run TestTokenParity ./`
- `go test ./...` green untagged; `go vet` clean under **both** tag sets.
- `gofmt` clean; `make verify-original` still reports 47 files matching.
- `testdata/charaxis/` and `testdata/tokens/` regenerate **byte-identically**.
- Anything not yet built keeps returning `UnsupportedError` with the upstream
  site. No construct does any more, but the rule is what the option work
  inherits. Never fall back to literal text — DECISIONS.md §9.
- No fixture is edited to make a test pass, and none is hand-authored. If the
  expected value did not come out of upstream, it does not go in `testdata/`.
- A divergence from upstream gets a **DECISIONS.md entry with a re-check**, not
  a code comment.
- New `Options` fields only where upstream actually reads the key —
  DECISIONS.md §2.
- Stdlib only. Keep new branches on `units` (`[]uint16`), never Go strings —
  DECISIONS.md §8.
