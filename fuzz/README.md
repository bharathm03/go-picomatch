# fuzz

Differential fuzzing: same inputs into upstream picomatch and into the Go port,
outputs compared.

**Not built yet.** This directory is scaffolded so the layout is fixed; the
harness lands once the matcher does, since there is nothing to differ against
while every entry point returns `ErrNotImplemented`.

## Planned shape

A Go `testing.F` fuzz target generates `(pattern, input, options)` triples and
compares the port's answer against upstream's. Upstream runs as a long-lived Node
subprocess reading newline-delimited JSON, so the cost is one process for the
session rather than one per input.

The Node side reuses `tools/extract`'s vendored, hash-pinned picomatch — so the
fuzzer and the conformance fixtures are provably testing against the same
revision, `4f41a8ed`.

Seed corpus comes from `testdata/original/cases.jsonl`: 20,930 patterns the
upstream suite already considered interesting is a far better starting point than
random strings, and it means fuzzing explores *outward* from known-good behaviour
rather than rediscovering it.

## Why this matters beyond the bonus

The recorded fixtures can only cover behaviour some upstream test triggers. That
is a real ceiling on what conformance can prove. Fuzzing is how the port reaches
inputs the suite never had — and it is the mechanism most likely to surface a
latent bug in the original, which is the more interesting result.

Any divergence found gets triaged into one of: a port bug, a deliberate
divergence documented in `DECISIONS.md`, or an upstream bug worth filing.
