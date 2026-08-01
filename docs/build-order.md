# Build order

Which scanner branch to write next, and what each one costs. Every number here
came from `make build-order`; **re-run it rather than trusting this file**, which
goes stale the moment a branch lands.

```bash
make build-order    # what each unbuilt branch would unblock
make tokens         # where the parser actually is
```

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
blocked on * (parse.js:1128)      696
blocked on !( extglob             145
blocked on [ (parse.js:814)       140
```

That is a diagnostic, not a plan. A pattern blocked on `*` may contain a bracket
too, so building `*` does not win it. The planning question is the other one —
how many patterns parse end to end once a branch exists and nothing else changes
— and `make build-order` answers it from the recorded token types. For `*` the
two numbers are 696 and **524**; the 172-pattern gap is patterns with a second
unbuilt construct behind the star.

## Measured order

| Step | Branch | Corpus | Gain |
|---|---|---|---|
| now | text, slash, dot, escape, quote, negate, `./` | 176 (11.80%) | — |
| 1 | `*` — star, globstar, maybe_slash | 700 (46.95%) | +524 |
| 2 | extglobs `!( +( *( ?( @(` | 1051 (70.49%) | +351 |
| 3 | `[` bracket | 1249 (83.77%) | +198 |
| 4 | `?` qmark | 1391 (93.29%) | +142 |
| 5 | `{` brace | 1491 (100.00%) | +100 |

`(` never earns its own step. Standalone it is worth +77, but after the extglobs
it adds **+0** — `extglobOpen` (`parse.js:523`) is what emits the paren tokens,
so step 2 subsumes it.

---

# Next: the `*` branch — `parse.js:1128-1283`

Two stages. Each is a point where `make tokens` can be re-run and `0 wrong`
re-asserted; neither is a place to stop.

| Stage | Lands | Corpus |
|---|---|---|
| 1 | plain star, `**` still declines | 489 (32.80%) |
| 2 | globstar | 700 (46.95%) |

## Why it is not three stages

`maybe_slash` (`parse.js:1304`) cannot be deferred. The push is **already
transcribed** in `internal/parse/scanner.go` — grep for `maybe_slash`, it is in
the post-loop block — and is only unreachable because nothing emits a star yet;
the moment stage 1 lands it fires on its own.

Splitting it out would not be a smaller stage, it would be a wrong one. The
scanner has no reason to *decline* a pattern like `a*` — it would parse it and
emit a stream one token short, which the gate scores as **`wrong`**, not
`unbuilt`, and `wrong` fails the run outright. That is the correct behaviour of
the gate, not a problem with it.

## Stage 1 — the plain star token

Build `parse.js:1246` and `:1263-1283`. Everything else in the branch declines.

- `:1246` — `{ type: 'star', value, output: star }`, where `star` is
  `STAR` = `[^/]*?` (`constants.js:26`) under default options.
- `:1263-1281` — the prefix rules. When the star is at `state.start`, or follows
  a slash or a dot, **both** `state.output` and `prev.output` get `nodot`
  (`(?!\.)`) appended — or `NO_DOT_SLASH` when prev is a dot — and then
  `ONE_CHAR` (`(?=.)`) unless the next character is another star.
- `:1283` — `push(token)`.

Keep declining, with an `UnsupportedError` naming the site:

- `:1140` — `*(`, an extglob (step 2 of the build order).
- `:1145` — `prev.type === 'star'`, i.e. `**`. This is stage 2.
- `:1128` — unreachable in stage 1 (nothing sets `prev.star` or emits a globstar
  yet), so it may stay a `fail()` until stage 2.

Unreachable under default options, and should be marked rather than written:
`:1248` (`opts.bash`), `:1257` (`opts.regex` with a bracket or paren prev).

**New constants required:** `STAR`, `ONE_CHAR`, `NO_DOT`, `NO_DOT_SLASH`,
`NO_DOTS_SLASH` from `constants.js:12-27`, plus the `nodot` binding at
`parse.js:399`. Add them beside the existing `dotLiteral`/`slashLiteral` block,
as the POSIX set only — the Windows set arrives with `Options.Windows`.

**Exit:** `make tokens` reports **489 / 32.80%**, `0 wrong`.

## Stage 2 — globstar

Build `parse.js:1145-1244`, seven arms, plus the two pieces it makes live.

| Arm | Site |
|---|---|
| `opts.noglobstar` | `:1146` (mark, do not write) |
| `prior`/`before`/`isStart`/`afterStar` lookbehind | `:1151-1154` |
| `opts.bash` non-start star | `:1156` (mark) |
| not-a-start star → plain star, empty output | `:1161-1166` |
| strip consecutive `/**/` | `:1168-1176` |
| `prior` is bos and eos | `:1178-1186` |
| `prior` is slash, not after bos, not after a star, eos | `:1188-1199` |
| `prior` is slash, not after bos, `rest[0] === '/'` | `:1201-1218` |
| `prior` is bos, `rest[0] === '/'` | `:1220-1229` |
| fallthrough globstar | `:1231-1243` |

It also switches on:

- **`push()`'s globstar lookbehind, `parse.js:494-505`** — currently a deliberate
  `fail()` in `scanner.go`. It rewrites a preceding globstar back to a star and
  truncates `state.output`. First real retroactive rewrite in the port.
- **`state.backtrack`, first set at `:1133`** — which makes the post-loop rebuild
  at `:1309-1319` live. Already transcribed; unreachable until now.
- **`consume(value, num)` with `num = 3` at `:1175`** — the second parameter is
  already carried for exactly this call. See transcription trap #6.
- **`state.globstar`** — a scanner field that does not exist yet.
- **Two-deep lookbehind** (`prev.prev.prev` at `:1188`). The `prev` chain exists
  on `token`; `bos.prev` is nil, and `:1188` is guarded by `prior.type === 'slash'`
  so it cannot be reached with `prior` as bos.

**Exit:** `make tokens` reports **700 / 46.95%**, `0 wrong`.

---

## Traps to register before writing either stage

`docs/transcription-traps.md` is the file; each entry needs the upstream site,
the reading that would have been wrong, and what it costs. These four are
visible from reading `parse.js:1128-1283` and should go in as they are confirmed,
not after something breaks.

1. **`slice(0, -X.length)` empties the output when `X` is empty** — `:499`,
   `:1189`, `:1204`, `:1232`, and later `:861`. JavaScript's `-0` is `0`, so
   `s.slice(0, -0)` is `s.slice(0, 0)` — the **empty string**. The Go reading,
   `out[:len(out)-n]` with `n == 0`, leaves the output **unchanged**. Opposite
   behaviour on the degenerate case, at five sites.
2. **`:1170` peeks `input[state.index + 4]`, not `+3`** — `rest` already starts
   at `index + 1`, so the character after `/**` is four ahead of the index.
3. **`:1263-1281` mutates `prev.output` and `state.output` in parallel** — this
   is the retroactive rewrite that `push()`'s in-place append optimisation must
   not alias. The seed-copy rule in `push()` exists for this; see the comment
   there before changing either.
4. **`:1263` tests `state.index === state.start`, not `=== 0`** — `start` moves
   past a negation prologue and a stripped `./`.

## What the corpus will *not* hold you to

Two blind spots inside this branch, both from `make build-order`:

- **`state.backtrack` is set by only 2 of the 524 patterns.** The rebuild at
  `:1309` goes live on two inputs. That is not coverage — it wants a targeted
  test in `internal/parse`, not reliance on the gate.
- **55 of the 524 compile differently under the fast path.** The scanner alone
  does not pin what `makeRe` returns for them. That is the separate normalisation
  pass, not a reason to change the scanner; the gate already stratifies on it
  ("fastpath-independent: 172 of 1424").

## Invariants every stage must preserve

Not optional, and CI enforces each one:

- `make tokens` reports **`0 wrong`**. A built branch that disagrees fails the
  run outright, regardless of the percentage. To turn the percentage into a hard
  gate for a stage, set the floor:
  `PICOMATCH_TOKENS_MIN=32 go test -tags conformance -run TestTokenParity ./`
- `go test ./...` green untagged; `go vet` clean under **both** tag sets.
- `gofmt` clean; `make verify-original` still reports 47 files matching.
- `testdata/charaxis/` and `testdata/tokens/` regenerate **byte-identically**.
- Constructs not yet built keep returning `UnsupportedError` with the upstream
  site. Never fall back to literal text — DECISIONS.md §9.
- No fixture is edited to make a test pass, and none is hand-authored. If the
  expected value did not come out of upstream, it does not go in `testdata/`.
- A divergence from upstream gets a **DECISIONS.md entry with a re-check**, not
  a code comment.
- New `Options` fields only where upstream actually reads the key —
  DECISIONS.md §2.
- Stdlib only. Keep new branches on `units` (`[]uint16`), never Go strings —
  DECISIONS.md §8.
