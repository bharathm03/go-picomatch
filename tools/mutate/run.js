#!/usr/bin/env node
'use strict';

/**
 * Measures what the fixture set can detect.
 *
 *   node tools/mutate/run.js            # report and gate against the baseline
 *   node tools/mutate/run.js --json     # machine-readable
 *
 * For each mutation: copy tests/original to a scratch tree, apply the edit,
 * prove the edit actually changed behaviour, then replay every fixture and
 * count how many detect it.
 *
 * Exit status is non-zero when a mutation marked `killed` survives — the
 * fixtures lost coverage they used to have — or when a mutation turns out to be
 * a no-op, which would make its result meaningless.
 *
 * tests/original is hash-pinned and never written to; every edit lands in a
 * temporary copy that is removed afterwards.
 */

const fs = require('fs');
const os = require('os');
const path = require('path');
const { execFileSync } = require('child_process');

const { MUTATIONS } = require('./mutations');
const { replay, DEFAULT_CASES } = require('./replay');

const REPO = path.resolve(__dirname, '..', '..');
const UPSTREAM = path.join(REPO, 'tests', 'original');
const CHARAXIS = path.join(REPO, 'testdata', 'charaxis', 'cases.jsonl');
const JSON_OUT = process.argv.includes('--json');

/**
 * The fixture sets, measured separately.
 *
 * `upstream` is recorded from picomatch's own unmodified suite and is the
 * honest parity measure. `charaxis` is the supplementary set built to cover what
 * the mutation results showed upstream cannot see; measuring it here is how we
 * check it does the job it exists for, rather than assuming it.
 */
const SETS = [{ name: 'upstream', file: null }];
if (fs.existsSync(CHARAXIS)) SETS.push({ name: 'charaxis', file: CHARAXIS });

const log = (...a) => { if (!JSON_OUT) console.log(...a); };

/** Copies upstream (minus its specs, which are not needed to replay). */
const copyTree = dest => {
  fs.rmSync(dest, { recursive: true, force: true });
  fs.cpSync(UPSTREAM, dest, { recursive: true });
  fs.rmSync(path.join(dest, 'test'), { recursive: true, force: true });
};

const applyEdits = (dir, edits, name) => {
  const sites = [];
  for (const [file, find, replace] of edits) {
    const p = path.join(dir, file);
    const before = fs.readFileSync(p, 'utf8');
    const n = before.split(find).length - 1;
    if (n === 0) throw new Error(`${name}: anchor not found in ${file}\n  ${find}`);
    fs.writeFileSync(p, before.split(find).join(replace));
    sites.push(`${file}×${n}`);
  }
  return sites;
};

/**
 * Evaluates witnesses in a child process, so module state and the pinned
 * navigator.platform cannot leak between trees.
 */
const evaluateWitnesses = (root, witnesses) => {
  const lib = path.join(root, 'lib', 'picomatch.js').replace(/\\/g, '/');
  const script = `
    Object.defineProperty(globalThis, 'navigator',
      { value: { platform: 'linux' }, configurable: true });
    const pm = require(${JSON.stringify(lib)});
    const out = [];
    for (const [pattern, input, options] of ${JSON.stringify(witnesses)}) {
      let match = null, threw = null;
      try { match = pm.isMatch(input, pattern, options || {}); }
      catch (e) { threw = e.message; }
      out.push({ match, threw });
    }
    console.log(JSON.stringify(out));
  `;
  return JSON.parse(execFileSync(process.execPath, ['-e', script], { encoding: 'utf8' }));
};

const main = () => {
  if (!fs.existsSync(DEFAULT_CASES)) {
    console.error(`fixtures not found: ${DEFAULT_CASES}\nRun \`make extract\` first.`);
    process.exit(2);
  }

  const scratch = fs.mkdtempSync(path.join(os.tmpdir(), 'picomatch-mutate-'));
  const base = path.join(scratch, 'base');
  const mutant = path.join(scratch, 'mutant');

  const results = [];
  let regressions = 0;
  let noops = 0;

  try {
    copyTree(base);

    for (const set of SETS) {
      const clean = replay(base, set.file || DEFAULT_CASES);
      log(`baseline ${set.name.padEnd(9)} ${clean.passed}/${clean.total} pass against unmutated upstream`);
      if (clean.passed !== clean.total) {
        console.error(`the replayer disagrees with unmodified picomatch on the ${set.name} set — ` +
          'it cannot measure anything');
        clean.failures.forEach(f => console.error('  ' + f));
        process.exit(2);
      }
      set.size = clean.total;
    }
    log('');

    const baseWitness = new Map();

    for (const m of MUTATIONS) {
      copyTree(mutant);
      const sites = applyEdits(mutant, m.edits, m.name);

      // Prove the mutation is real before believing any survival.
      if (!baseWitness.has(m.name)) baseWitness.set(m.name, evaluateWitnesses(base, m.witnesses));
      const before = baseWitness.get(m.name);
      const after = evaluateWitnesses(mutant, m.witnesses);
      const diverging = before.filter((b, i) =>
        b.match !== after[i].match || b.threw !== after[i].threw).length;

      const kills = {};
      for (const set of SETS) {
        const run = replay(mutant, set.file || DEFAULT_CASES);
        kills[set.name] = run.failed + run.unsupported;
      }
      const killed = kills.upstream;
      const outcome = killed > 0 ? 'killed' : 'survives';

      const noop = diverging === 0;
      if (noop) noops++;
      // `expected` is a claim about the UPSTREAM set only: it is the one nobody
      // chose the contents of, so it is the one whose coverage is worth pinning.
      const regressed = !noop && m.expected === 'killed' && outcome === 'survives';
      if (regressed) regressions++;

      results.push({
        name: m.name, expected: m.expected, outcome, kills,
        witnessesDiverging: diverging, witnesses: m.witnesses.length, noop, regressed,
        improved: m.expected === 'survives' && outcome === 'killed'
      });

      log(`${m.name}`);
      log(`  ${m.why}`);
      log(`  applied to ${sites.join(', ')}; witnesses diverging: ${diverging}/${m.witnesses.length}`);
      if (noop) {
        log('  !! NO-OP — the edit changed no behaviour, so its result means nothing');
      } else {
        for (const set of SETS) {
          const n = kills[set.name];
          log(`  ${set.name.padEnd(9)} ${n === 0
            ? `SURVIVES all ${set.size} — invisible`
            : `killed by ${n} / ${set.size} (${(100 * n / set.size).toFixed(2)}%)`}`);
        }
      }
      if (regressed) log('  !! REGRESSION — this used to be detected');
      if (m.expected === 'survives' && outcome === 'killed') {
        log('  ++ upstream coverage improved; update `expected` in mutations.js');
      }
      log('');
    }
  } finally {
    fs.rmSync(scratch, { recursive: true, force: true });
  }

  // A mutation no fixture set detects is a live hole: a port could ship it and
  // every number in the repo would still read clean.
  const uncovered = results.filter(r =>
    !r.noop && SETS.every(s => (r.kills[s.name] || 0) === 0));

  if (JSON_OUT) {
    console.log(JSON.stringify({
      sets: SETS.map(s => ({ name: s.name, size: s.size })),
      mutations: results,
      regressions, noops, uncovered: uncovered.map(r => r.name)
    }, null, 2));
  } else {
    console.log('=== summary ===');
    console.log(`${''.padEnd(36)}${SETS.map(s => s.name.padStart(12)).join('')}`);
    for (const r of results) {
      const cells = SETS.map(s => String(r.noop ? '-' : r.kills[s.name]).padStart(12)).join('');
      const mark = r.noop ? '  NO-OP'
        : SETS.every(s => r.kills[s.name] === 0) ? '  UNCOVERED'
        : r.kills.upstream === 0 ? '  (upstream blind)' : '';
      console.log(`${r.name.padEnd(36)}${cells}${mark}`);
    }
    console.log(`\nfixtures: ${SETS.map(s => `${s.name}=${s.size}`).join(', ')}`);

    const blind = results.filter(r => !r.noop && r.kills.upstream === 0);
    console.log(`${blind.length} of ${results.length} mutations are invisible to the upstream suite.`);
    if (SETS.length > 1) {
      console.log(`${blind.length - uncovered.length} of those ${blind.length} are caught by charaxis.`);
    }
    if (uncovered.length) {
      console.log(`\n${uncovered.length} UNCOVERED: ${uncovered.map(r => r.name).join(', ')}`);
    }
    if (regressions) console.log(`${regressions} REGRESSION(S): a detected mutation now survives.`);
    if (noops) console.log(`${noops} no-op mutation(s): fix the edit or drop it.`);
  }

  process.exit(regressions + noops + uncovered.length > 0 ? 1 : 0);
};

main();
