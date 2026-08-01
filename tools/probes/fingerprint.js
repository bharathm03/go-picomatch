'use strict';
// Q4 -- "what pins the boundary?"
//
//   node tools/probes/fingerprint.js            # stats over the corpus
//   node tools/probes/fingerprint.js "a/**"     # one pattern's fingerprint
//   OUT=testdata/probes/fingerprints.jsonl node tools/probes/fingerprint.js
//
// Enumerates every string over a per-pattern alphabet up to MAXLEN and records
// accept/reject. That is a COMPLETE behavioural fingerprint within the bound --
// both sides of every boundary, by construction, rather than the one side a
// fixture happens to assert.
//
// This matters because a large fraction of corpus patterns only ever prove ONE
// side. An implementation that is systematically too permissive passes all of
// them. Enumeration attacks that directly, for about a second of compute.
//
// The alphabet MUST be derived per pattern. A global alphabet leaves most
// patterns matching nothing, because their literals fall outside it.
//
// DERIVING IT NARROWLY FAILS THE SAME WAY. An earlier version took the first
// TWO alphanumeric literals via /[A-Za-z0-9]/, which left a third of the corpus
// matching nothing in bound -- most of which the corpus itself proves has a
// match. Two mechanisms:
//
//   truncation      'a/*/*.md' -> [a,m,q,/,.]   'd' dropped, so no string
//                                               resembling the pattern is ever
//                                               enumerated
//   punctuation     'a+b/src/*.js', '*.*-*'     '+' and '-' are literals here
//                                               and /[A-Za-z0-9]/ cannot see
//                                               them at all
//
// The second is the worse one: silent and structural rather than a bound you
// can reason about. The affected patterns report a clean, confident "matches
// nothing".
//
// Cost grows as |alphabet|^MAXLEN, so widening is not free. Measured on the
// research corpus, the alphabet was the binding constraint and MAXLEN the
// expensive axis: widening bought 13.6 points of two-sided coverage for 2x the
// compute, where MAXLEN 5->6 bought 3.5 for 10x. Do not assume the same holds
// for a different probe -- fastpath-diff.js is the mirror image.

const fs = require('fs');
const corpus = require('./lib/corpus');

const pm = corpus.upstream();
const MAXLEN = Number(process.env.MAXLEN || 5);

// literals from the pattern itself + a foreign char + the two structural
// characters that carry glob semantics. `_+-` are in the literal class because
// they appear as literals in real patterns ('a+b/src/*.js', '*.*-*') while
// never being glob-structural on their own.
const alphabetFor = p => {
  const lits = [...new Set(p.match(/[A-Za-z0-9_+-]/g) || [])].slice(0, 4);
  return [...new Set([...lits, 'q', '/', '.'])].slice(0, 7);
};

const enumerate = alpha => {
  const out = [];
  (function gen(prefix, d) {
    if (d > 0) out.push(prefix);
    if (d === MAXLEN) return;
    for (const c of alpha) gen(prefix + c, d + 1);
  })('', 0);
  return out;
};

const fingerprint = p => {
  const m = pm(p);
  const alpha = alphabetFor(p);
  const strings = enumerate(alpha);
  const accepts = strings.filter(s => m(s));
  return { pattern: p, alphabet: alpha, maxlen: MAXLEN, tried: strings.length, accepts };
};

// ---- single-pattern mode -------------------------------------------------
const one = process.argv[2];
if (one) {
  const f = fingerprint(one);
  console.log('pattern   %j', f.pattern);
  console.log('alphabet  %j   maxlen %d   strings %d', f.alphabet, f.maxlen, f.tried);
  console.log('accepts   %d', f.accepts.length);
  console.log(JSON.stringify(f.accepts.slice(0, 60), null, 0));
  process.exit(0);
}

// ---- corpus-wide mode ----------------------------------------------------
const S = corpus.portableWithPattern();

// Which patterns does the corpus itself only ever prove one side of?
const byPattern = new Map();
for (const r of S) {
  const v = corpus.verdictOf(r);
  if (v === null) continue;
  const p = corpus.patternOf(r);
  if (!byPattern.has(p)) byPattern.set(p, { t: 0, f: 0 });
  byPattern.get(p)[v ? 't' : 'f']++;
}
const oneSided = new Set([...byPattern].filter(([, v]) => v.t === 0 || v.f === 0).map(([p]) => p));

const t0 = Date.now();
const out = [];
let calls = 0, twoSided = 0, empty = 0, recovered = 0, threw = 0;
for (const p of corpus.patterns(S)) {
  let f;
  try { f = fingerprint(p); } catch (e) { threw++; continue; }
  calls += f.tried;
  if (f.accepts.length === 0) empty++;
  else if (f.accepts.length < f.tried) { twoSided++; if (oneSided.has(p)) recovered++; }
  out.push({ pattern: p, alphabet: f.alphabet, maxlen: f.maxlen, tried: f.tried, accepts: f.accepts });
}
const ms = Date.now() - t0;

console.log('fingerprinted %d patterns in %dms  (%s calls, %s/sec)',
  out.length, ms, calls.toLocaleString('en-US'), Math.round(calls / (ms / 1000)).toLocaleString('en-US'));
console.log('  two-sided within bound : %d  (%s%%)', twoSided, (twoSided / out.length * 100).toFixed(1));
console.log('  match nothing in bound : %d', empty);
console.log('  threw                  : %d', threw);
console.log('one-sided in corpus      : %d', oneSided.size);
console.log('  made two-sided by enum : %d  (%s%% of the gap closed)',
  recovered, (recovered / oneSided.size * 100).toFixed(1));

if (process.env.OUT) {
  fs.mkdirSync(require('path').dirname(process.env.OUT), { recursive: true });
  fs.writeFileSync(process.env.OUT, out.map(r => JSON.stringify(r)).join('\n'));
  console.log('\nwrote %s (%d records)', process.env.OUT, out.length);
}
