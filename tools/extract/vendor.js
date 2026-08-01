#!/usr/bin/env node
'use strict';

/**
 * Copies the pinned upstream picomatch tree into `tests/original/` and writes a
 * SHA-256 manifest over every file.
 *
 * The vendored suite is the artefact judges hash at submission, so this script
 * only ever *writes* into `tests/original/`; it never edits upstream content.
 *
 *   node vendor.js --from ../../picomatch   # use an existing local checkout
 *   node vendor.js                          # clone the pinned commit into a temp dir
 */

const { execFileSync } = require('child_process');
const fs = require('fs');
const os = require('os');
const path = require('path');

const { hashTree } = require('./lib/hashes');
const { MANIFEST_PATH, UPSTREAM, UPSTREAM_DIR, VENDORED_ENTRIES } = require('./lib/paths');

const git = (args, cwd) => execFileSync('git', args, { cwd, encoding: 'utf8' }).trim();

const parseArgs = argv => {
  const opts = { from: null, allowDirty: false };

  for (let i = 0; i < argv.length; i++) {
    if (argv[i] === '--from') opts.from = path.resolve(argv[++i]);
    else if (argv[i] === '--allow-dirty') opts.allowDirty = true;
    else throw new Error(`unknown argument: ${argv[i]}`);
  }

  return opts;
};

/**
 * Resolves a checkout of the pinned commit. Either validates the caller's local
 * clone or makes a throwaway one, so vendoring is reproducible on a bare machine.
 */
const resolveSource = opts => {
  if (opts.from) {
    const head = git(['rev-parse', 'HEAD'], opts.from);

    if (head !== UPSTREAM.commit) {
      throw new Error(
        `${opts.from} is at ${head}, expected ${UPSTREAM.commit} (v${UPSTREAM.version}).\n` +
        `Check out the pinned commit, or run without --from to clone it.`
      );
    }

    const dirty = git(['status', '--porcelain'], opts.from);
    if (dirty && !opts.allowDirty) {
      throw new Error(
        `${opts.from} has uncommitted changes; vendoring it would bake local edits ` +
        `into the "unmodified" suite. Stash them, or pass --allow-dirty if intentional.\n${dirty}`
      );
    }

    return { dir: opts.from, cleanup: null };
  }

  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'picomatch-'));
  console.log(`cloning ${UPSTREAM.repo} @ ${UPSTREAM.version} -> ${tmp}`);
  git(['clone', '--quiet', '--depth', '1', '--branch', UPSTREAM.version, UPSTREAM.repo, tmp]);

  const head = git(['rev-parse', 'HEAD'], tmp);
  if (head !== UPSTREAM.commit) {
    throw new Error(`tag ${UPSTREAM.version} resolved to ${head}, expected ${UPSTREAM.commit}`);
  }

  return { dir: tmp, cleanup: () => fs.rmSync(tmp, { recursive: true, force: true }) };
};

const copyEntry = (srcRoot, destRoot, entry) => {
  const src = path.join(srcRoot, entry);
  if (!fs.existsSync(src)) throw new Error(`upstream is missing ${entry}`);
  fs.cpSync(src, path.join(destRoot, entry), { recursive: true });
};

const main = () => {
  const opts = parseArgs(process.argv.slice(2));
  const { dir: srcRoot, cleanup } = resolveSource(opts);

  try {
    // Replace wholesale: a stale file left behind would be hashed as if upstream
    // shipped it.
    fs.rmSync(UPSTREAM_DIR, { recursive: true, force: true });
    fs.mkdirSync(UPSTREAM_DIR, { recursive: true });

    for (const entry of VENDORED_ENTRIES) copyEntry(srcRoot, UPSTREAM_DIR, entry);

    const files = hashTree(UPSTREAM_DIR, VENDORED_ENTRIES);
    // Top-level specs only, matching the set extract.js hands to mocha.
    // `test/support/` holds helpers that are required by specs, never run as one.
    const specs = Object.keys(files).filter(
      f => f.startsWith('test/') && f.endsWith('.js') && !f.slice('test/'.length).includes('/')
    );

    const manifest = {
      _comment:
        'SHA-256 of every vendored upstream file. `npm run verify` re-hashes this tree; ' +
        'any drift means the original test suite was modified and must be justified in DECISIONS.md.',
      upstream: UPSTREAM,
      vendoredAt: new Date().toISOString(),
      algorithm: 'sha256',
      fileCount: Object.keys(files).length,
      specCount: specs.length,
      files
    };

    fs.writeFileSync(MANIFEST_PATH, `${JSON.stringify(manifest, null, 2)}\n`);

    console.log(`vendored ${manifest.fileCount} files (${manifest.specCount} specs) -> tests/original`);
    console.log(`manifest: ${path.relative(process.cwd(), MANIFEST_PATH)}`);
  } finally {
    if (cleanup) cleanup();
  }
};

try {
  main();
} catch (err) {
  console.error(`vendor: ${err.message}`);
  process.exit(1);
}
