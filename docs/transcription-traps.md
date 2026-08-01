# Transcription traps

Places where the obvious Go reading of upstream's source is wrong — where a
careful, idiomatic transcription produces code that compiles, reads correctly,
and disagrees with picomatch.

This is not a list of bugs in picomatch, and not a list of divergences in the
port. Every entry is somewhere the port *matches* upstream because the trap was
noticed. It is here so the next branch does not have to notice it again.

**Add to this list when a branch turns up another one.** An entry needs the
upstream site, the reading that would have been wrong, and what it costs —
without the third part a reader cannot tell whether it is worth the attention.

## How these were found, and what that implies

By reading `lib/parse.js`, not by the token gate. The gate reported `0 wrong` on
its first run precisely because these had already been handled.

That is the ordering worth keeping. `make tokens` splits its failures into
**unbuilt** (the scanner refused a construct it does not implement) and **wrong**
(a branch that exists disagreed with the recording), and `wrong` fails the run
unconditionally — see [DECISIONS.md](../DECISIONS.md) §9. But it is a backstop,
not the method: it can only catch a trap on a pattern the corpus happens to
contain, and it tells you a token differed, not which reading was too clever.
Reading first is cheaper than bisecting afterwards.

---

## 1. `!` falls through — `parse.js:1053-1065`

```js
if (value === '!') {
  if (opts.noextglob !== true && peek() === '(') { ... continue; }
  if (opts.nonegate !== true && state.index === 0) { negate(); continue; }
}
// no continue here
```

Every neighbouring branch ends by consuming its character. This one does not: a
`!` that is neither an extglob opener nor at index 0 drops out of the branch
entirely and is picked up by the plain-text case at `:1109`.

**The wrong reading.** Closing the branch the way its neighbours close — adding a
trailing `continue`, or restructuring into a `switch` where each case is
self-contained — silently discards every mid-pattern `!`.

**What it costs.** `a!b` loses a character. A `switch` on the current character
is the natural Go shape for this loop and it makes the trap structural rather
than accidental, which is why the port keeps the `if`/`continue` chain in
upstream's order instead.

## 2. The text merge is JavaScript-truthy — `parse.js:513`

```js
prev.output = (prev.output || prev.value) + tok.value;
```

`prev.output || prev.value` falls back to the value when `output` is the **empty
string**, not only when it is absent. JavaScript's `||` tests truthiness; `""` is
falsy.

**The wrong reading.** `if prev.output != nil` — the direct translation of "has
an output" — is a different test, and it differs on exactly the tokens that carry
an empty output.

**What it costs.** 1,883 of the 10,558 recorded tokens have `output` present and
empty (against 2,366 with it absent), so this is not an edge case. It is also the
reason `Token.Output` is a `*string`: a plain `string` collapses absent and empty
into one state and cannot express either side of this line.

## 3. The bare `+` arm is shaped unlike its siblings — `parse.js:1087`

```js
if ((prev && prev.value === '(') || opts.regex === false) {
  push({ type: 'plus', value, output: PLUS_LITERAL });   // :1078
}
if (... prev.type === 'bracket' | 'paren' | 'brace' ...) {
  push({ type: 'plus', value });                          // :1083
}
push({ type: 'plus', value: PLUS_LITERAL });              // :1087
```

Three arms, three shapes. The last one puts the escape in `value` and sets **no
`output` at all**, where the first sets both and the second sets neither.

**The wrong reading.** Normalising the three into one constructor — the obvious
tidy-up — gets the token type and the emitted text right and the field
*presence* wrong.

**What it costs.** Nothing visible in the output string, which is what makes it
dangerous: the regex source matches and only the token record differs. The
pointer fields on `Token` exist so the gate can see it.

## 4. `}` emits a literal where `]` escapes — `parse.js:840` vs `:901`

```js
push({ type: 'text', value, output: `\\${value}` });   // ']' with no '['  :840
push({ type: 'text', value, output: value });          // '}' with no '{'  :901
```

An unmatched `]` is escaped. An unmatched `}` is not.

**The wrong reading.** The two branches are otherwise symmetrical, sit ninety
lines apart, and both handle "closing delimiter with nothing open". Factoring
them together, or assuming the second is a typo for the first, changes the
emitted regex.

**What it costs.** `\}` and `}` mean the same thing to most regex engines, so
this survives casual testing and shows up as a token-level difference rather than
a behavioural one.

## 5. `state.consumed` is not a slice of the input — `parse.js:430`, `:982`

Three separate mechanisms detach `consumed` from the input the caller passed:

- `utils.removePrefix` strips a leading `./` **before the loop starts**
  (`:430`), so `./foo` consumes `foo`;
- the leading-`!` run is consumed by `negate()` and never appended, so
  `!!!!!!!!abc` consumes `abc`;
- escaped values are accumulated in their **escaped** form, so `!$` consumes
  `\$` — two characters where the input had one.

Separately, `:982` resets `consumed` to empty mid-parse when a `./` survived
behind a negation.

**The wrong reading.** Treating `consumed` as an offset or a subslice of the
input — the representation a Go port reaches for, since it avoids a copy.

**What it costs.** 23 of the 134 simplest patterns in the corpus already differ
from their input, before any of the interesting syntax is built. It also runs the
other way: `Consumed` can be *longer* than the input it came from, which an
offset cannot express at all. The doc on `State.Consumed` records the case where
parsing `a/**/*.js` consumes `a/**//*.js` — the scanner invents a slash.

## 6. `consume` has a second parameter, and eight of nine callers omit it — `parse.js:447`

```js
const consume = (value = '', num = 0) => {
  state.consumed += value;
  state.index += num;
};
```

Two jobs, one helper. Eight call sites pass only the value; the ninth is
`consume('/**', 3)` in the globstar branch (`:1175`), which is also advancing the
index past the three characters it just accounted for.

**The wrong reading.** Writing the helper from the calls you have in front of you.
Every branch built so far routes through `append()`, which calls
`consume(token.value)` — so a Go transcription that stops at
`func (s *scanner) consume(value units)` is complete for everything currently
written, compiles, passes, and is missing half its body.

**What it costs.** Nothing yet, which is the problem: the parameter is missing
exactly until the branch that needs it. `*` is top of the measured build order at
696 blocked patterns, and the globstar path is inside it — so the next person to
open this file reaches for a helper whose signature says it does the whole job,
and the scanner silently re-reads `/**`. The port carries the parameter with all
current callers passing `0` rather than a comment saying to remember.

---

## Related

- [DECISIONS.md](../DECISIONS.md) §8 — why the scanner indexes UTF-16 code units.
  Arguably the largest trap of all, but it is a deliberate divergence in
  representation rather than a misreading, so it is recorded as a decision.
- [DECISIONS.md](../DECISIONS.md) §9 — the unbuilt/wrong split that backstops
  this list, and why a declined parse still returns its tokens so the branches
  that did run are scored.
- [DECISIONS.md](../DECISIONS.md) §11 — upstream's `parse()` does not terminate
  on every input. The faithful transcription of `eos()` inherits the hang, which
  is the one case where matching upstream exactly is the wrong answer.
- [tools/mutate/README.md](../tools/mutate/README.md) — the complementary
  question: not "what is easy to transcribe wrongly" but "what would no fixture
  notice if you did".
