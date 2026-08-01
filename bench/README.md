# bench

Benchmarks: Go port versus the Node original, on a shared workload.

**Not built yet.** Scaffolded for layout; there is nothing to measure while the
matcher is unimplemented.

## Planned shape

`methodology.md` and `results.json`, reporting **p99, RSS and startup time**
alongside throughput. Throughput alone is the number that flatters a port; the
ones that matter for a library used in a file-watcher's hot path are tail latency
and how long the process takes to be ready at all.

Two workloads:

- **Compile-heavy** — many distinct patterns compiled once each. This is where a
  cold Go binary should win outright against Node's startup.
- **Match-heavy** — one compiled pattern against many inputs. This is the honest
  comparison, and the one where V8's JIT is a genuine competitor. A regression
  here would not be surprising.

Patterns are drawn from `testdata/original/cases.jsonl` rather than invented, so
the workload reflects what the upstream suite actually exercises.

## Reporting rule

Numbers get published whichever way they come out, with methodology and
confounders named. A disclosed 2× regression scores better than a throughput-only
chart, and is more useful to anyone deciding whether to adopt this.
