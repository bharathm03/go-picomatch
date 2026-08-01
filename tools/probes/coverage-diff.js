'use strict';
// Q2 -- "which rule decided it?"
//
//   node tools/probes/coverage-diff.js "a/**" "a/*"
//   node tools/probes/coverage-diff.js                 # built-in demo set
//   node tools/probes/coverage-diff.js --one "a/**"    # raw line list (JSON)
//
// Records which lines of tests/original/lib/parse.js execute while parsing a
// pattern, then diffs two patterns. Against a NEAR neighbour the residue is the
// rule that differs -- 'a/**' \ 'a/*' is 38 lines, starting at parse.js:396,
// the globstar emitter that forced this port to a hand-written matcher. The
// diff finds it with no human input.
//
// Use it when a conformance case fails and you need the rule, not the symptom:
// diff the failing pattern against the nearest PASSING one.
//
// EACH PATTERN IS MEASURED IN A FRESH PROCESS. V8 precise coverage accumulates
// across measurements inside one process: the same pattern reported 316 lines
// when measured first and 847 when measured later. Subprocess isolation costs
// ~130ms per pattern and makes the result order-independent, which a diagnostic
// tool has to be.
//
// LIMIT: this measures the FULL SCANNER only. It calls pm.parse, which is
// parse(pattern, { ...options, fastpaths: false }) -- lib/picomatch.js:212 --
// so for a fastpath-eligible pattern it is not measuring what makeRe ran.
// Diffing two patterns is still valid, since both sides take the same path, but
// do not read the line set as "what happened when this pattern was compiled".
// See tools/probes/fastpath-diff.js.
//
// It discriminates control flow AND, in practice, data: '[[:alpha:]]' vs
// '[a-z]' separates at parse.js:720-740. An earlier version of this tool
// reported them as identical -- that was trap 2 below, not a real limit. If a
// pair does come back NONE, fall back to diffing makeRe(p, opts, true).

const inspector = require('inspector');
const fs = require('fs');
const { execFileSync } = require('child_process');
const corpus = require('./lib/corpus');

// ---- child mode: measure exactly one pattern, print JSON -----------------
if (process.argv[2] === '--one') {
  const pattern = process.argv[3];
  const pm = corpus.upstream();
  const src = fs.readFileSync(corpus.PARSE_JS, 'utf8');
  const lineStarts = [0];
  for (let i = 0; i < src.length; i++) if (src[i] === '\n') lineStarts.push(i + 1);
  const lineOf = off => {
    let lo = 0, hi = lineStarts.length - 1;
    while (lo < hi) { const m = (lo + hi + 1) >> 1; if (lineStarts[m] <= off) lo = m; else hi = m - 1; }
    return lo + 1;
  };

  const session = new inspector.Session();
  session.connect();
  const post = (m, p) => new Promise((res, rej) => session.post(m, p, (e, r) => e ? rej(e) : res(r)));

  (async () => {
    await post('Profiler.enable');
    await post('Profiler.startPreciseCoverage', { callCount: true, detailed: true });
    try { pm.parse(pattern); } catch (e) { /* lenient: unparseable patterns still yield coverage */ }
    const { result } = await post('Profiler.takePreciseCoverage');

    // V8 emits an outer range with count>0 plus NESTED count-0 ranges for
    // blocks not taken. Painting outer-to-inner so inner wins is REQUIRED --
    // without it every pattern reads as "whole file covered" (967 lines).
    const paint = new Int32Array(src.length).fill(-1);
    for (const script of result) {
      if (!script.url.replace(/\\/g, '/').endsWith('/lib/parse.js')) continue;
      for (const fnCov of script.functions) {
        const ranges = [...fnCov.ranges].sort((a, b) => (a.startOffset - b.startOffset) || (b.endOffset - a.endOffset));
        for (const r of ranges) paint.fill(r.count, r.startOffset, Math.min(r.endOffset, src.length));
      }
    }
    const lines = new Set();
    for (let i = 0; i < paint.length; i++) if (paint[i] > 0) lines.add(lineOf(i));
    process.stdout.write(JSON.stringify([...lines].sort((a, b) => a - b)));
    session.disconnect();
  })();
  return;
}

// ---- parent mode ---------------------------------------------------------
const measure = pattern =>
  new Set(JSON.parse(execFileSync(process.execPath, [__filename, '--one', pattern], { encoding: 'utf8' })));

const DEMO = [['a/**', 'a/*'], ['a/**/c', 'a/**'], ['[[:alpha:]]', '[a-z]'], ['{a,b}', 'a/b']];
const argv = process.argv.slice(2);
const pairs = argv.length >= 2 ? [[argv[0], argv[1]]] : DEMO;

for (const [a, b] of pairs) {
  const [ca, cb] = [measure(a), measure(b)];
  const only = [...ca].filter(l => !cb.has(l)).sort((x, y) => x - y);
  console.log(`${JSON.stringify(a)} (${ca.size} lines)  \\  ${JSON.stringify(b)} (${cb.size} lines)`);
  if (!only.length) {
    console.log('  NONE -- identical control flow. The difference is in the DATA;');
    console.log('  diff makeRe(p, opts, true) instead.');
  } else if (only.length > 50) {
    console.log(`  ${only.length} lines -- too coarse, these are not near neighbours.`);
    console.log('  Pick a closer comparison pattern.');
  } else {
    console.log('  lib/parse.js:' + only.join(', '));
  }
  console.log('');
}
