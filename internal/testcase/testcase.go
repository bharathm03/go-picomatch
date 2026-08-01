// Package testcase loads the behavioural fixtures extracted from upstream
// picomatch.
//
// Each fixture is one call the unmodified upstream Mocha suite made into
// picomatch, together with what picomatch returned. The Go conformance harness
// replays them; nothing here depends on the port itself, so the fixtures can be
// inspected and counted even before a single matcher exists.
//
// Fixtures are produced by tools/extract. See tools/extract/README.md.
package testcase

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// Platform names the path semantics a case was recorded under. picomatch's entry
// point picks slash handling from the host OS, so the same pattern legitimately
// has two answers and both are recorded.
const (
	PlatformPosix   = "posix"
	PlatformWindows = "windows"
)

// Outcome values for the upstream test a case was observed in.
const (
	OutcomePassed = "passed"
	OutcomeFailed = "failed"
)

// Case is a single recorded call into picomatch.
type Case struct {
	ID       int    `json:"id"`
	Platform string `json:"platform"`

	// Module is the upstream file the function came from ("index",
	// "lib/scan", ...); API is the function name, or "matcher" for a call to the
	// function returned by a picomatch factory.
	Module string `json:"module"`
	API    string `json:"api"`

	// Construct holds the picomatch(glob, options, returnState) arguments a
	// matcher was built from. Nil for every API except "matcher".
	Construct []json.RawMessage `json:"construct"`

	// Args are the call's arguments; Result is its return value.
	Args   []json.RawMessage `json:"args"`
	Result json.RawMessage   `json:"result"`

	// Error is set instead of Result when the call threw.
	Error *JSError `json:"error"`

	// Portable is false when an argument contains a callback, which the Go
	// harness cannot reconstruct. Truncated is true when a recorded value was
	// cut short as cyclic or too deep.
	Portable  bool `json:"portable"`
	Truncated bool `json:"truncated"`

	// Occurrences counts how many identical calls the suite made; the fixture
	// keeps one. It is a weight, not a repetition count.
	Occurrences int `json:"occurrences"`

	// Provenance: the upstream spec and test this call was first seen in.
	Spec        string `json:"spec"`
	Test        string `json:"test"`
	TestOutcome string `json:"testOutcome"`
}

// Replayable reports whether a case can be used as a conformance assertion.
//
// Cases are excluded when they take a callback (nothing to replay), when the
// recorded value was truncated (the expectation is incomplete), or when the
// upstream test itself failed (the observation is real but describes a path
// upstream does not consider correct). Excluding them keeps the parity
// denominator honest rather than flattering.
func (c *Case) Replayable() bool {
	return c.Portable && !c.Truncated && c.TestOutcome == OutcomePassed
}

// Name is a stable, readable identifier for use as a Go subtest name.
func (c *Case) Name() string {
	return fmt.Sprintf("%d/%s/%s.%s", c.ID, c.Platform, c.Module, c.API)
}

// Windows reports whether the case was recorded under Windows path semantics.
func (c *Case) Windows() bool { return c.Platform == PlatformWindows }

// DecodedArgs decodes the call's arguments into Go values.
func (c *Case) DecodedArgs() ([]any, error) { return decodeAll(c.Args) }

// DecodedConstruct decodes the picomatch(glob, options, ...) arguments a matcher
// was built from. Returns nil for non-matcher cases.
func (c *Case) DecodedConstruct() ([]any, error) {
	if c.Construct == nil {
		return nil, nil
	}
	return decodeAll(c.Construct)
}

// DecodedResult decodes the call's return value.
func (c *Case) DecodedResult() (any, error) {
	if len(c.Result) == 0 {
		return nil, nil
	}
	return decode(c.Result)
}

func decodeAll(raws []json.RawMessage) ([]any, error) {
	out := make([]any, len(raws))
	for i, raw := range raws {
		v, err := decode(raw)
		if err != nil {
			return nil, fmt.Errorf("argument %d: %w", i, err)
		}
		out[i] = v
	}
	return out, nil
}

// Load reads every case from a JSONL fixture file.
func Load(path string) ([]Case, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Read(f)
}

// Read parses JSONL cases from r.
func Read(r io.Reader) ([]Case, error) {
	var cases []Case

	sc := bufio.NewScanner(r)
	// Recorded parse states run to a few kilobytes; the default 64 KiB token
	// limit would silently truncate them into parse errors.
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for line := 1; sc.Scan(); line++ {
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}

		var c Case
		if err := json.Unmarshal([]byte(text), &c); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		cases = append(cases, c)
	}

	if err := sc.Err(); err != nil {
		return nil, err
	}
	return cases, nil
}

// Stats summarises a fixture set.
type Stats struct {
	Total      int
	Replayable int
	Unportable int
	Truncated  int
	ByAPI      map[string]int
	ByPlatform map[string]int
}

// Summarise counts a fixture set by category.
func Summarise(cases []Case) Stats {
	s := Stats{
		Total:      len(cases),
		ByAPI:      map[string]int{},
		ByPlatform: map[string]int{},
	}

	for i := range cases {
		c := &cases[i]
		if c.Replayable() {
			s.Replayable++
		}
		if !c.Portable {
			s.Unportable++
		}
		if c.Truncated {
			s.Truncated++
		}
		s.ByAPI[c.Module+"."+c.API]++
		s.ByPlatform[c.Platform]++
	}

	return s
}
