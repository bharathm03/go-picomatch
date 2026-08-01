'use strict';
/**
 * Which unbuilt branch to write next, measured rather than guessed.
 *
 *   make build-order
 *
 * `make tokens` reports which construct each failing pattern tripped on FIRST.
 * That is the wrong number to plan with: a pattern blocked on `*` may contain a
 * bracket as well, so building `*` does not unblock it. This probe asks the
 * other question -- for each candidate branch, how many patterns would parse end
 * to end once it exists and nothing else changes.
 *
 * The proxy is the recorded token types. A pattern is reachable when every type
 * upstream produced for it is one this scanner can emit; the built set below is
 * the list of types internal/parse pushes today. That makes the estimate an
 * upper bound on the branch and an exact answer for the corpus, since the types
 * come from the recording rather than from a guess about what the branch emits.
 *
 * Diagnostic, not a fixture. Nothing in the Go suite replays it, and the numbers
 * move as branches land -- re-run it rather than trusting docs/build-order.md.
 */

const fs = require('fs');
const path = require('path');
const corpus = require('./lib/corpus');

const CASES = path.join(corpus.ROOT, 'testdata', 'tokens', 'cases.jsonl');
const cases = fs.readFileSync(CASES, 'utf8').split('\n').filter(Boolean).map(JSON.parse);

// Token types internal/parse emits today. Keep in step with the scanner: a type
// listed here that the scanner cannot actually push inflates every number below.
const BUILT = ['bos', 'text', 'slash', 'dot', 'comma', 'plus', 'at'];

// Token types each unbuilt branch would add. `(` is listed separately from the
// extglobs on purpose -- it is worth measuring how much of it they subsume.
const BRANCHES = {
  '* (star, globstar, maybe_slash)': ['star', 'globstar', 'maybe_slash'],
  'extglobs !( +( *( ?( @(': ['negate', 'paren'],
  '[ bracket': ['bracket'],
  '? qmark': ['qmark'],
  '{ brace': ['brace'],
  '( paren': ['paren'],
};

const typesOf = c => new Set(c.tokens.map(t => t.type));
const reachable = (c, allowed) => [...typesOf(c)].every(t => allowed.has(t));
const count = allowed => cases.filter(c => reachable(c, allowed)).length;
const pct = n => ((n / cases.length) * 100).toFixed(2) + '%';

const built = new Set(BUILT);
const now = count(built);

console.log(`corpus ${cases.length} patterns, ${now} reachable with what is built (${pct(now)})\n`);

console.log('if this branch landed next, on its own');
const rows = Object.entries(BRANCHES)
  .map(([name, adds]) => [name, count(new Set([...built, ...adds]))])
  .sort((a, b) => b[1] - a[1]);
for (const [name, n] of rows) {
  console.log(`  ${name.padEnd(34)} ${String(n).padStart(5)}  ${pct(n).padStart(7)}  +${n - now}`);
}

console.log('\ngreedy order, each step taking whichever branch buys the most');
const allowed = new Set(built);
const left = { ...BRANCHES };
let cur = now;
while (Object.keys(left).length) {
  let best = null;
  let bestN = -1;
  for (const [name, adds] of Object.entries(left)) {
    const n = count(new Set([...allowed, ...adds]));
    if (n > bestN) {
      bestN = n;
      best = name;
    }
  }
  for (const t of left[best]) allowed.add(t);
  delete left[best];
  console.log(`  + ${best.padEnd(34)} ${String(bestN).padStart(5)}  ${pct(bestN).padStart(7)}  +${bestN - cur}`);
  cur = bestN;
}

// The next branch, staged. Each row is a point at which the gate can be re-run
// and `0 wrong` re-asserted, rather than one commit that lands all of `*`.
//
// maybe_slash rides along with the first stage and cannot be deferred past it.
// The push is already transcribed (internal/parse/scanner.go, parse.js:1304) and
// only unreachable because nothing emits a star yet; the moment one exists it
// fires. Splitting it out would not be a smaller stage, it would be a wrong one
// -- the scanner has no reason to decline those patterns, so it would emit a
// stream one token short and the gate would score `wrong`, not `unbuilt`.
console.log('\nstaging the top branch');
const stages = [
  ['plain star, ** declined', ['star', 'maybe_slash']],
  ['+ globstar', ['star', 'maybe_slash', 'globstar']],
];
for (const [name, adds] of stages) {
  const n = count(new Set([...built, ...adds]));
  console.log(`  ${name.padEnd(34)} ${String(n).padStart(5)}  ${pct(n).padStart(7)}  +${n - now}`);
}

// What the corpus will and will not hold you to inside that branch.
const full = new Set([...built, 'star', 'globstar', 'maybe_slash']);
const won = cases.filter(c => !reachable(c, built) && reachable(c, full));
const backtrack = won.filter(c => c.backtrack).length;
const diverges = won.filter(c => c.fastpathDiverges).length;
console.log(`\nof the ${won.length} patterns that branch would win:`);
console.log(`  ${backtrack} set state.backtrack — the rebuild at parse.js:1309 goes live on ${backtrack} patterns, so the corpus barely covers it`);
console.log(`  ${diverges} compile differently under the fast path, so the scanner alone does not pin what makeRe returns for them`);
