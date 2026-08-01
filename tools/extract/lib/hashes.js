'use strict';

const crypto = require('crypto');
const fs = require('fs');
const path = require('path');

/**
 * Lists every file under `dir`, relative to `base`, with POSIX separators and
 * sorted so the result is stable across platforms.
 */
const walk = (dir, base) => {
  const out = [];

  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const abs = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      out.push(...walk(abs, base));
    } else if (entry.isFile()) {
      out.push(path.relative(base, abs).split(path.sep).join('/'));
    }
  }

  return out.sort();
};

/**
 * Hashes file contents byte-for-byte. Line endings are deliberately *not*
 * normalised: a CRLF conversion is exactly the kind of silent edit the manifest
 * exists to catch.
 */
const hashFile = file => {
  return crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex');
};

/**
 * Builds `{ 'test/stars.js': '<sha256>', ... }` for every file under the given
 * roots, keyed relative to `base`.
 */
const hashTree = (base, entries) => {
  const files = {};

  for (const entry of entries) {
    const abs = path.join(base, entry);
    if (!fs.existsSync(abs)) continue;

    const targets = fs.statSync(abs).isDirectory()
      ? walk(abs, base)
      : [path.relative(base, abs).split(path.sep).join('/')];

    for (const rel of targets) {
      files[rel] = hashFile(path.join(base, rel));
    }
  }

  return files;
};

/**
 * Compares two hash maps. Returns added/removed/changed lists so callers can
 * report exactly which upstream files drifted.
 */
const diffHashes = (expected, actual) => {
  const added = Object.keys(actual).filter(f => !(f in expected)).sort();
  const removed = Object.keys(expected).filter(f => !(f in actual)).sort();
  const changed = Object.keys(expected)
    .filter(f => f in actual && expected[f] !== actual[f])
    .sort();

  return { added, removed, changed, clean: !added.length && !removed.length && !changed.length };
};

module.exports = { diffHashes, hashFile, hashTree, walk };
