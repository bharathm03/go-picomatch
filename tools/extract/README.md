# tools/extract

Records picomatch's observable behaviour while its **own unmodified test suite**
runs, and emits language-neutral fixtures for the Go port.

Build-time only. The Go package does not depend on any of this.

## Usage

```bash
npm install            # mocha + fill-range
node vendor.js --from ../../picomatch   # re-pin tests/original (optional)
node verify.js         # prove tests/original matches its manifest
node extract.js        # record fixtures -> ../../testdata/original/
```

From the repo root: `make deps`, `make vendor`, `make verify-original`,
`make extract`.

## How it works

`hook.js` is loaded via mocha's `--require`, before any spec. It patches
`Module._load`, so when a spec does `require('..')` or `require('../lib/scan')` it
receives a transparent wrapper around the real module instead. Every call the
*test* makes is written to JSONL with its arguments and its result.

The upstream specs are never read, rewritten, or code-generated from. They just
run.

### Only test-initiated calls are recorded

The recorder tracks call depth and records only calls entered at depth 0.
picomatch calls itself constantly — array globs recurse, `ignore` builds a nested
matcher, `parse` calls `scan` — and recording those would couple the fixtures to
picomatch's internal call graph rather than its public contract.

### Two platforms, always

`index.js` defaults `options.windows` to `utils.isWindows()`, which reads
`navigator.platform` before `process.platform`. `hook.js` pins `navigator` so the
same fixtures come out of a Windows laptop and a Linux CI runner, and
`extract.js` runs the suite once per platform. About 17% of cases genuinely
diverge between the two.

### The transparency check

Every extraction runs the suite **twice per platform**: once in `baseline` mode
(same module resolution and platform pinning, no instrumentation) and once
recording. If a single test outcome differs, extraction aborts.

This is the pipeline's core guarantee. A recorder that quietly breaks tests would
emit fixtures asserting behaviour picomatch does not have — and the port would
then be "proved" against them. It has already caught two real bugs; see
DECISIONS.md §4.

## Output

`testdata/original/cases.jsonl` — one JSON object per distinct recorded call:

```json
{
  "id": 1, "platform": "posix",
  "module": "index", "api": "matcher",
  "construct": ["*.js", {"dot": true}, false],
  "args": ["a.js"],
  "result": true,
  "error": null,
  "portable": true, "truncated": false, "occurrences": 3,
  "spec": "test/stars.js", "test": "picomatch stars should match ...",
  "testOutcome": "passed"
}
```

`api` is the function name, or `"matcher"` for a call to the function a picomatch
factory returned — in which case `construct` carries the `picomatch(glob, options,
returnState)` arguments it was built from, so every record replays standalone.

Identical calls are collapsed; `occurrences` preserves how much of the suite each
case backs.

`testdata/original/summary.json` — upstream pass counts per platform, case
totals by API and by spec, the observed **option surface** (every option key the
suite passes, with frequency and value types), the result shapes each API
returns, and any non-determinism detected.

### Value encoding

Values JSON cannot express are tagged so decoding stays unambiguous — a recorded
string is always a string, never a smuggled RegExp:

| Tag           | JavaScript value                     |
| ------------- | ------------------------------------ |
| `$undefined`  | `undefined` (distinct from `null`)   |
| `$regexp`     | `RegExp` — source and flags          |
| `$function`   | a callback; makes the case unportable |
| `$number`     | `NaN`, `Infinity`, `-Infinity`       |
| `$match`      | a match array, with `index`/`input`  |
| `$matcher`    | a factory's returned matcher         |
| `$circular`   | a cycle the encoder stopped at       |
| `$truncated`  | a value past the depth limit         |
| `$error`      | a thrown exception                   |
| `$set`/`$map` | `Set` / `Map`                        |

`internal/testcase` is the Go decoder.

## Files

| File                | Role                                                       |
| ------------------- | ---------------------------------------------------------- |
| `vendor.js`         | Copies the pinned upstream commit in, writes MANIFEST.json  |
| `verify.js`         | Re-hashes `tests/original`, fails on drift                  |
| `extract.js`        | Orchestrates baseline + recording runs, folds the output    |
| `hook.js`           | Mocha `--require` entry: platform pinning + interception    |
| `lib/instrument.js` | `Module._load` patch and the call recorder                  |
| `lib/serialize.js`  | The tagged value encoding                                   |
| `lib/hashes.js`     | SHA-256 tree hashing and diffing                            |
| `lib/paths.js`      | Repo paths and the pinned upstream revision                 |
