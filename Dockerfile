# One command to a verified artifact:
#
#   docker build -t go-picomatch .
#
# The build compiles the module, proves the vendored upstream test suite is
# byte-for-byte unmodified, and runs the port's own tests. It fails if any of
# those fail, so a green image is a real signal rather than a copy step.
#
# Node is present only for the offline test-extraction pipeline. The Go package
# itself has no JavaScript at runtime and no dependency outside the standard
# library.

FROM golang:1.26-bookworm AS build

# Node is here only to run tools/extract/verify.js, which re-hashes the vendored
# upstream suite. Without it the build could not make the integrity claim above;
# this stage is discarded, so it costs the final image nothing.
RUN apt-get update \
 && apt-get install -y --no-install-recommends nodejs \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /src

# No third-party Go modules yet; copying go.mod alone still gives the dependency
# graph its own cache layer for when that changes.
COPY go.mod ./
RUN go mod download

COPY . .

# The integrity receipt: tests/original must still match MANIFEST.json. This runs
# before anything else, so an edited upstream suite fails the build rather than
# reaching the point where it could inflate a parity number.
RUN node tools/extract/verify.js

RUN gofmt -l . | tee /tmp/unformatted && test ! -s /tmp/unformatted
RUN go vet ./... && go vet -tags conformance ./...
RUN go build ./...
RUN go test ./...

# Confirm the fixtures the parity claim rests on still load and still agree with
# their manifest.
RUN go test -tags conformance -run TestConformance ./...


FROM golang:1.26-bookworm

# Node is needed only to re-run extraction (`make extract`); the port does not
# use it.
RUN apt-get update \
 && apt-get install -y --no-install-recommends nodejs npm make \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=build /src /app

# Default: prove the vendored upstream suite is unmodified, then run the tests.
CMD ["make", "check"]
