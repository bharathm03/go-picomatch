package testcase

import (
	"encoding/json"
	"fmt"
	"math"
)

// JavaScript values that JSON cannot express are recorded by the extractor as
// single-key tagged objects. These types are their Go counterparts.
//
// The tagging is what lets the decoder stay unambiguous: a recorded string is
// always a Go string, never a RegExp that happened to serialise as one.

// Undefined is JavaScript's undefined, which is distinct from null. picomatch
// treats a missing option and an explicitly nil one differently in places, so the
// distinction has to survive extraction.
type Undefined struct{}

// Regexp is a recorded JavaScript RegExp. Source is ECMAScript syntax and will
// not always be a valid Go regexp; comparing it is a diagnostic aid, not a
// conformance requirement.
type Regexp struct {
	Source string
	Flags  string
}

// Func is a recorded JavaScript function passed as an option value (onMatch,
// format, expandRange). It can never be replayed, so any case containing one is
// reported unportable by the extractor.
type Func struct {
	Name   string
	Source string
}

// Matcher marks the return value of a picomatch factory call. The matcher's own
// invocations are recorded as separate cases.
type Matcher struct{}

// Elided marks a value the extractor cut short, either because it was cyclic or
// because it exceeded the depth limit.
type Elided struct{ Circular bool }

// Match is a JavaScript RegExp match array, which carries an index and the input
// alongside its capture groups.
type Match struct {
	Groups []any
	Index  int
	Input  string
}

// JSError is a recorded thrown exception.
type JSError struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

func (e *JSError) Error() string { return e.Name + ": " + e.Message }

// decode converts one extractor-encoded JSON value into a Go value.
//
// Plain JSON maps to the obvious Go types (string, float64, bool, nil,
// []any, map[string]any); tagged objects map to the types above.
func decode(raw json.RawMessage) (any, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return convert(v)
}

func convert(v any) (any, error) {
	switch t := v.(type) {
	case map[string]any:
		return convertObject(t)
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			c, err := convert(item)
			if err != nil {
				return nil, err
			}
			out[i] = c
		}
		return out, nil
	default:
		return v, nil
	}
}

// convertObject resolves a tagged object, or recurses into a plain one.
func convertObject(obj map[string]any) (any, error) {
	// Tags are single-key by construction, so checking membership is enough.
	if _, ok := obj["$undefined"]; ok {
		return Undefined{}, nil
	}
	if _, ok := obj["$matcher"]; ok {
		return Matcher{}, nil
	}
	if _, ok := obj["$circular"]; ok {
		return Elided{Circular: true}, nil
	}
	if _, ok := obj["$truncated"]; ok {
		return Elided{}, nil
	}

	// The remaining tags carry a payload. A tag that is present but malformed is
	// an error rather than a fall-through: silently decoding {"$regexp":"abc"}
	// into a plain map would hand the conformance harness an expectation of
	// entirely the wrong type while every decode test still passed.
	if v, ok := obj["$regexp"]; ok {
		r, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("$regexp payload is %T, want object", v)
		}
		return Regexp{Source: str(r["source"]), Flags: str(r["flags"])}, nil
	}
	if v, ok := obj["$function"]; ok {
		f, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("$function payload is %T, want object", v)
		}
		return Func{Name: str(f["name"]), Source: str(f["source"])}, nil
	}
	if v, ok := obj["$error"]; ok {
		e, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("$error payload is %T, want object", v)
		}
		return &JSError{Name: str(e["name"]), Message: str(e["message"])}, nil
	}
	if v, ok := obj["$number"]; ok {
		n, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("$number payload is %T, want string", v)
		}
		return specialNumber(n)
	}
	for _, tag := range []string{"$bigint", "$symbol", "$date"} {
		v, ok := obj[tag]
		if !ok {
			continue
		}
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("%s payload is %T, want string", tag, v)
		}
		return s, nil
	}

	if v, ok := obj["$match"]; ok {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("$match payload is %T, want object", v)
		}
		groups, err := convert(m["groups"])
		if err != nil {
			return nil, err
		}
		g, _ := groups.([]any)
		idx, _ := m["index"].(float64)
		return Match{Groups: g, Index: int(idx), Input: str(m["input"])}, nil
	}

	for _, tag := range []string{"$set", "$map"} {
		v, ok := obj[tag]
		if !ok {
			continue
		}
		items, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("%s payload is %T, want array", tag, v)
		}
		return convert(items)
	}

	out := make(map[string]any, len(obj))
	for k, val := range obj {
		c, err := convert(val)
		if err != nil {
			return nil, fmt.Errorf("key %q: %w", k, err)
		}
		out[k] = c
	}
	return out, nil
}

// specialNumber decodes the non-finite floats JSON cannot carry.
func specialNumber(s string) (any, error) {
	switch s {
	case "NaN":
		return math.NaN(), nil
	case "Infinity":
		return math.Inf(1), nil
	case "-Infinity":
		return math.Inf(-1), nil
	default:
		return nil, fmt.Errorf("unrecognised $number %q", s)
	}
}

func str(v any) string {
	s, _ := v.(string)
	return s
}
