package testcase

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func decodeString(t *testing.T, s string) any {
	t.Helper()
	v, err := decode(json.RawMessage(s))
	if err != nil {
		t.Fatalf("decode(%s): %v", s, err)
	}
	return v
}

func TestDecodePlainJSON(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want any
	}{
		{"string", `"a/*.js"`, "a/*.js"},
		{"bool", `true`, true},
		{"number", `42`, float64(42)},
		{"null", `null`, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := decodeString(t, tt.in); got != tt.want {
				t.Errorf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

// undefined and null must stay distinguishable: picomatch's entry point branches
// on `options.windows === null || undefined`, so collapsing them would erase a
// real behavioural fork.
func TestDecodeUndefinedIsNotNull(t *testing.T) {
	undef := decodeString(t, `{"$undefined":true}`)
	if !IsUndefined(undef) {
		t.Fatalf("expected Undefined, got %#v", undef)
	}
	if got := decodeString(t, `null`); got != nil {
		t.Fatalf("expected nil for null, got %#v", got)
	}
	if IsUndefined(nil) {
		t.Error("nil must not report as undefined")
	}
	if !IsAbsent(undef) || !IsAbsent(nil) {
		t.Error("both undefined and null must count as absent")
	}
}

func TestDecodeTaggedValues(t *testing.T) {
	t.Run("regexp", func(t *testing.T) {
		got := decodeString(t, `{"$regexp":{"source":"^(?:a)$","flags":"i"}}`)
		re, ok := got.(Regexp)
		if !ok {
			t.Fatalf("got %T, want Regexp", got)
		}
		if re.Source != "^(?:a)$" || re.Flags != "i" {
			t.Errorf("got %+v", re)
		}
	})

	t.Run("function", func(t *testing.T) {
		got := decodeString(t, `{"$function":{"name":"format","source":"str => str"}}`)
		fn, ok := got.(Func)
		if !ok {
			t.Fatalf("got %T, want Func", got)
		}
		if fn.Name != "format" {
			t.Errorf("got name %q", fn.Name)
		}
	})

	t.Run("matcher", func(t *testing.T) {
		if _, ok := decodeString(t, `{"$matcher":true}`).(Matcher); !ok {
			t.Error("expected Matcher")
		}
	})

	t.Run("elided", func(t *testing.T) {
		circular, ok := decodeString(t, `{"$circular":true}`).(Elided)
		if !ok || !circular.Circular {
			t.Errorf("got %#v, want circular Elided", circular)
		}
		deep, ok := decodeString(t, `{"$truncated":true}`).(Elided)
		if !ok || deep.Circular {
			t.Errorf("got %#v, want depth-limited Elided", deep)
		}
	})

	t.Run("match array", func(t *testing.T) {
		got := decodeString(t, `{"$match":{"groups":["ab","b"],"index":3,"input":"xxab"}}`)
		m, ok := got.(Match)
		if !ok {
			t.Fatalf("got %T, want Match", got)
		}
		if m.Index != 3 || m.Input != "xxab" || len(m.Groups) != 2 {
			t.Errorf("got %+v", m)
		}
	})

	t.Run("non-finite numbers", func(t *testing.T) {
		if f := decodeString(t, `{"$number":"Infinity"}`).(float64); !math.IsInf(f, 1) {
			t.Errorf("got %v, want +Inf", f)
		}
		if f := decodeString(t, `{"$number":"NaN"}`).(float64); !math.IsNaN(f) {
			t.Errorf("got %v, want NaN", f)
		}
	})
}

func TestDecodeNestedObject(t *testing.T) {
	got := decodeString(t, `{"isMatch":true,"output":"a.js","opts":{"dot":{"$undefined":true}}}`)
	obj, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("got %T, want map", got)
	}
	if obj["isMatch"] != true || obj["output"] != "a.js" {
		t.Errorf("got %#v", obj)
	}

	nested, ok := obj["opts"].(map[string]any)
	if !ok {
		t.Fatalf("nested opts is %T", obj["opts"])
	}
	if !IsUndefined(nested["dot"]) {
		t.Errorf("nested undefined not preserved: %#v", nested["dot"])
	}
}

func TestAsStringsAcceptsBothSpellings(t *testing.T) {
	// picomatch takes a pattern as a string or an array of strings; both must
	// normalise identically.
	single, ok := AsStrings("*.js")
	if !ok || len(single) != 1 || single[0] != "*.js" {
		t.Errorf("string form: %v %v", single, ok)
	}

	many, ok := AsStrings([]any{"*.js", "*.md"})
	if !ok || len(many) != 2 || many[1] != "*.md" {
		t.Errorf("array form: %v %v", many, ok)
	}

	if _, ok := AsStrings([]any{"*.js", 3.0}); ok {
		t.Error("a mixed array must be rejected, not silently truncated")
	}
	if _, ok := AsStrings(42.0); ok {
		t.Error("a number must be rejected")
	}
}

func TestOptionsArgTreatsAbsentAsEmpty(t *testing.T) {
	for name, args := range map[string][]any{
		"missing":   {"a"},
		"null":      {"a", nil},
		"undefined": {"a", Undefined{}},
	} {
		t.Run(name, func(t *testing.T) {
			opts, ok := OptionsArg(args, 1)
			if !ok {
				t.Fatal("expected ok")
			}
			if len(opts) != 0 {
				t.Errorf("expected empty options, got %#v", opts)
			}
		})
	}

	if _, ok := OptionsArg([]any{"a", "not-an-object"}, 1); ok {
		t.Error("a non-object options argument must be rejected")
	}
}

func TestReplayableExcludesUntrustworthyCases(t *testing.T) {
	tests := []struct {
		name string
		c    Case
		want bool
	}{
		{"clean", Case{Portable: true, TestOutcome: OutcomePassed}, true},
		{"callback", Case{Portable: false, TestOutcome: OutcomePassed}, false},
		{"truncated", Case{Portable: true, Truncated: true, TestOutcome: OutcomePassed}, false},
		{"from failing test", Case{Portable: true, TestOutcome: OutcomeFailed}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.c.Replayable(); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReadSkipsBlankLines(t *testing.T) {
	input := `{"id":1,"platform":"posix","module":"index","api":"matcher"}
` + "\n" + `{"id":2,"platform":"windows","module":"lib/scan","api":"scan"}
`
	cases, err := Read(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(cases) != 2 {
		t.Fatalf("got %d cases, want 2", len(cases))
	}
	if !cases[1].Windows() || cases[0].Windows() {
		t.Error("platform not decoded correctly")
	}
	if got := cases[0].Name(); got != "1/posix/index.matcher" {
		t.Errorf("Name() = %q", got)
	}
}

func TestReadReportsLineNumberOnBadJSON(t *testing.T) {
	_, err := Read(strings.NewReader("{}\n{not json}\n"))
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("error should name the offending line, got: %v", err)
	}
}
