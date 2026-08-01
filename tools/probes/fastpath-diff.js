'use strict';
// picomatch has THREE parsers. Do they agree?
//
//   node tools/probes/fastpath-diff.js              # corpus-wide summary
//   node tools/probes/fastpath-diff.js "a*"         # one pattern, side by side
//   OUT=testdata/probes/fastpath-divergent.jsonl node tools/probes/fastpath-diff.js
//
//   parse.fastpaths()   lib/parse.js:1330   entered by makeRe when input[0] is
//                                           '.' or '*'  (lib/picomatch.js:312)
//   inline fastpath     lib/parse.js:606    entered inside parse() when the
//                                           pattern has no /()[]{}" and does
//                                           not start with '*' or '!'
//   full scanner        lib/parse.js:440+   everything else
//
// The fast paths are NOT a pure optimisation -- they produce different regex
// source and, for some patterns, different match results. No corpus record sets
// `fastpaths`, so the corpus asserts FASTPATH semantics under default options.
// A Go port with a single parser implementing full-scanner semantics fails
// those records, and the failures look like unrelated dot/globstar bugs.
//
// The record count is a LOWER BOUND and moves with MAXLEN -- see the note on
// the constant below. Whatever bound you run, the set is what gets pinned as
// fastpath-asserting, so treat a change here as a port decision, not a tuning
// knob.
//
// Divergences cluster on trailing slashes and '..'.
//
// NOTE both sides are compared via makeRe(p, opts, /* returnOutput */ true),
// which returns state.output BEFORE compileRe wraps it. Comparing `.source`
// instead would report every inline-fastpath pattern as different, because
// compileRe adds a second ^(?:...)$ around an already-anchored source. Do not
// "simplify" this to .source.

const fs = require('fs');
const path = require('path');
const corpus = require('./lib/corpus');

const pm = corpus.upstream();

// MAXLEN 5, not 4. At 4 this probe reported 28 patterns / 142 records on the
// research corpus; the divergence is LENGTH-bound, and raising the bound by one
// found 37 / 213. Widening the alphabet alone at 4 changed nothing, so length is
// the binding constraint here -- the opposite of fingerprint.js, where the
// alphabet was. The source-differs figure is stable under both.
const MAXLEN = Number(process.env.MAXLEN || 5);

// Eligibility, transcribed from the two call sites.
const eligibleTop = p => p[0] === '.' || p[0] === '*';            // picomatch.js:312
const eligibleInline = p => !/(^[*!]|[/()[\]{}"])/.test(p);       // parse.js:606
const eligible = p => eligibleTop(p) || eligibleInline(p);

// Kept in sync with tools/probes/fingerprint.js -- see the note there on why
// `_+-` are in the literal class and why two literals was not enough.
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

// Returns null when the pattern is ineligible or unparseable, otherwise a
// verdict: same source / source-only diff / behavioural diff.
const compare = p => {
  if (!eligible(p)) return null;
  let fastSrc, slowSrc, mf, ms;
  try {
    fastSrc = pm.makeRe(p, {}, true);
    slowSrc = pm.makeRe(p, { fastpaths: false }, true);
    mf = pm(p);
    ms = pm(p, { fastpaths: false });
  } catch (e) {
    return null;
  }
  if (fastSrc === slowSrc) return { p, source: false, behaviour: false };
  const strings = enumerate(alphabetFor(p));
  const bad = strings.filter(s => mf(s) !== ms(s));
  return { p, source: true, behaviour: bad.length > 0, fastSrc, slowSrc, tried: strings.length, bad };
};

// ---- single-pattern mode -------------------------------------------------
const one = process.argv[2];
if (one) {
  const v = compare(one);
  if (!v) {
    console.log('%j takes no fast path (or does not parse) -- full scanner only.', one);
    process.exit(0);
  }
  console.log('pattern   %j', one);
  console.log('eligible  parse.fastpaths=%s  inline=%s', eligibleTop(one), eligibleInline(one));
  console.log('');
  console.log('fast source  %s', pm.makeRe(one, {}, true));
  console.log('slow source  %s', pm.makeRe(one, { fastpaths: false }, true));
  console.log('');
  if (!v.source) {
    console.log('identical source -- the fast path is a pure optimisation here.');
  } else if (!v.behaviour) {
    console.log('source differs, behaviour agrees within MAXLEN=%d (%d strings).', MAXLEN, v.tried);
  } else {
    console.log('BEHAVIOUR DIFFERS on %d of %d strings (MAXLEN=%d):', v.bad.length, v.tried, MAXLEN);
    for (const s of v.bad.slice(0, 20)) {
      console.log('  %s  fast=%s  slow=%s', JSON.stringify(s).padEnd(10), pm(one)(s), pm(one, { fastpaths: false })(s));
    }
  }
  process.exit(0);
}

// ---- corpus-wide mode ----------------------------------------------------
const S = corpus.portableWithPattern();
const pats = corpus.patterns(S);

const t0 = Date.now();
const verdicts = pats.map(compare).filter(Boolean);
const srcDiff = verdicts.filter(v => v.source);
const behDiff = verdicts.filter(v => v.behaviour);
const ms = Date.now() - t0;

const behSet = new Set(behDiff.map(v => v.p));
const srcSet = new Set(srcDiff.map(v => v.p));
const recsBeh = S.filter(r => behSet.has(corpus.patternOf(r)));
const recsSrc = S.filter(r => srcSet.has(corpus.patternOf(r)));
const setsFastpaths = S.filter(r => {
  const o = Array.isArray(r.construct) ? r.construct[1] : r.args[1];
  return o && typeof o === 'object' && 'fastpaths' in o;
}).length;

console.log('--- eligibility ---------------------------------------------------');
console.log('distinct string patterns          : %d', pats.length);
console.log('  parse.fastpaths() eligible      : %d', pats.filter(eligibleTop).length);
console.log('  inline fastpath eligible        : %d', pats.filter(eligibleInline).length);
console.log('  compared (either, and parseable): %d', verdicts.length);
console.log('');
console.log('--- disagreement (MAXLEN=%d, %dms) ---------------------------------', MAXLEN, ms);
console.log('regex SOURCE differs              : %d patterns, %d corpus records', srcDiff.length, recsSrc.length);
console.log('BEHAVIOUR differs                 : %d patterns, %d corpus records', behDiff.length, recsBeh.length);
console.log('  of those records, expected=true : %d', recsBeh.filter(r => corpus.verdictOf(r) === true).length);
console.log('corpus records setting `fastpaths`: %d   <- the corpus asserts fastpath semantics', setsFastpaths);
console.log('');
console.log('--- examples ------------------------------------------------------');
for (const v of behDiff.slice(0, 15)) {
  const mf = pm(v.p), msl = pm(v.p, { fastpaths: false });
  console.log('  %s %d/%d  %s', JSON.stringify(v.p).padEnd(24), v.bad.length, v.tried,
    v.bad.slice(0, 3).map(s => `${JSON.stringify(s)} fast=${mf(s)} slow=${msl(s)}`).join('  '));
}

if (process.env.OUT) {
  fs.mkdirSync(path.dirname(process.env.OUT), { recursive: true });
  const out = behDiff.map(v => JSON.stringify({
    pattern: v.p, maxlen: MAXLEN, tried: v.tried, divergent: v.bad,
    fastSource: v.fastSrc, slowSource: v.slowSrc
  })).join('\n');
  fs.writeFileSync(process.env.OUT, out);
  console.log('\nwrote %s (%d patterns)', process.env.OUT, behDiff.length);
}
