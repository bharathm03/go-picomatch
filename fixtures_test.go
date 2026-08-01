package picomatch_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/bharathm03/go-picomatch/internal/testcase"
)

// These tests guard the extraction pipeline's output. They are the reason a
// scaffolding-stage repo has a meaningful green suite: the port is unimplemented,
// but the evidence it will be built against is already verifiable.

const (
	casesPath   = "testdata/original/cases.jsonl"
	summaryPath = "testdata/original/summary.json"
)

type summary struct {
	Upstream struct {
		Version string `json:"version"`
		Commit  string `json:"commit"`
	} `json:"upstream"`
	UpstreamSuite map[string]struct {
		Tests    int `json:"tests"`
		Passes   int `json:"passes"`
		Failures int `json:"failures"`
	} `json:"upstreamSuite"`
	Cases struct {
		Total      int `json:"total"`
		Replayable int `json:"replayable"`
	} `json:"cases"`
	Conflicts []any `json:"conflicts"`
}

func loadSummary(t *testing.T) summary {
	t.Helper()
	data, err := os.ReadFile(summaryPath)
	if err != nil {
		t.Fatalf("read summary: %v\n\nRun `make extract` to generate fixtures.", err)
	}
	var s summary
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("parse summary: %v", err)
	}
	return s
}

func loadCases(t *testing.T) []testcase.Case {
	t.Helper()
	cases, err := testcase.Load(casesPath)
	if err != nil {
		t.Fatalf("load cases: %v\n\nRun `make extract` to generate fixtures.", err)
	}
	return cases
}

func TestFixturesLoad(t *testing.T) {
	cases := loadCases(t)
	if len(cases) == 0 {
		t.Fatal("no cases extracted")
	}

	stats := testcase.Summarise(cases)
	t.Logf("total=%d replayable=%d unportable=%d truncated=%d",
		stats.Total, stats.Replayable, stats.Unportable, stats.Truncated)
	for api, n := range stats.ByAPI {
		t.Logf("  %-26s %d", api, n)
	}
}

// The summary is what the README and DECISIONS.md quote. If it drifts from the
// fixture file, the published numbers are wrong.
func TestSummaryMatchesFixtures(t *testing.T) {
	cases := loadCases(t)
	s := loadSummary(t)
	stats := testcase.Summarise(cases)

	if s.Cases.Total != stats.Total {
		t.Errorf("summary says %d cases, fixture has %d", s.Cases.Total, stats.Total)
	}
	if s.Cases.Replayable != stats.Replayable {
		t.Errorf("summary says %d replayable, fixture has %d", s.Cases.Replayable, stats.Replayable)
	}
}

// Extraction must be deterministic: identical inputs may never have produced
// different recorded outputs.
func TestExtractionHadNoConflicts(t *testing.T) {
	if s := loadSummary(t); len(s.Conflicts) != 0 {
		t.Errorf("extraction recorded %d non-deterministic cases", len(s.Conflicts))
	}
}

// The upstream suite must have passed in full while being recorded. A failing
// upstream test means the recorder perturbed it, and the fixtures would then
// encode behaviour picomatch does not actually have.
func TestUpstreamSuitePassedDuringExtraction(t *testing.T) {
	s := loadSummary(t)
	if len(s.UpstreamSuite) == 0 {
		t.Fatal("summary records no upstream suite run")
	}

	for platform, run := range s.UpstreamSuite {
		if run.Tests == 0 {
			t.Errorf("%s: no tests ran", platform)
		}
		if run.Failures != 0 {
			t.Errorf("%s: %d upstream tests failed during extraction (%d/%d passed)",
				platform, run.Failures, run.Passes, run.Tests)
		}
	}
}

// Both platform modes must be present. picomatch picks slash semantics from the
// host OS, so a single-platform fixture set would bake in whichever machine ran
// the extraction.
func TestBothPlatformsExtracted(t *testing.T) {
	stats := testcase.Summarise(loadCases(t))

	for _, platform := range []string{testcase.PlatformPosix, testcase.PlatformWindows} {
		if stats.ByPlatform[platform] == 0 {
			t.Errorf("no cases recorded for %s", platform)
		}
	}
}

// Every case must decode cleanly. A fixture the Go side cannot read is worse than
// no fixture, because it would be quietly skipped at replay time.
func TestEveryCaseDecodes(t *testing.T) {
	cases := loadCases(t)

	for i := range cases {
		c := &cases[i]

		if c.Module == "" || c.API == "" {
			t.Fatalf("case %d has no module/api", c.ID)
		}
		if c.Occurrences < 1 {
			t.Errorf("%s: occurrences = %d", c.Name(), c.Occurrences)
		}
		if _, err := c.DecodedArgs(); err != nil {
			t.Fatalf("%s: args: %v", c.Name(), err)
		}
		if _, err := c.DecodedConstruct(); err != nil {
			t.Fatalf("%s: construct: %v", c.Name(), err)
		}
		if _, err := c.DecodedResult(); err != nil {
			t.Fatalf("%s: result: %v", c.Name(), err)
		}
	}
}

// A matcher case is only replayable if it carries the arguments its matcher was
// built from.
func TestMatcherCasesCarryConstruction(t *testing.T) {
	cases := loadCases(t)
	seen := 0

	for i := range cases {
		c := &cases[i]
		if c.API != "matcher" {
			continue
		}
		seen++
		if len(c.Construct) == 0 {
			t.Fatalf("%s: matcher case has no construction arguments", c.Name())
		}
	}

	if seen == 0 {
		t.Error("no matcher cases extracted")
	}
}

// A case that threw must not also claim a result, and vice versa.
func TestErrorAndResultAreExclusive(t *testing.T) {
	cases := loadCases(t)
	threw := 0

	for i := range cases {
		c := &cases[i]
		if c.Error == nil {
			continue
		}
		threw++
		if c.Error.Message == "" {
			t.Errorf("%s: recorded error has no message", c.Name())
		}
		result, err := c.DecodedResult()
		if err != nil {
			t.Fatalf("%s: %v", c.Name(), err)
		}
		if result != nil {
			t.Errorf("%s: case both threw and returned %#v", c.Name(), result)
		}
	}

	// The suite has assert.throws cases; extracting none would mean the
	// recorder swallowed them.
	if threw == 0 {
		t.Error("no thrown-error cases extracted")
	}
}
