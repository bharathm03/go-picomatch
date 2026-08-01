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

## conformance: replay upstream's recorded behaviour and report the parity rate.
## Reports only. Set PICOMATCH_PARITY_MIN=95 to make it a gate.
.PHONY: conformance
conformance:
	$(GO) test -tags conformance -run TestConformance -v $(PKG)

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

## help: list targets
.PHONY: help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## /  /'
