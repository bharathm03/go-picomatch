'use strict';
// What the Go Token/State structs have to hold, measured rather than guessed --
// plus a census of the places the scanner REWRITES tokens it already emitted.
//
//   node tools/probes/token-inventory.js            # inventory + hotspots
//   node tools/probes/token-inventory.js "a/**/c"   # token stream for one pattern
//   OUT=testdata/probes/tokens.jsonl node tools/probes/token-inventory.js
//
// The rewrite census is the load-bearing part. picomatch's parse() maintains
// TWO representations at once -- an incrementally-appended regex string
// (state.output) and the token array -- and when a decision invalidates output
// already appended it sets state.backtrack and REBUILDS the string from the
// tokens at parse.js:1309. That tells you which representation picomatch trusts
// when they disagree: the tokens. The string is a cache.
//
// That is why a conventional lexer -> parser split cannot reproduce picomatch,
// and why the Go port builds a node slice and never maintains a parallel
// serialised form.
//
// CAVEAT on the golden token stream: `picomatch.parse` is
// `parse(pattern, { ...options, fastpaths: false })` (lib/picomatch.js:212), so
// it always takes the full scanner. `makeRe` does not -- see
// tools/probes/fastpath-diff.js. For a fastpath-eligible pattern the token
// stream describes a code path that did not run. Any tokens.jsonl this writes
// must carry a fastpath flag per pattern or it asserts the wrong oracle for a
// few hundred of them.

const fs = require('fs');
const path = require('path');
const corpus = require('./lib/corpus');

const parse = corpus.parseModule();

const strip = t => {
  const o = {};
  for (const k of Object.keys(t)) if (k !== 'prev') o[k] = t[k];
  return o;
};

// ---- single-pattern mode -------------------------------------------------
const one = process.argv[2];
if (one) {
  const st = parse(one, { fastpaths: false });
  console.log('pattern  %j', one);
  console.log('consumed %j        <- note any slash the scanner invented', st.consumed);
  console.log('output   %j', st.output);
  console.log('');
  for (const [i, t] of st.tokens.entries()) {
    console.log('  %s %s %s', String(i).padStart(3), t.type.padEnd(12), JSON.stringify(strip(t)));
  }
  process.exit(0);
}

// ---- corpus-wide mode ----------------------------------------------------
const pats = corpus.patterns();

const types = new Map();
const fields = new Map();
const out = [];
let nTok = 0, nPat = 0, threw = 0;
let backtrack = 0, risky = 0, hasDoubleStar = 0, keptGlobstar = 0, demoted = 0;

for (const p of pats) {
  let st;
  try { st = parse(p, { fastpaths: false }); } catch (e) { threw++; continue; }
  nPat++;

  for (const t of st.tokens) {
    nTok++;
    types.set(t.type, (types.get(t.type) || 0) + 1);
    for (const k of Object.keys(t)) {
      if (k === 'prev') continue;
      if (!fields.has(k)) fields.set(k, new Set());
      fields.get(k).add(typeof t[k]);
    }
  }

  // Retroactive-rewrite census.
  //
  // state.backtrack means "I mutated a token I had already emitted, so the
  // incrementally-built state.output string is now stale". When it is set, the
  // whole output is DISCARDED and rebuilt from the token array
  // (parse.js:1309-1319). Four sites set it:
  //
  //   parse.js:731   POSIX class expanded inside a bracket
  //   parse.js:922   brace range expanded via expandRange()
  //   parse.js:1133  globstar (or star-run) rewritten in place to a star
  //   parse.js:561   risky extglob: opener becomes `text`, later tokens blanked
  //
  // Only the last is isolable from outside, via maxExtglobRecursion:false.
  //
  // Two MORE rewrites do not set the flag because they truncate state.output by
  // hand: push() at parse.js:493-505 demotes an already-emitted globstar to a
  // star and slices the output back, and parse.js:512-516 merges consecutive
  // text tokens so tokens are not 1:1 with source characters. Those are read
  // from the source, not counted here.
  if (st.backtrack === true) backtrack++;
  try {
    if (st.output !== parse(p, { fastpaths: false, maxExtglobRecursion: false }).output) risky++;
  } catch (e) { /* ignore */ }

  // Separately: how often does '**' fail to produce a globstar token at all?
  // Causes are mixed -- the segment conditions may never have been met, or
  // push() (parse.js:493-505) may have demoted it because the next token was
  // not on the exception list. The count below does not distinguish them.
  if (/\*\*/.test(p)) {
    hasDoubleStar++;
    if (st.tokens.some(t => t.type === 'globstar')) keptGlobstar++; else demoted++;
  }

  if (process.env.OUT) {
    out.push(JSON.stringify({
      pattern: p, consumed: st.consumed, output: st.output,
      negated: st.negated, backtrack: st.backtrack,
      tokens: st.tokens.map(strip)
    }));
  }
}

console.log('--- token inventory (parse, fastpaths:false) ----------------------');
console.log('%d patterns parsed, %d threw, %d tokens', nPat, threw, nTok);
console.log('');
console.log('token types (%d):', types.size);
for (const [t, n] of [...types].sort((a, b) => b[1] - a[1])) {
  console.log('  %s %s', t.padEnd(12), String(n).padStart(6));
}
console.log('');
console.log('token fields (%d, excluding `prev`):', fields.size);
for (const [k, v] of fields) console.log('  %s %s', k.padEnd(12), [...v].join('|'));

const st = parse('a/**/*.js', { fastpaths: false });
console.log('');
console.log('--- parse() state fields ------------------------------------------');
for (const k of Object.keys(st)) {
  const v = st[k];
  if (typeof v === 'function') continue;
  const shown = Array.isArray(v) ? `array(${v.length})` : JSON.stringify(v);
  console.log('  %s %s %s', k.padEnd(12), (typeof v).padEnd(8), String(shown).slice(0, 56));
}

console.log('');
console.log('--- retroactive-rewrite census ------------------------------------');
console.log('state.backtrack set -- output discarded and rebuilt from tokens');
console.log('  (parse.js:1309, set at :561 :731 :922 :1133) : %d patterns', backtrack);
console.log('  of those, the risky-extglob rewrite          : %d patterns', risky);
console.log('    isolated by maxExtglobRecursion:false changing the output');
console.log('');
console.log('patterns containing "**"                       : %d', hasDoubleStar);
console.log('  globstar token emitted                       : %d', keptGlobstar);
console.log('  no globstar token                            : %d', demoted);
console.log('    causes are mixed: segment conditions never met, or push()');
console.log('    demoted it at parse.js:499. Not distinguished here.');

if (process.env.OUT) {
  fs.mkdirSync(path.dirname(process.env.OUT), { recursive: true });
  fs.writeFileSync(process.env.OUT, out.join('\n'));
  console.log('\nwrote %s (%d records)', process.env.OUT, out.length);
}
