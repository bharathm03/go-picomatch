# Port Mortem — A Hackathon Raptors Event

[Code Resurrection · A Hackathon Raptors series](https://coderesurrection.com)

Wave 2 · 2026 · presents

# Port Mortem

Pick a real repo. Pick a new language. Prove the rewrite holds up.

Dates

Jul 31 – Aug 03

Format

72h · Online

Entry

Free

Prize pool

$1,800

[Register — it's free](https://tally.so/r/A7aNP0) [Join the Discord](https://discord.gg/xfYPDZYqeh) [Read the brief](#problem)

✦ What's this, in plain English?

TL;DR

You and your team pick an open-source GitHub project and rewrite it in a different programming language — within 72 hours. The north-star goal we all aim at is for the rewrite to pass the original project's own test suite, so it actually behaves like the original and not just compiles. Falling short is fine and expected; how close you get is what the judges weigh.

13,044

unsafe blocks at the Bun Zig→Rust merge

99.8%

Linux x64 test-pass rate Bun claimed at merge

1B

lines of C/C++ Microsoft targets to port to Rust by 2030

72h

to build a port that actually behaves like the original

01 / The problem

## The problem

In May 2026, Bun merged a 960,000-line, 2,188-file rewrite from Zig to Rust in **six days**, generated mostly by Claude Code agents.[1](#ref-1)[2](#ref-2) At merge it had a **99.8% Linux x64 test-pass rate** — and **13,044 _unsafe_ blocks**.[3](#ref-3) For comparison, Astral's _uv_ (a comparable systems-level Rust project) has 73.

Then people noticed something else: parts of the original Bun test suite had been edited to make the Rust port green.[4](#ref-4)

The "rewrite it in Rust" meme has officially graduated to corporate strategy. Microsoft published a goal of porting **one billion lines** of C/C++ to Rust by 2030, AI-assisted.[5](#ref-5) DARPA's TRACTOR program is funding the same direction.[6](#ref-6) Microsoft's TypeScript compiler is being rewritten from TypeScript into Go for a ~10× speedup, due GA in 2026.[7](#ref-7) Discord's Go-to-Rust migration of their read-states service is now five years old and still cited in every distributed-systems hiring loop.[8](#ref-8)

Generating a cross-language port is now trivial. Anyone with a modern AI coding agent and a weekend can produce something that compiles. **Producing a port that actually behaves like the original** — same edge cases, same concurrency semantics, same failure modes, with the original test suite untouched — is the open problem.

Port Mortem is a 72-hour hackathon. Pick a real public GitHub repo in language X. Rewrite it in language Y. Prove the original test suite still passes, unmodified. Survive a differential fuzz session. Show your _unsafe_ count. Defend your architectural decisions to a human.

We're not against AI-assisted porting. We're against the version of it that ships 13,000 _unsafe_ blocks and edits the tests on the way out.

You have 72 hours.

02 / Why now

## Why now

The “rewrite it in another language” phase of the industry stopped being a meme in 2025 and became corporate strategy in 2026.

Microsoft is rewriting the TypeScript compiler in Go (~10× speedup), and has stated a goal of porting one billion lines of C/C++ to Rust by 2030, AI-assisted.[5](#ref-5)[7](#ref-7) DARPA's TRACTOR program is funding C-to-Rust translation at scale.[6](#ref-6) Anthropic acquired Bun and shipped a 960,000-line Zig-to-Rust rewrite in six days using Claude Code.[1](#ref-1)[2](#ref-2) Astral has rebuilt the entire Python tooling stack (ruff, uv, ty) in Rust and is replacing each tool people use daily.[10](#ref-10) Discord's five-year-old Go-to-Rust read-states migration is still the canonical reference for “GC vs predictable latency” in distributed systems interviews.[8](#ref-8) Cloudflare's Pingora replaced NGINX with Rust at 40M+ req/sec.[11](#ref-11)

The technical capability is here. The cultural permission is here. What's missing is **the rigor**.

Bun shipped 13,044 _unsafe_ blocks at merge and edited tests on the way out. Microsoft's billion-line goal assumes the AI-generated ports are correct without specifying how anyone will verify that at scale. Most “we rewrote X in Y” weekend posts ship without test parity, without differential fuzzing, without honest benchmarks.

This is the gap. Generating ports is solved. **Proving they work** is the open problem.

That's the hackathon. 72 hours. Real repo. Real test suite. Real differential fuzzing. Real signed Evaluation Protocols at the end. The north star: don't edit the tests, don't unsafe-everywhere, don't ship a port that compiles but doesn't actually behave like the original. How close you get to that bar is the score — not a pass/fail gate.

Languages die. Code doesn't.

03 / Eight tracks

## Eight tracks

Eight source-to-target pairs. Pick one.

Source ↓ · Target →

→ Rust

→ Go

→ Zig

→ Other

C

[A](#track-a)

·

[G](#track-g)

·

Zig

[B](#track-b)

·

·

·

Python

[D](#track-d)

·

·

·

Go

[E](#track-e)

·

·

·

TypeScript

·

[C](#track-c)

·

·

JavaScript

[F](#track-f)

[F](#track-f)

·

·

Open (X→Y)

·

·

·

[H](#track-h)

**Filled cell** · jumps to that track. Each track is anchorable as **#track-a** through **#track-h**.

[Browse the recommended repo pool →](/2026/repo-pool) Or bring your own repo, no pre-approval needed.

01 / 08 A

A · C → Rust

### The DARPA direction

-   Difficulty Hard
-   Team 2–3
-   Pool ~6 repos
-   LOC 3–8k

Memory-safety migration backed by Microsoft and DARPA. c2rust produces mostly unsafe — the interesting work is what comes after.

Ideal exit criteria — north stars, not pass/fail gates

-   Passes the original C test suite, unmodified, ≥99%
-   Reduces unsafe count to under a documented threshold versus the source line count
-   Catches at least one latent bug in the original via differential fuzzing
-   Handles error paths idiomatically — Result, not errno translated

Systems engineers · Security teams · Compiler folks

02 / 08 B

B · Zig → Rust

### The Bun story

-   Difficulty Hard
-   Team 2–3
-   Pool ~4 repos
-   LOC 2–8k

The Bun-shaped problem, solved without 13,000 unsafe blocks.

Ideal exit criteria — north stars, not pass/fail gates

-   Targets a real Zig project under 8k LOC with a meaningful test suite
-   Preserves allocator behavior and explicit error sets in idiomatic Rust
-   Documents every unsafe block with a rationale in the source
-   Beats the original on at least one benchmark without sacrificing test parity

Systems engineers · Language implementers · Runtime authors

03 / 08 C

C · TypeScript → Go

### The Microsoft TS7 direction

-   Difficulty Medium
-   Team 1–3
-   Pool ~5 repos
-   LOC 3–8k

Native compilers and tools for the JS ecosystem. The defensible alternative to Rust.

Ideal exit criteria — north stars, not pass/fail gates

-   Starts measurably faster than the TypeScript original (cold + warm)
-   Preserves API compatibility for downstream consumers
-   Exposes a clean Go module structure that idiomatic Go reviewers would accept
-   Survives the original Jest/Vitest/Mocha suite when bridged through a thin adapter

Tooling authors · DX engineers · Platform teams

04 / 08 D

D · Python → Rust

### The Astral playbook

-   Difficulty Medium
-   Team 1–2
-   Pool ~7 repos
-   LOC 2–6k

The Astral lineage. Python tools that earn their right to be in Rust.

Ideal exit criteria — north stars, not pass/fail gates

-   Targets a Python tool with a published test suite (linter, formatter, CLI, parser)
-   Matches or exceeds 10× speedup on a documented workload
-   Compiles to a single binary, no Python runtime required
-   Reports honest p99 latency and RSS — not just hot-loop throughput

Python tooling maintainers · Developer-experience engineers · CLI builders

05 / 08 E

E · Go → Rust

### The Discord canon

-   Difficulty Medium
-   Team 2–3
-   Pool ~4 repos
-   LOC 3–8k

GC pauses vs predictable tail latency. The original “is GC negotiable?” story.

Ideal exit criteria — north stars, not pass/fail gates

-   Demonstrates measurable p99 latency improvement on a representative workload
-   Preserves concurrency semantics under a soak test
-   Documents memory footprint reduction with a methodology a senior SRE would accept
-   Survives a shadow-traffic-equivalent differential test against the Go original

SREs · Backend engineers · Platform teams

06 / 08 F

F · JavaScript → Go or Rust

### Runtime modernization

-   Difficulty Easy–Med
-   Team 1–2
-   Pool ~6 repos
-   LOC 2–5k

A Node-only tool, shipped as a static binary nobody needs node\_modules to run.

Ideal exit criteria — north stars, not pass/fail gates

-   Eliminates the Node runtime dependency entirely
-   Matches the original CLI surface (flags, exit codes, stdout/stderr layout)
-   Passes the original tests when re-pointed at the new binary
-   Reduces startup time on a documented benchmark

DevTool builders · CLI maintainers · Open-source authors

07 / 08 G

G · C → Zig

### The other memory-safety answer

-   Difficulty Hard
-   Team 1–2
-   Pool ~3 repos
-   LOC 2–6k

Zig's allocator discipline and explicit error sets, fixing lifetime bugs the C original has carried for years.

Ideal exit criteria — north stars, not pass/fail gates

-   Translates a real C library under 8k LOC into idiomatic Zig
-   Exposes the same C ABI for downstream consumers
-   Surfaces and documents at least one allocator-misuse bug in the original
-   Passes the original test suite via the preserved ABI

Systems engineers · Embedded folks · Language enthusiasts

08 / 08 H

H · Open Pair (X → Y)

### Surprise us

-   Difficulty Variable
-   Team 1–4
-   Pool Any defensible
-   LOC 2–8k

Ruby → Rust. PHP → Go. COBOL → Go. Java → Kotlin. Elixir → Gleam. Pick a defensible pair, justify it in your README.

Ideal exit criteria — north stars, not pass/fail gates

-   Solves a real problem the original language poorly serves
-   Maintains behavioral equivalence under the original test suite
-   Documents the migration rationale — performance, safety, ecosystem, hiring
-   Reads as idiomatic in the target language to a senior reviewer

Polyglot engineers · Legacy modernizers · Open-source maintainers

04 / Deliverables

## Deliverables

After 72 hours, we want a working port. Not a transpiler proof-of-concept. Not a slide deck.

A working port

The ideal: the original repo's full test suite, unmodified, runnable against your code. Hashed at kickoff, verified at submission. If the tests diff from what we hashed, judges weigh the diff against your DECISIONS.md — it costs Test Parity points but doesn't disqualify.

Actual behavioral equivalence

Not just a successful compile. Show us a differential fuzz session. Show us the diff in CLI outputs on a shared input set. Show us the benchmark with p99, RSS, startup time — not just hot-loop throughput.

Honest numbers

unsafe block count. any count. Test pass rate per file. Coverage diff. Judges will trust a team that says “we got 94% test parity and here's the failing edge case” over a team that claims 100% and can't reproduce it on demand.

You'll submit

---

-   01 Public GitHub repo with the port
-   02 Build command that produces a working binary or runnable artifact in one step
-   03 The original repo's test suite, hashed at kickoff, passing against your port (ideal target — partial passes still score)
-   04 Differential fuzz harness (we'll provide a template)
-   05 DECISIONS.md — every non-trivial architectural divergence from the original, with rationale
-   06 Benchmark report — original vs port, on a shared workload, with methodology
-   07 5-minute demo video showing the original test suite passing live against your port

05 / Anatomy of a working port

## Anatomy of a working port

What a serious submission looks like on disk.

your-port/

├── README.md ← migration rationale, build instructions

├── DECISIONS.md ← every non-trivial divergence + why

├── Dockerfile ← one command to a runnable artifact

├── src/ ← your idiomatic target-language code

├── tests/original/ ← hashed at kickoff · ideal: unmodified

├── tests/port/ ← new tests you added (optional, encouraged)

├── fuzz/

│ ├── harness.\* ← differential fuzzer template

│ └── log.txt ← 60s+ run, zero divergences (if claiming bonus)

├── bench/

│ ├── methodology.md ← how you measured

│ └── results.json ← p99, RSS, startup, throughput

└── .port-mortem.toml ← track letter, source URL, kickoff hash

-   01
    
    One command builds.
    
    make, cargo build, or docker compose up. If we have to read your CI to figure out how, you've failed this rule.
    
-   02
    
    Tests are the north star.
    
    Ideal: original suite file-hashes match what we pinned at kickoff. Add tests freely; touching the originals is a scoring hit, not a disqualification — document any edit.
    
-   03
    
    Decisions are documented.
    
    DECISIONS.md is read by judges and counts toward Engineering Discipline (15%). Empty bullets don't count.
    
-   04
    
    Numbers are honest.
    
    p99, RSS, startup, throughput — with methodology. Throughput-only benchmarks score below honest p99 regressions.
    

Layout is advisory. Judges read what you actually ship.

06 / Bonus points

## Bonus points

Optional. Pick one. Nail it.

Differential Fuzz Survivor +5 Hard

Run a differential fuzzer (Trail of Bits' DIFFER, or your own) against the original and the port for at least 60 continuous seconds. Zero divergences on a shared public API. Publish the fuzz log.

Zero Unsafe +5 Hard

Land your port with unsafe / any / equivalent-escape-hatch counts under a documented threshold (we'll publish per-pair thresholds at kickoff — calibrated against comparable real-world projects like uv and pingora).

Bug Catcher +3 Medium

Discover and document a latent bug in the original repo through your differential testing. File the issue upstream during the hackathon. Bonus bonus if it gets accepted.

Decision Log +3 Medium

Submit a DECISIONS.md with at least 10 non-trivial architectural divergences and their rationales. Judges will read it. Empty bullet points won't count.

07 / Out of scope

## Out of scope

These won't score well. Don't bother.

-   Hello-world translations or single-function rewrites
-   Ports that shell out to the original binary and call it a translation
-   Ports that FFI into the source-language runtime (“we ported it to Rust but link against the Python interpreter”)
-   Silently editing the original test suite to make it green — the goal is to leave them untouched; if you must edit, name the change in DECISIONS.md so judges can weigh it
-   Cherry-picking happy-path tests while ignoring concurrency or error paths
-   LLM dumps with no decision log and no human able to defend the architecture
-   Repos over 8,000 source lines (you will not finish in 72 hours; don't try)
-   Anything requiring custom hardware, GUI frameworks, or proprietary toolchains

We're not against AI-assisted porting. We're against the version of it that ships unsafe-everywhere code, edits the tests, and can't explain its own decisions.

08 / Timeline

## Timeline

All times UTC. 2026.

Pre-Event

---

Jun 29

**Registration opens** Join the Discord, scout your candidate repo.

Jul 27

**Team formation** 1–4 people per team. Solo welcome.

Jul 30

**Repo pool published** Recommended repo pool goes live. Pick one, or bring your own public repo (at least 1,000 source lines; a test suite is optional) — no pre-approval needed.

Hackathon — 72h

---

Jul 31 · 18:00 UTC

**Kickoff** Original test suites hashed and pinned. Hacking begins.

Aug 03 · 18:00 UTC

**Code freeze** Submissions due. Test parity verified live.

Post-Event

---

Aug 03 – 13

**Judging window** Each project reviewed by multiple judges on structured forms across the one-week window. Weighted scores + written feedback to every team.

Aug 10 · 18:00 UTC

**Write-Up side-quest closes** Deadline for the $300 Write-Up submissions (top 3 × $100).

Aug 14

**Winners announced** Main results + Write-Up top 3.

**Lock your spot.** Pre-registration is open — kickoff Jul 31 · 18:00 UTC.

[Register now](https://tally.so/r/A7aNP0)

09 / Scoring

## Scoring

How submissions are scored.

Each project is rated on a 5-point scale across four weighted criteria. Final ranking is the weighted average across all judges who evaluated the project.

Criterion

Weight

What it covers

Functionality & Reliability

40%

Does the port build with one command, run, and pass the original test suite unmodified? That's the ideal — partial passes still score, proportionally to how far you got. File-hash verified at submission. A 99% pass rate with zero test edits beats a 100% claim with two suspicious deletions.

Behavioral Equivalence

30%

Event-specific. Differential fuzz survival on shared inputs. Property tests survive translation; example-based tests don't. Concurrency semantics under soak. Honest p99 / RSS / startup, with methodology. Distributions compared, not just averages; confounders called out.

Code Quality

20%

Idiomatic in the target language to a senior reviewer. unsafe / any / escape-hatch ratio versus comparable real-world projects. Decision-log quality. Error-handling patterns native to the target.

Innovation

10%

Creative track-H pair choices that defend themselves. Latent bugs caught in the original via differential testing. Architectural decisions a senior reviewer would adopt upstream.

Bonus challenges

Challenge

Difficulty

Points

Differential Fuzz Survivor

Hard

+5

Zero Unsafe

Hard

+5

Bug Catcher

Medium

+3

Decision Log

Medium

+3

10 / Prizes

## Prizes

$1,800 total prize pool.

$800

### 1st Place — Grand Prize

The port that nailed test parity, survived differential fuzzing, reads as idiomatic in the target language, and made the judges say “I'd merge this upstream.”

$400

### 2nd Place — Runner-Up

Exceptional execution across the board. Strong test parity, defensible architecture, honest benchmarks. Almost took the crown.

$200

### 3rd Place

A creative translation that stood out for either the surprising language pair or the bug it caught in the original.

$100

### Bug Catcher

For the team that discovered and documented the most consequential latent bug in the original repo through their differential testing.

$300

Side Quest · NEW CATEGORY NEW · $100 × 3

### The Write-Up

**Generating a port is trivial. Explaining how you proved it holds up is the part almost nobody does. So we're paying for it.**

Retiring the Community Choice vote and putting the same $300 here instead. Popularity contests reward the biggest audience. We'd rather reward the best explanation.

What it is

Publish a post or blog about your port. What you picked, what broke, how you proved behavioral equivalence, the edge case that ate six hours, the decision you'd take back. Tag Hackathon Raptors.

Where

X, LinkedIn, Dev.to, or any developer-focused platform. Your call.

Prize

Top 3 write-ups. $100 each.

How they're judged

On insight, not follower count. We're reading for honest technical substance: the debugging story, the benchmark that disappointed you, the unsafe block you couldn't remove. A 200-follower account writing something genuinely useful beats a viral thread that says nothing. Small accounts — this one is winnable. Same standard as the main event: honest numbers over confident claims.

When

Write any time from kickoff. Submissions close Aug 10, 18:00 UTC. Winners announced Aug 14, with the main results.

Optional. Doesn't affect your main score. Submission details at kickoff.

11 / Rules

## Rules

What counts as a valid submission.

-   01
    
    Pick a Real Repo
    
    Pick from our recommended repo pool (live now), or bring your own public repo — no pre-approval needed. Your own repo should be at least 1,000 source lines (and under ~8,000), OSS-licensed, and not already ported to the target. A test suite is optional but recommended. Each track's suggested LOC range is on its track card.
    
-   02
    
    Tests Are the North Star
    
    The original repo's test suite is hashed at kickoff so judges can see exactly what changed. Leaving the originals untouched is the ideal we score toward — edits aren't auto-disqualifying, but they're weighed heavily against the rationale you write up. Add new tests freely in your port; document any edit to the originals in DECISIONS.md.
    
-   03
    
    Standalone & Runnable
    
    Your port must build with a single command (make, cargo build, docker compose up, etc.) and produce a runnable artifact. No “works on my machine” submissions.
    
-   04
    
    New Code Only
    
    All port code written during the 72-hour window. Standard library use, open-source dependencies in the target language ecosystem, AI assistance — all fair game. Pre-existing partial ports of your chosen repo are not.
    
-   05
    
    No Source-Language Runtime
    
    Your Python → Rust port cannot link against the Python interpreter. Your C → Rust port cannot FFI into the original libc-wrapping library. The whole point is to leave the source language behind.
    
-   06
    
    Team Size
    
    1–4 people. Solo entries welcome. Find teammates on the Hackathon Raptors Discord before or during the event.
    
-   07
    
    Pick a Track
    
    Choose one track A–H. Your port must clearly target that pair. Open Pair (Track H) requires a defensible rationale in your README.
    
-   08
    
    Source Code Public
    
    GitHub repo, OSI-approved license. Public at submission. Anonymous-username submissions accepted, but the team must be reachable for written follow-up by judges during the evaluation window.
    
-   09
    
    AI Tools Are Expected
    
    Cursor, Aider, Copilot, Continue, local Ollama agents, vibe-coding — bring whatever you've got, free or paid. We don't gatekeep on whether you used AI; we gatekeep on whether the port holds up. DECISIONS.md, the differential fuzz log, and the original test suite are the receipts. If the artifact can't be defended in writing, it scores accordingly.
    

12 / Who this is for

## Who this is for

If you've ever opened a 10,000-line PR labeled “Rewrite in Rust” and wondered whether anyone actually read it, this is your hackathon.

---

### Backend & Systems Engineers

Tracks · A · B · E

You've spent a year fighting GC tails or memory-corruption bugs. Now pick the migration you've been arguing for in design docs.

### SREs & Platform Engineers

Tracks · D · E · F

The reliability story is the migration story. Build the port that proves the p99 case.

### Language Implementers & Compiler Folks

Tracks · B · C · G

Cross-language translation is your home turf. Set the bar for what idiomatic looks like.

### Open-Source Maintainers

Tracks · A · D · F · H

Pick the project you wish was in a different language and prove the migration is feasible. Upstream the patch if you nail it.

### Tooling & DX Engineers

Tracks · C · D · F

The Astral playbook. Take a slow tool, ship a fast one, and report honest numbers.

### Polyglot Engineers

Tracks · H

You've shipped in five languages and have opinions. Track H exists for you. Surprise us.

### Security Engineers

Tracks · A · B · G

Memory-safety migrations are your beat. Use differential fuzzing to find bugs in the original.

13 / Featured judges

## Featured judges

Spotlighting members of the Port Mortem panel.

-   ![Anshul Kumar Purohit](/2026/judges/anshul-kumar-purohit.jpg)
    
    Anshul Kumar Purohit
    
    IBM · USA
    
    Anshul Purohit is a technology leader and Chief Architect with extensive experience in AI, cloud, enterprise architecture, and hospitality technology.
    
    [LinkedIn](https://www.linkedin.com/in/anshul-k-purohit/)
    
-   ![Karthik Kapula](/2026/judges/karthik-kapula.jpg)
    
    Karthik Kapula
    
    ICS Global Soft Inc · United States
    
    3x UiPath MVP, UiPath community Dallas chapter leader, and automation leader with 15+ years of IT experience across RPA, agentic automation, test…
    
    [LinkedIn](https://www.linkedin.com/in/karthik-kapula-a84109179/)
    
-   ![Deep Saxena](/2026/judges/deep-saxena.jpg)
    
    Deep Saxena
    
    United States
    
    Senior Software Engineer and Tech Lead at Microsoft with deep expertise in distributed storage engines, query execution, and Rust-based systems.
    
    [LinkedIn](https://www.linkedin.com/in/deep-saxena-158b82a4/)
    
-   ![Ismoil Shokirov](/2026/judges/ismoil-shokirov.jpg)
    
    Ismoil Shokirov
    
    Thread Magic · United States
    
    Ismoil Shokirov is a Full-Stack Software Engineer with 7+ years of experience building scalable web and desktop applications.
    
    [LinkedIn](https://www.linkedin.com/in/ismoil-shokirov/)
    

14 / Evaluation Protocol

## Evaluation Protocol

How every submission gets reviewed. Fully async, multi-judge, weighted-average, signed protocol per reviewer — no live calls, no interviews.

-   Format
    
    Fully online and asynchronous. No live panels, no scheduled calls. Every project reviewed independently by multiple judges drawn from the panel.
    
-   Panel
    
    Senior engineers, architects, and technical leaders whose expertise spans the event's domain — compiler engineering, SRE, A/B and differential analysis, security, language tooling, RPA and agentic automation.
    
-   Batches
    
    Projects grouped into batches of ~10–12. Each judge is assigned one batch on a structured online form.
    
-   Time commitment
    
    1–2 hours per week over a one-week evaluation window. Comfortable around a full-time role.
    
-   Criteria
    
    Four weighted criteria on a 5-point scale, tailored to Port Mortem. See §09 / Scoring for the rubric.
    
-   Output
    
    Weighted score per project · written feedback to every team, including distributional analysis where relevant · signed Evaluation Protocol issued to each judge as a permanent record of their participation.
    

Every team receives

Weighted score · written feedback · judge initials (panel anonymised).

Know someone? Email [hello@raptors.dev](mailto:hello@raptors.dev?subject=Judge%20Nomination%20%E2%80%94%20Port%20Mortem) with the subject `Judge Nomination — Port Mortem`.

15 / Judging panel

## Judging panel

Every judge reviewing Port Mortem submissions — plus the Raptors community group reviewing alongside them.

The full judging panel

[![Alok Kumar](/2026/judges/alok-kumar.jpg)

Alok Kumar

Nvidia

](https://www.linkedin.com/in/alokrai7/)[![Amit Kumar Verma](/2026/judges/amit-kumar-verma.jpg)

Amit Kumar Verma

CyberSolve

](https://www.linkedin.com/in/amit-kr-verma/)[![Artem Dolia](/2026/judges/artem-dolia.jpg)

Artem Dolia

Oracle

](https://www.linkedin.com/in/artdolya/)[![Badal Shah](/2026/judges/badal-shah.jpg)

Badal Shah

Neolytica - A QPharma Company

](https://www.linkedin.com/in/shahbadal/)[![Daniil Khudenko](/2026/judges/daniil-khudenko.jpg)

Daniil Khudenko](https://www.linkedin.com/in/dkhudenko/) [![Duygu Unlu](/2026/judges/duygu-unlu.jpg)

Duygu Unlu

FAMETECH INC

](https://www.linkedin.com/in/duygugunduzunlu/)[![Gaurav Agrawal](/2026/judges/gaurav-agrawal.jpg)

Gaurav Agrawal](https://www.linkedin.com/in/gaurav-agrawal-a90baa14/) [![Irina Ivchenkova](/2026/judges/irina-ivchenkova.jpg)

Irina Ivchenkova](https://www.linkedin.com/in/irina-ivch/) [![Janardhana Naidu Kola](/2026/judges/janardhana-naidu-kola.jpg)

Janardhana Naidu Kola

ADP

](https://www.linkedin.com/in/janardhana-kola/)[![Jolly Shah](/2026/judges/jolly-shah.jpg)

Jolly Shah

Google

](https://www.linkedin.com/in/jolly-shah-2a55b68/)[![madhvi sharma](/2026/judges/madhvi-sharma.jpg)

madhvi sharma](https://www.linkedin.com/in/madhvisharma/) [![Maxim Zolotarev](/2026/judges/maxim-zolotarev.jpg)

Maxim Zolotarev

Tabby

](https://www.linkedin.com/in/maksim-zolotarev-a553b8177/)[![Nissan Modi](/2026/judges/nissan-modi.jpg)

Nissan Modi

Coinbase

](https://www.linkedin.com/in/nissanmodi/?skipRedirect=true)[![Nitish Ratan](/2026/judges/nitish-ratan.jpg)

Nitish Ratan](https://www.linkedin.com/in/nitish-ratan-appanasamy-03a353a3/) [![Oleksandr Shevchenko](/2026/judges/oleksandr-shevchenko.jpg)

Oleksandr Shevchenko](https://www.linkedin.com/in/al3xshevchenko/) [![Prajwal Pitlehra](/2026/judges/prajwal-pitlehra.jpg)

Prajwal Pitlehra

Monaco Research

](https://www.linkedin.com/in/prajwalpitlehra/)[![Ram Sekhar Bodala](/2026/judges/ram-sekhar-bodala.jpg)

Ram Sekhar Bodala

AMTRAK

](https://www.linkedin.com/in/ramsekhar/)[![Ravindra Rajasekhar Kavuru](/2026/judges/ravindra-rajasekhar-kavuru.jpg)

Ravindra Rajasekhar Kavuru](https://www.linkedin.com/in/ravindrakavuru/) [![Rosh Perumpully Ramadass](/2026/judges/rosh-perumpully-ramadass.jpg)

Rosh Perumpully Ramadass

Zscaler

](https://www.linkedin.com/in/roshpr/)[![Sai Manoj Jayakannan](/2026/judges/sai-manoj-jayakannan.jpg)

Sai Manoj Jayakannan](www.linkedin.com/in/jsai-manoj) [![Sapna Pillai](/2026/judges/sapna-pillai.jpg)

Sapna Pillai

Ernst & Young

](https://www.linkedin.com/in/sapna-pillai-20946a11/)[![Satya Sagar Reddi](/2026/judges/satya-sagar-reddi.jpg)

Satya Sagar Reddi

Microsoft

](https://www.linkedin.com/in/sagar-reddi-92bb7944/)[![Shon Thomas](/2026/judges/shon-thomas.jpg)

Shon Thomas

Yahoo

](http://lnkd.in/bC3Wsz9)[![Sonali Sheetal Appikonda](/2026/judges/sonali-sheetal-appikonda.jpg)

Sonali Sheetal Appikonda

Citizens Property Insurance

](https://www.linkedin.com/in/sonali-sheetal-appikonda-a30712237/)[![Srinivasa Rao Gunda](/2026/judges/srinivasa-rao-gunda.jpg)

Srinivasa Rao Gunda](https://www.linkedin.com/in/srinivasa-rao-gunda-8787a320b/) [![Starkov Kirill](/2026/judges/starkov-kirill.jpg)

Starkov Kirill

Refact.ai

](https://www.linkedin.com/in/kirill-starkov/)[![sunil kumar vytla](/2026/judges/sunil-kumar-vytla.jpg)

sunil kumar vytla](https://www.linkedin.com/in/sunil-kumar-vytla-8b88a7132) [![Swamy Biru](/2026/judges/swamy-biru.jpg)

Swamy Biru](https://www.linkedin.com/in/swamy-biru/) [![Tanay Chowdhury](/2026/judges/tanay-chowdhury.jpg)

Tanay Chowdhury](https://www.linkedin.com/in/tanayz/) [![Venkata Rama Uday Bokam](/2026/judges/venkata-rama-uday-bokam.jpg)

Venkata Rama Uday Bokam](https://www.linkedin.com/in/uday-kiran-bokam-13a041129/) [![Vicky Katara](/2026/judges/vicky-katara.jpg)

Vicky Katara

Amazon Web Services

](https://www.linkedin.com/in/vickykatara/)[![Vijay Anthony Richard](/2026/judges/vijay-anthony-richard.jpg)

Vijay Anthony Richard

Amazon

](https://www.linkedin.com/in/vijayanthonyrichard/)[![Vyom Mittal](/2026/judges/vyom-mittal.jpg)

Vyom Mittal

Amazon

](https://www.linkedin.com/in/vyommittal/)[![Alok Kumar](/2026/judges/alok-kumar.jpg)

Alok Kumar

Nvidia

](https://www.linkedin.com/in/alokrai7/)[![Amit Kumar Verma](/2026/judges/amit-kumar-verma.jpg)

Amit Kumar Verma

CyberSolve

](https://www.linkedin.com/in/amit-kr-verma/)[![Artem Dolia](/2026/judges/artem-dolia.jpg)

Artem Dolia

Oracle

](https://www.linkedin.com/in/artdolya/)[![Badal Shah](/2026/judges/badal-shah.jpg)

Badal Shah

Neolytica - A QPharma Company

](https://www.linkedin.com/in/shahbadal/)[![Daniil Khudenko](/2026/judges/daniil-khudenko.jpg)

Daniil Khudenko](https://www.linkedin.com/in/dkhudenko/) [![Duygu Unlu](/2026/judges/duygu-unlu.jpg)

Duygu Unlu

FAMETECH INC

](https://www.linkedin.com/in/duygugunduzunlu/)[![Gaurav Agrawal](/2026/judges/gaurav-agrawal.jpg)

Gaurav Agrawal](https://www.linkedin.com/in/gaurav-agrawal-a90baa14/) [![Irina Ivchenkova](/2026/judges/irina-ivchenkova.jpg)

Irina Ivchenkova](https://www.linkedin.com/in/irina-ivch/) [![Janardhana Naidu Kola](/2026/judges/janardhana-naidu-kola.jpg)

Janardhana Naidu Kola

ADP

](https://www.linkedin.com/in/janardhana-kola/)[![Jolly Shah](/2026/judges/jolly-shah.jpg)

Jolly Shah

Google

](https://www.linkedin.com/in/jolly-shah-2a55b68/)[![madhvi sharma](/2026/judges/madhvi-sharma.jpg)

madhvi sharma](https://www.linkedin.com/in/madhvisharma/) [![Maxim Zolotarev](/2026/judges/maxim-zolotarev.jpg)

Maxim Zolotarev

Tabby

](https://www.linkedin.com/in/maksim-zolotarev-a553b8177/)[![Nissan Modi](/2026/judges/nissan-modi.jpg)

Nissan Modi

Coinbase

](https://www.linkedin.com/in/nissanmodi/?skipRedirect=true)[![Nitish Ratan](/2026/judges/nitish-ratan.jpg)

Nitish Ratan](https://www.linkedin.com/in/nitish-ratan-appanasamy-03a353a3/) [![Oleksandr Shevchenko](/2026/judges/oleksandr-shevchenko.jpg)

Oleksandr Shevchenko](https://www.linkedin.com/in/al3xshevchenko/) [![Prajwal Pitlehra](/2026/judges/prajwal-pitlehra.jpg)

Prajwal Pitlehra

Monaco Research

](https://www.linkedin.com/in/prajwalpitlehra/)[![Ram Sekhar Bodala](/2026/judges/ram-sekhar-bodala.jpg)

Ram Sekhar Bodala

AMTRAK

](https://www.linkedin.com/in/ramsekhar/)[![Ravindra Rajasekhar Kavuru](/2026/judges/ravindra-rajasekhar-kavuru.jpg)

Ravindra Rajasekhar Kavuru](https://www.linkedin.com/in/ravindrakavuru/) [![Rosh Perumpully Ramadass](/2026/judges/rosh-perumpully-ramadass.jpg)

Rosh Perumpully Ramadass

Zscaler

](https://www.linkedin.com/in/roshpr/)[![Sai Manoj Jayakannan](/2026/judges/sai-manoj-jayakannan.jpg)

Sai Manoj Jayakannan](www.linkedin.com/in/jsai-manoj) [![Sapna Pillai](/2026/judges/sapna-pillai.jpg)

Sapna Pillai

Ernst & Young

](https://www.linkedin.com/in/sapna-pillai-20946a11/)[![Satya Sagar Reddi](/2026/judges/satya-sagar-reddi.jpg)

Satya Sagar Reddi

Microsoft

](https://www.linkedin.com/in/sagar-reddi-92bb7944/)[![Shon Thomas](/2026/judges/shon-thomas.jpg)

Shon Thomas

Yahoo

](http://lnkd.in/bC3Wsz9)[![Sonali Sheetal Appikonda](/2026/judges/sonali-sheetal-appikonda.jpg)

Sonali Sheetal Appikonda

Citizens Property Insurance

](https://www.linkedin.com/in/sonali-sheetal-appikonda-a30712237/)[![Srinivasa Rao Gunda](/2026/judges/srinivasa-rao-gunda.jpg)

Srinivasa Rao Gunda](https://www.linkedin.com/in/srinivasa-rao-gunda-8787a320b/) [![Starkov Kirill](/2026/judges/starkov-kirill.jpg)

Starkov Kirill

Refact.ai

](https://www.linkedin.com/in/kirill-starkov/)[![sunil kumar vytla](/2026/judges/sunil-kumar-vytla.jpg)

sunil kumar vytla](https://www.linkedin.com/in/sunil-kumar-vytla-8b88a7132) [![Swamy Biru](/2026/judges/swamy-biru.jpg)

Swamy Biru](https://www.linkedin.com/in/swamy-biru/) [![Tanay Chowdhury](/2026/judges/tanay-chowdhury.jpg)

Tanay Chowdhury](https://www.linkedin.com/in/tanayz/) [![Venkata Rama Uday Bokam](/2026/judges/venkata-rama-uday-bokam.jpg)

Venkata Rama Uday Bokam](https://www.linkedin.com/in/uday-kiran-bokam-13a041129/) [![Vicky Katara](/2026/judges/vicky-katara.jpg)

Vicky Katara

Amazon Web Services

](https://www.linkedin.com/in/vickykatara/)[![Vijay Anthony Richard](/2026/judges/vijay-anthony-richard.jpg)

Vijay Anthony Richard

Amazon

](https://www.linkedin.com/in/vijayanthonyrichard/)[![Vyom Mittal](/2026/judges/vyom-mittal.jpg)

Vyom Mittal

Amazon

](https://www.linkedin.com/in/vyommittal/)

Community judges

[![Keshav Varshney](/2026/judges/keshav-varshney.jpg)

Keshav Varshney](https://linkedin.com/in/ikeshavvarshney) [![Maaz](/2026/judges/maaz.jpg)

Maaz](https://www.linkedin.com/in/maaz--/) [![Pratik Rasam](/2026/judges/pratik-rasam.jpg)

Pratik Rasam](https://www.linkedin.com/in/pratik-rasam-5a90294b/) 

16 / FAQ

## FAQ

---

What's the team size limit?

1–4 people. Solo welcome. We recommend 2–3 for the 72-hour window — bigger teams hit coordination overhead fast on a port project.

Can I use AI code generation?

Yes, and you should. Cursor, Aider, Copilot, Continue, local Ollama agents — anything from free OSS tools to paid SaaS is expected. We don't gatekeep AI use; we gatekeep the artifact. DECISIONS.md, the differential fuzz log, and the original test suite are the receipts. If the port can't be defended in writing, it scores accordingly.

Can I pick any GitHub repo?

Yes. Pick from our recommended pool, or bring your own public repo — no pre-approval needed. Your own repo should be at least 1,000 source lines, OSS-licensed, and not already ported to the target language. A test suite is optional but recommended. Track H (Open Pair) also wants the language pair justified in your README.

What are the LOC limits?

Each track has a suggested range on its card. If you bring your own repo, the floor is about 1,000 source lines and the ceiling is ~8,000. Bigger than that and 72 hours becomes a compile, not a proven port — Bun took 6 days with an agent fleet and merged with 13,000 unsafe blocks, and you have less.

Can I edit the original test suite?

You can — but it hurts your score. The file hashes are pinned at kickoff, so judges will see the diff. Leaving the originals untouched is the goal we aim at; edits are weighed against the rationale you document, not auto-disqualified. The lesson from the Bun merge is that silent edits poison the signal — if you have to touch them, explain why in DECISIONS.md.

What if the original repo has flaky tests?

Document the flakes in your DECISIONS.md, exclude them from your parity calculation with a written rationale, and judges will assess case-by-case. Don't silently skip them.

What counts as “passing the original test suite”?

The original repo's tests run against your port's binary/artifact via a thin adapter (we'll provide templates for each track). Pass rate = passing tests / total tests. ≥99% with no edits is the ideal that scores top marks — lower pass rates still score, just proportionally less.

Can I work on my port before July 31?

No code. Planning, candidate-repo scouting, agent prompt tuning, reading the source — all fine. Any port code committed before kickoff disqualifies the project.

What's the differential fuzz session?

A harness that runs the original and your port on the same inputs and diffs the outputs. We'll provide templates for CLI tools, libraries with HTTP APIs, and parser/serializer projects. Surviving 60+ continuous seconds with zero divergence earns the bonus.

Can the port be slower than the original?

Yes, but honest reporting is required. If your port is 2× slower, say so and explain why. Hiding the regression scores worse than disclosing it. Top placements need behavioral equivalence first; performance is a tiebreaker.

Do I have to use any specific tools?

No. Use whatever ships your port in 72 hours.

What if my port discovers a bug in the original?

File the upstream issue during the hackathon. Document it in your submission. That's the Bug Catcher bonus. The whole point of differential testing is that the rewriter often sees what the original maintainers missed.

References

## References

1.  \[1\] BigGo / Theo, “Bun Zig→Rust merge analysis”  
    [https://finance.biggo.com/news/cdd79ba072c5c5d9](https://finance.biggo.com/news/cdd79ba072c5c5d9)
2.  \[2\] The Register, “Anthropic's Bun Rust rewrite merged at the speed of AI”  
    [https://www.theregister.com/devops/2026/05/14/anthropics-bun-rust-rewrite-merged-at-speed-of-ai/](https://www.theregister.com/devops/2026/05/14/anthropics-bun-rust-rewrite-merged-at-speed-of-ai/)
3.  \[3\] Byteiota, “The 13,000 unsafe block problem”  
    [https://byteiota.com/bun-rust-rewrite-merged-the-13000-unsafe-block-problem/](https://byteiota.com/bun-rust-rewrite-merged-the-13000-unsafe-block-problem/)
4.  \[4\] Techzine, “Bun's Rust rewrite raises questions”  
    [https://www.techzine.eu/news/devops/141364/](https://www.techzine.eu/news/devops/141364/)
5.  \[5\] The New Stack, “Microsoft's bold goal: replace 1B lines of C/C++ with Rust”  
    [https://thenewstack.io/microsofts-bold-goal-replace-1b-lines-of-c-c-with-rust/](https://thenewstack.io/microsofts-bold-goal-replace-1b-lines-of-c-c-with-rust/)
6.  \[6\] IT Pro, “DARPA wants to accelerate C-to-Rust translation with AI (TRACTOR program)”  
    [https://www.itpro.com/software/development/darpa-wants-to-accelerate-translation-of-c-code-to-rust-and-its-relying-on-ai-to-do-it](https://www.itpro.com/software/development/darpa-wants-to-accelerate-translation-of-c-code-to-rust-and-its-relying-on-ai-to-do-it)
7.  \[7\] Microsoft DevBlogs, “A 10× faster TypeScript (native port to Go)”  
    [https://devblogs.microsoft.com/typescript/typescript-native-port/](https://devblogs.microsoft.com/typescript/typescript-native-port/)
8.  \[8\] Discord Engineering, “Why Discord is switching from Go to Rust”  
    [https://discord.com/blog/why-discord-is-switching-from-go-to-rust](https://discord.com/blog/why-discord-is-switching-from-go-to-rust)
9.  \[9\] Trail of Bits, “Introducing DIFFER”  
    [https://blog.trailofbits.com/2024/01/31/introducing-differ-a-new-tool-for-testing-and-validating-transformed-programs/](https://blog.trailofbits.com/2024/01/31/introducing-differ-a-new-tool-for-testing-and-validating-transformed-programs/)
10.  \[10\] Astral, “uv: an extremely fast Python package and project manager”  
    [https://github.com/astral-sh/uv](https://github.com/astral-sh/uv)
11.  \[11\] Cloudflare, “Pingora: open-sourcing our Rust framework”  
    [https://blog.cloudflare.com/pingora-open-source/](https://blog.cloudflare.com/pingora-open-source/)
12.  \[12\] Immunant, “c2rust: C to Rust translator”  
    [https://github.com/immunant/c2rust](https://github.com/immunant/c2rust)
13.  \[13\] arXiv, “SmartC2Rust: LLM-assisted unsafe reduction”  
    [https://arxiv.org/html/2409.10506](https://arxiv.org/html/2409.10506)
14.  \[14\] arXiv, “Differential fuzzing for LLM-generated code translations”  
    [https://arxiv.org/pdf/2602.15761](https://arxiv.org/pdf/2602.15761)

Media partner

## Media partner

[![Eventopia — Official Media Partner](/2026/partners/eventopia.svg)Official Media PartnerCollege fests & events discovery · India](https://eventopia.in)

Build a port that holds up.

JUL 31 — AUG 03 · 72H · ONLINE

[Register for Port Mortem](https://tally.so/r/A7aNP0) [Join the Discord](https://discord.gg/xfYPDZYqeh)