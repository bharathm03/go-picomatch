// Package tokencase loads the golden token streams recorded from upstream
// picomatch's parser.
//
// Each case is one pattern and the token stream upstream's full scanner produced
// for it. The Go token gate replays them; nothing here depends on the port, so
// the fixtures can be inspected and counted before a scanner exists.
//
// Fixtures are produced by tools/tokens/generate.js. The recorded shape is kept
// deliberately separate from [github.com/bharathm03/go-picomatch/internal/parse]'s
// own types rather than shared: if the port's Token drifts from what upstream
// records, the conversion is where that becomes visible. A shared struct would
// make a drift compile cleanly and disappear.
package tokencase

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// Fastpath values: which of picomatch's three parsers makeRe would really have
// used for this pattern, under default options.
const (
	// FastpathNone means the full scanner ran, so the recorded tokens are
	// exactly what makeRe compiled.
	FastpathNone = "none"
	// FastpathTop means parse.fastpaths() returned output and parse() never ran
	// (lib/picomatch.js:311-317).
	FastpathTop = "top"
	// FastpathInline means parse() returned from the fast path at
	// lib/parse.js:606, before the scanner loop.
	FastpathInline = "inline"
)

// Token is one recorded token.
//
// Every optional field is a pointer so that "absent" and "the zero value" stay
// distinguishable — upstream records both, and conflating them would let a
// parser that never populates a field score a clean pass. See internal/parse.
type Token struct {
	Type   string  `json:"type"`
	Value  string  `json:"value"`
	Output *string `json:"output"`

	Extglob bool `json:"extglob"`
	Posix   bool `json:"posix"`
	Comma   bool `json:"comma"`
	Star    bool `json:"star"`

	OutputIndex *int `json:"outputIndex"`
	TokensIndex *int `json:"tokensIndex"`
}

// Case is one pattern and the token stream upstream produced for it.
type Case struct {
	Pattern   string  `json:"pattern"`
	Consumed  string  `json:"consumed"`
	Output    string  `json:"output"`
	Negated   bool    `json:"negated"`
	Backtrack bool    `json:"backtrack"`
	Tokens    []Token `json:"tokens"`

	// Fastpath is which parser makeRe would really have used, and
	// FastpathDiverges whether that parser produced different regex source than
	// the scanner would have.
	//
	// Neither weakens the token assertion: a full-scanner parser is right to
	// produce these tokens either way. What they bound is what a green score
	// BUYS. Where a fastpath ran and diverged, matching these tokens no longer
	// settles what makeRe compiled, and the pattern is not pinned until the
	// fastpath pass exists. The gate stratifies on this rather than filtering by
	// it, so the unpinned patterns stay counted and stay visible.
	Fastpath         string `json:"fastpath"`
	FastpathDiverges bool   `json:"fastpathDiverges"`
}

// FastpathIndependent reports whether matching this case's tokens is sufficient
// to pin what upstream's makeRe compiled for the pattern.
func (c *Case) FastpathIndependent() bool { return !c.FastpathDiverges }

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
	// Token streams for the larger brace and extglob patterns run to several
	// kilobytes; the default 64 KiB limit would truncate them into parse errors.
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
	Tokens     int
	Backtrack  int
	ByFastpath map[string]int
	Diverges   int
}

// Summarise counts a fixture set by category.
func Summarise(cases []Case) Stats {
	s := Stats{ByFastpath: map[string]int{}}
	for i := range cases {
		c := &cases[i]
		s.Total++
		s.Tokens += len(c.Tokens)
		if c.Backtrack {
			s.Backtrack++
		}
		s.ByFastpath[c.Fastpath]++
		if c.FastpathDiverges {
			s.Diverges++
		}
	}
	return s
}
