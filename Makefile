# go-picomatch — Port Mortem 2026, Track F (JavaScript -> Go)
#
# `make build` is the one-command build. Everything else is optional.

GO      ?= go
NPM     ?= npm
PKG     := ./...
EXTRACT := tools/extract

.DEFAULT_GOAL := help

## build: compile the module (the one-command build)
.PHONY: build
build:
	$(GO) build $(PKG)

## test: run the port's own test suite
.PHONY: test
test:
	$(GO) test $(PKG)

## check: everything CI enforces — format, vet, tests, and upstream integrity
.PHONY: check
check: fmt-check vet test verify-original

## fmt: rewrite Go sources in canonical form
.PHONY: fmt
fmt:
	$(GO) fmt $(PKG)

## fmt-check: fail if any Go source is not gofmt-clean
.PHONY: fmt-check
fmt-check:
	@unformatted=$$(gofmt -l . 2>/dev/null); \
	if [ -n "$$unformatted" ]; then \
		echo "not gofmt-clean:"; echo "$$unformatted"; exit 1; \
	fi
	@echo "gofmt clean"

## vet: static checks, including the build-tagged conformance harness
.PHONY: vet
vet:
	$(GO) vet $(PKG)
	$(GO) vet -tags conformance $(PKG)

## cover: test coverage for the port
.PHONY: cover
cover:
	$(GO) test -coverprofile=coverage.out $(PKG)
	$(GO) tool cover -func=coverage.out | tail -1

# --- Behavioural parity ------------------------------------------------------

## conformance: replay recorded behaviour and report the parity rate, per fixture
## set. Reports only. Set PICOMATCH_PARITY_MIN=95 to make it a gate.
.PHONY: conformance
conformance:
	$(GO) test -tags conformance -run 'TestConformance|TestCharacterAxis' -v $(PKG)

## tokens: report parser parity against the recorded token streams — moves long
## before conformance can. PICOMATCH_TOKENS_MIN=95 gates it. No Node needed.
.PHONY: tokens
tokens:
	$(GO) test -tags conformance -run 'TestToken' -v $(PKG)

## emit: replay testdata/emit against the port's emitter -> emitter %, stratified
## by layer. PICOMATCH_EMIT_MIN=25 gates it; any `wrong` field fails regardless.
.PHONY: emit
emit:
	$(GO) test -tags conformance -run 'TestEmit|TestCompareEmit' -v $(PKG)

# --- Test-extraction pipeline (needs Node; not required to build or test) ----

## deps: install the extractor's Node dependencies
.PHONY: deps
deps:
	cd $(EXTRACT) && $(NPM) install --no-audit --no-fund

## vendor: re-pin tests/original to the upstream commit and rewrite MANIFEST.json
.PHONY: vendor
vendor:
	cd $(EXTRACT) && node vendor.js

## verify-original: prove the vendored upstream suite is byte-for-byte unmodified
.PHONY: verify-original
verify-original:
	cd $(EXTRACT) && node verify.js

## extract: re-record fixtures from the unmodified upstream suite
.PHONY: extract
extract: verify-original
	cd $(EXTRACT) && node extract.js

## charaxis: regenerate the supplementary character-domain fixtures
.PHONY: charaxis
charaxis: verify-original
	node tools/charaxis/generate.js

## tokens-fixture: re-record testdata/tokens from upstream's parser (rare)
.PHONY: tokens-fixture
tokens-fixture: verify-original
	node tools/tokens/generate.js

## emit-fixture: regenerate testdata/emit from the vendored upstream
.PHONY: emit-fixture
emit-fixture: verify-original
	node tools/emit/generate.js

## mutate: measure what the fixture sets can detect (see tools/mutate/README.md)
.PHONY: mutate
mutate: verify-original
	node tools/mutate/run.js

# --- Diagnostics -------------------------------------------------------------

## probes: measure upstream's structure — parsers, tokens, boundaries (reports only)
.PHONY: probes
probes: verify-original
	node tools/probes/fastpath-diff.js
	node tools/probes/token-inventory.js
	node tools/probes/fingerprint.js

## build-order: what each unbuilt branch would unblock, from the recorded tokens
.PHONY: build-order
build-order:
	node tools/probes/build-order.js

## probes-data: write the probe artifacts to testdata/probes/ (gitignored)
.PHONY: probes-data
probes-data: verify-original
	OUT=testdata/probes/fingerprints.jsonl node tools/probes/fingerprint.js
	OUT=testdata/probes/tokens.jsonl node tools/probes/token-inventory.js
	OUT=testdata/probes/fastpath-divergent.jsonl node tools/probes/fastpath-diff.js

## help: list targets
.PHONY: help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## /  /'
