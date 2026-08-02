'use strict';
/**
 * Writes testdata/emit/ -- the recorded output of upstream's EMITTER and
 * COMPILER, one record per distinct (pattern, emit-relevant-options) pair.
 *
 *   node tools/emit/generate.js     # regenerate (needs Node)
 *
 * WHY THIS EXISTS SEPARATELY FROM make tokens
 *
 * testdata/tokens already records `state.output`, so the full-scanner emitter is
 * gated -- but only under DEFAULT options, and only for that one of upstream's
 * three parsers. Three things it records nowhere:
 *
 *   1. Non-default options. Half the corpus's (pattern, options) pairs set at
 *      least one emit-relevant key, and `windows` alone accounts for more of
 *      them than every other key put together.
 *   2. `parse.fastpaths()`. A second emitter, entered from picomatch.js:313,
 *      sharing no code with the scanner loop and disagreeing with it on a
 *      minority of patterns. It has no Go entry point at all today.
 *   3. `picomatch.compileRe`'s layer -- the `^(?:X)$` wrap, the negation wrap
 *      `^(?!...).*$`, and the flags from `opts.nocase` / `opts.flags`. Nothing
 *      in the Go root package produces regex source, so nothing measures it.
 *
 * Each record therefore carries all three layers side by side, plus `path`: which
 * of the three parsers `makeRe` ACTUALLY used. Eligibility is not use --
 * picomatch.js:316 falls through to the scanner whenever fastpaths returns
 * falsy, which is why `fastpathEligible` is its own boolean and the fall-through
 * is a recorded fact rather than an inference from an absent field.
 *
 * `output` is recorded even though it is derivable from `path` plus the other two
 * outputs. That redundancy is the point: the derivation is the claim under test,
 * and it is where the inline fast path's double wrap becomes a fixture fact
 * rather than a footnote. Under default options `foo` records path "inline",
 * output `^(?:foo)$` (utils.wrapOutput runs INSIDE parse at parse.js:653) and
 * source `^(?:^(?:foo)$)$`. A gate that unwrapped instead of stratifying on the
 * recorded `path` would score that as agreement with the scanner's bare `foo`.
 *
 * WHY PLATFORM IS NOT AN AXIS
 *
 * Measured, not assumed. `utils.isWindows` is exported at utils.js:17 and called
 * nowhere inside lib/ -- no call site in parse.js, picomatch.js, constants.js or
 * scan.js. The only platform input the emitter has is `opts.windows`, read at
 * parse.js:377 and :1351 and handed positionally to constants.globChars, which
 * tests `win32 === true`. Empirically the same: pinning navigator.platform and
 * re-requiring the tree changes makeRe(p, {}, true) on no corpus pattern, and the
 * posix and windows halves of testdata/original produce identical pair sets.
 *
 * Recording a platform axis would double the file to describe nothing. This is
 * not in tension with CLAUDE.md's `Options.Windows is never inferred from the
 * host` -- it is the same fact from the other side. The platform divergence that
 * testdata/original does record lives in the MATCHER (picomatch.js:63 picks
 * toPosixSlashes; utils.js:64 splits basenames), which this fixture never reaches.
 *
 * It is a DIFFERENT KIND OF EVIDENCE from testdata/original, in exactly the way
 * testdata/tokens is. The patterns and the option sets are upstream's own -- both
 * are taken from the recorded calls, never chosen here -- but every recorded
 * value is internal state, which DECISIONS.md §6 excludes from parity. So it is
 * reported separately and never folded into the headline figure.
 *
 * WHAT IS DELIBERATELY NOT HERE
 *
 * Pairs whose surviving options carry a `$function` value are dropped and
 * counted under `excluded` in summary.json -- see DECISIONS.md §16. The rule is
 * mechanical and only ever bites `expandRange`; the other function-valued keys
 * the corpus uses are matcher-only and project away regardless.
 *
 * WHY IT IS COMMITTED
 *
 * ~1.2MB, the same order as testdata/tokens and for the same reason: `go test`
 * must work with no Node installed. Regenerate only when the upstream pin moves.
 * `node tools/extract/verify.js` is the receipt that the tree this was recorded
 * from is unmodified, and CI regenerates this directory and diffs it, which is
 * why nothing here carries a timestamp.
 */

const fs = require('fs');
const path = require('path');
const corpus = require('../probes/lib/corpus');

const pm = corpus.upstream();
const parse = corpus.parseModule();
const OUT_DIR = path.join(corpus.ROOT, 'testdata', 'emit');

/**
 * The option keys that can change what the emitter emits, what the compiler
 * compiles, or whether either throws. Survey of upstream's own reads, split
 * three ways:
 *
 *   emitter      bash capture contains dot expandRange fastpaths keepQuotes
 *                literalBrackets maxExtglobRecursion nobrace nobracket noext
 *                noextglob noglobstar nonegate posix prepend regex
 *                strictSlashes unescape windows
 *   error surface  maxLength strictBrackets debug
 *   compile only   nocase flags
 *
 * A key outside this set and outside MATCHER_ONLY is a build failure rather than
 * a silent projection: a new upstream option must not be able to collapse two
 * distinct pairs into one without anyone noticing.
 */
const EMIT_KEYS = new Set([
  'bash', 'capture', 'contains', 'debug', 'dot', 'expandRange', 'fastpaths',
  'flags', 'keepQuotes', 'literalBrackets', 'maxExtglobRecursion', 'maxLength',
  'nobrace', 'nobracket', 'nocase', 'noext', 'noextglob', 'noglobstar',
  'nonegate', 'posix', 'prepend', 'regex', 'strictBrackets', 'strictSlashes',
  'unescape', 'windows',
]);

/**
 * Keys upstream reads only in picomatch.js's matcher body, in utils.js, or in
 * scan.js. None of them can reach state.output, the compiled source or the
 * flags, so projecting them away is what collapses 20,198 pattern-bearing
 * records to ~2,000 pairs. Declared rather than inferred, so the collapse can
 * never quietly widen.
 */
const MATCHER_ONLY = new Set([
  'basename', 'format', 'ignore', 'matchBase', 'noparen', 'onIgnore', 'onMatch',
  'onResult', 'parts', 'relaxSlashes', 'scanToEnd',
]);

const isPlainObject = v => v !== null && typeof v === 'object' && !Array.isArray(v);
const isTaggedFunction = v => isPlainObject(v) && Object.prototype.hasOwnProperty.call(v, '$function');

/**
 * The options argument a record passed, located by CALL SHAPE. This is the only
 * derivation in this file that corpus.js does not already own, and it carries
 * the same trap patternOf does: picomatch.isMatch(STR, PATTERNS, OPTIONS) takes
 * the input FIRST, so its options are args[2] while every other api's are
 * args[1]. A record built through a constructed matcher passed them as
 * construct[1], beside the pattern at construct[0].
 */
const optionsOf = r => (Array.isArray(r.construct) ? r.construct[1]
  : r.api === 'isMatch' ? r.args[2]
    : r.args[1]);

/** Sorted-key projection onto EMIT_KEYS. Throws on a key in neither list. */
const project = raw => {
  const out = {};
  if (!isPlainObject(raw)) return out;
  for (const k of Object.keys(raw).sort()) {
    if (MATCHER_ONLY.has(k)) continue;
    if (!EMIT_KEYS.has(k)) {
      throw new Error(
        `unknown option key ${JSON.stringify(k)}: add it to EMIT_KEYS if it can ` +
        'change the emitted output, the compiled source, the flags or a throw, ' +
        'and to MATCHER_ONLY otherwise');
    }
    out[k] = raw[k];
  }
  return out;
};

const canonical = opts => JSON.stringify(Object.entries(opts));
const throwOf = e => ({ name: e && e.constructor ? e.constructor.name : 'Error', message: String(e && e.message) });

// ---------------------------------------------------------------------------
// 1. Walk the corpus and collect the distinct (pattern, projected options) pairs.
// ---------------------------------------------------------------------------

const pairs = new Map();
const excludedPairs = new Set();
const excludedKeys = new Set();
// Every allow-listed key any corpus record sets, counted BEFORE the $function
// drop -- otherwise expandRange would be reported as never exercised when in
// fact it is exercised and deliberately excluded, which are different claims.
const exercisedKeys = new Set();
let excludedRecords = 0;

for (const r of corpus.records()) {
  // scan() has its own state machine and its own fixture set; utils.basename
  // takes a path rather than a pattern. Neither reaches the emitter.
  if (r.module === 'lib/scan' || r.module === 'lib/utils') continue;
  const pattern = corpus.patternOf(r);
  if (pattern === null) continue;

  const opts = project(optionsOf(r));
  const key = pattern + ' ' + canonical(opts);
  for (const k of Object.keys(opts)) exercisedKeys.add(k);

  // A recorded $function value is source text, not behaviour -- reproducing it
  // means evaluating recorded JavaScript, and two of the three recorded
  // expandRange sources call an npm package this repo does not vendor.
  // DECISIONS.md §16; counted here, never silently dropped.
  const fnKeys = Object.keys(opts).filter(k => isTaggedFunction(opts[k]));
  if (fnKeys.length > 0) {
    excludedRecords++;
    excludedPairs.add(key);
    for (const k of fnKeys) excludedKeys.add(k);
    continue;
  }

  for (const [k, v] of Object.entries(opts)) {
    const t = typeof v;
    if (t !== 'boolean' && t !== 'number' && t !== 'string') {
      throw new Error(`option ${JSON.stringify(k)} holds an unrecordable ${t} value on pattern ${JSON.stringify(pattern)}`);
    }
  }

  if (!pairs.has(key)) pairs.set(key, { pattern, opts });
}

// ---------------------------------------------------------------------------
// 2. Record every layer for every pair. Each call gets its own try/catch: a
//    throw at one layer must not erase what the others produced.
// ---------------------------------------------------------------------------

const records = [];
const pathCounts = { none: 0, inline: 0, top: 0 };
const fastpath = { eligible: 0, returnedOutput: 0, returnedFalsy: 0, threw: 0 };
const layers = { path: 0, scanner: 0, fastpath: 0, compile: 0 };
const optionSurface = new Map();
const patterns = new Set();
const optionSets = new Set();
let defaultOptions = 0, negatedCount = 0, threw = 0, scannerThrew = 0;

for (const { pattern, opts } of [...pairs.values()].sort((a, b) => {
  const ka = a.pattern + ' ' + canonical(a.opts);
  const kb = b.pattern + ' ' + canonical(b.opts);
  return ka < kb ? -1 : ka > kb ? 1 : 0;
})) {
  const rec = { pattern, options: opts };

  // picomatch.js:312, BOTH conjuncts. Over UTF-16 code units, which is what
  // String#[] indexes. Recorded rather than derived on the Go side because it is
  // the gate the fastpaths pass has to reproduce, and because it is what
  // distinguishes "not eligible" from "eligible and fell through".
  //
  // The `opts.fastpaths !== false` half is easy to drop, because no corpus
  // record sets the key (summary.json's neverExercised lists it) so nothing
  // here would notice. Dropping it is still wrong: makeRe('*.js',
  // {fastpaths:false}) never calls fastpaths at all, and recording
  // fastpathEligible:true for it names a gate that did not run. `!== false`
  // rather than a truthiness test, so `{fastpaths: undefined}` stays eligible.
  const eligible = opts.fastpaths !== false &&
    (pattern[0] === '.' || pattern[0] === '*');
  rec.fastpathEligible = eligible;

  if (eligible) {
    fastpath.eligible++;
    try {
      // Called bare at picomatch.js:313, so a throw here escapes makeRe from
      // there rather than from parse().
      const out = parse.fastpaths(pattern, opts);
      if (out) {
        rec.fastpathOutput = out;
        fastpath.returnedOutput++;
      } else {
        fastpath.returnedFalsy++;
      }
    } catch (e) {
      rec.fastpathThrow = throwOf(e);
      fastpath.threw++;
    }
  }

  // The full scanner, bare -- exactly what internal/parse.State.Output targets.
  // fastpaths:false is what picomatch.parse itself passes (picomatch.js:212).
  try {
    const st = parse(pattern, { ...opts, fastpaths: false });
    rec.scannerOutput = st.output;
    rec.negated = st.negated === true;
    if (st.negated === true) negatedCount++;
  } catch (e) {
    rec.scannerThrow = throwOf(e);
    scannerThrew++;
  }

  let output, source, flags, failure = null;
  try {
    // returnOutput=true short-circuits at picomatch.js:265, so this is
    // state.output and NOT a compiled .source -- diffing .source would
    // double-count compileRe's own ^(?:...)$ layer.
    output = pm.makeRe(pattern, opts, true);
  } catch (e) {
    failure = e;
  }
  if (failure === null) {
    try {
      const re = pm.makeRe(pattern, opts);
      source = re.source;
      flags = re.flags;
    } catch (e) {
      failure = e;
    }
  }

  if (failure === null) {
    rec.path = corpus.fastpathOf(pattern, opts);
    rec.output = output;
    rec.source = source;
    rec.flags = flags;
    pathCounts[rec.path]++;
  } else {
    rec.throw = throwOf(failure);
    threw++;
  }

  layers.path += rec.path === undefined ? 0 : 1;
  layers.scanner += (rec.scannerOutput === undefined ? 0 : 2) + (rec.scannerThrow === undefined ? 0 : 1);
  layers.fastpath += eligible ? 1 : 0;
  layers.compile += rec.source === undefined ? 0 : 2;

  patterns.add(pattern);
  optionSets.add(canonical(opts));
  const keys = Object.keys(opts);
  if (keys.length === 0) defaultOptions++;
  for (const k of keys) optionSurface.set(k, (optionSurface.get(k) || 0) + 1);

  records.push(JSON.stringify(rec));
}

// ---------------------------------------------------------------------------
// 3. Serialise. No generatedAt anywhere: CI regenerates this set and diffs it.
// ---------------------------------------------------------------------------

const byCountThenName = (a, b) => (b[1] - a[1]) || (a[0] < b[0] ? -1 : a[0] > b[0] ? 1 : 0);
const sortedSet = s => [...s].sort();

const summary = {
  _comment: 'Generated by tools/emit/generate.js from the vendored upstream at tests/original. Recorded emitter and compiler output per (pattern, options) pair; reported separately from conformance parity, never folded into it. Do not hand-edit.',
  upstream: JSON.parse(fs.readFileSync(path.join(corpus.UPSTREAM_DIR, 'MANIFEST.json'), 'utf8')).upstream,
  generatedBy: 'tools/emit/generate.js',
  cases: {
    total: records.length,
    patterns: patterns.size,
    optionSets: optionSets.size,
    defaultOptions,
    nonDefaultOptions: records.length - defaultOptions,
    negated: negatedCount,
    threw,
    scannerThrew,
    comparableFields: layers.path + layers.scanner + layers.fastpath + layers.compile,
  },
  excluded: {
    functionValuedOptions: {
      records: excludedRecords,
      pairs: excludedPairs.size,
      keys: sortedSet(excludedKeys),
    },
  },
  path: pathCounts,
  fastpath,
  layers,
  // Pairs blocked per option key: how much of the fixture each key unblocks once
  // it is threaded through. Overlapping -- a pair setting two keys is counted
  // under both -- so these do not sum to nonDefaultOptions.
  optionSurface: Object.fromEntries([...optionSurface].sort(byCountThenName)),
  optionProjection: {
    recorded: sortedSet(EMIT_KEYS),
    droppedAsMatcherOnly: sortedSet(MATCHER_ONLY),
    // Allow-listed, but no corpus record sets them at all -- distinct from
    // `excluded` above, which names keys that ARE exercised and are dropped on
    // purpose. The fixture gates the rest and says nothing about these;
    // covering them needs chosen input, i.e. a charaxis-shaped companion set.
    neverExercised: sortedSet(EMIT_KEYS).filter(k => !exercisedKeys.has(k)),
  },
};

fs.mkdirSync(OUT_DIR, { recursive: true });
fs.writeFileSync(path.join(OUT_DIR, 'cases.jsonl'), records.join('\n') + '\n');
fs.writeFileSync(path.join(OUT_DIR, 'summary.json'), JSON.stringify(summary, null, 2) + '\n');

const line = (label, n) => console.log(`  ${label.padEnd(22)} ${n}`);

console.log('wrote testdata/emit/cases.jsonl  (%d pairs, %d patterns, %d option sets, %d comparable fields)',
  records.length, patterns.size, optionSets.size, summary.cases.comparableFields);
line('default options', defaultOptions);
line('non-default options', records.length - defaultOptions);
line('path none/inline/top', `${pathCounts.none} / ${pathCounts.inline} / ${pathCounts.top}`);
line('fastpath eligible', `${fastpath.eligible} (${fastpath.returnedOutput} output, ${fastpath.returnedFalsy} falsy, ${fastpath.threw} threw)`);
line('negated', negatedCount);
line('makeRe threw', threw);
line('scanner threw', scannerThrew);
line('layers', `path ${layers.path} / scanner ${layers.scanner} / fastpath ${layers.fastpath} / compile ${layers.compile}`);
line('excluded ($function)', `${excludedRecords} records, ${excludedPairs.size} pairs, keys ${sortedSet(excludedKeys).join(',') || 'none'}`);
