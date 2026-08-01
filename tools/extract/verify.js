#!/usr/bin/env node
'use strict';

/**
 * Re-hashes `tests/original/` and compares it against MANIFEST.json.
 *
 * This is the "we did not touch the tests" receipt. CI runs it on every push, so
 * an accidental edit to the upstream suite fails the build rather than quietly
 * inflating the parity number.
 */

const fs = require('fs');
const path = require('path');

const { diffHashes, hashTree } = require('./lib/hashes');
const { MANIFEST_PATH, UPSTREAM_DIR, VENDORED_ENTRIES } = require('./lib/paths');

const main = () => {
  if (!fs.existsSync(MANIFEST_PATH)) {
    throw new Error('tests/original/MANIFEST.json not found — run `npm run vendor` first');
  }

  const manifest = JSON.parse(fs.readFileSync(MANIFEST_PATH, 'utf8'));
  const actual = hashTree(UPSTREAM_DIR, VENDORED_ENTRIES);
  const { added, removed, changed, clean } = diffHashes(manifest.files, actual);

  if (clean) {
    console.log(
      `verify: ${Object.keys(actual).length} upstream files match MANIFEST.json ` +
      `(picomatch v${manifest.upstream.version} @ ${manifest.upstream.commit.slice(0, 10)})`
    );
    return;
  }

  console.error('verify: tests/original has drifted from MANIFEST.json\n');
  for (const f of changed) console.error(`  modified  ${f}`);
  for (const f of removed) console.error(`  removed   ${f}`);
  for (const f of added) console.error(`  added     ${f}`);
  console.error(
    '\nIf an edit was deliberate, document it in DECISIONS.md and re-run ' +
    '`npm run vendor` to re-pin the manifest.'
  );

  process.exit(1);
};

try {
  main();
} catch (err) {
  console.error(`verify: ${err.message}`);
  process.exit(1);
}
