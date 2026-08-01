package testcase

// Typed accessors over decoded fixture values.
//
// Every accessor returns (value, ok) rather than panicking or zero-valuing: a
// fixture whose shape we did not anticipate must surface as a skipped, counted
// case, never as a silently passing one.

// AsString returns v as a string.
func AsString(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

// AsBool returns v as a bool.
func AsBool(v any) (bool, bool) {
	b, ok := v.(bool)
	return b, ok
}

// AsNumber returns v as a float64. Recorded JSON numbers are always float64.
func AsNumber(v any) (float64, bool) {
	f, ok := v.(float64)
	return f, ok
}

// AsObject returns v as a JSON object.
func AsObject(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

// AsStrings returns v as a string slice.
//
// picomatch accepts a pattern argument as either a single string or an array of
// them, and the `ignore` option does the same, so both spellings normalise here.
func AsStrings(v any) ([]string, bool) {
	if s, ok := v.(string); ok {
		return []string{s}, true
	}

	items, ok := v.([]any)
	if !ok {
		return nil, false
	}

	out := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := item.(string)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

// IsUndefined reports whether v is JavaScript undefined.
func IsUndefined(v any) bool {
	_, ok := v.(Undefined)
	return ok
}

// IsAbsent reports whether v is undefined or null, the two ways an argument can
// be "not supplied" in the fixtures.
func IsAbsent(v any) bool {
	return v == nil || IsUndefined(v)
}

// Arg returns the i'th decoded argument, or nil if the call had fewer.
func Arg(args []any, i int) any {
	if i < 0 || i >= len(args) {
		return nil
	}
	return args[i]
}

// OptionsArg returns the i'th argument as an options object. A missing, null or
// undefined argument yields an empty map, matching picomatch's own `options || {}`.
func OptionsArg(args []any, i int) (map[string]any, bool) {
	v := Arg(args, i)
	if IsAbsent(v) {
		return map[string]any{}, true
	}
	return AsObject(v)
}

// HasCallback reports whether an options object carries a function value. Such
// options cannot be replayed from a fixture.
func HasCallback(opts map[string]any) bool {
	for _, v := range opts {
		if _, ok := v.(Func); ok {
			return true
		}
	}
	return false
}
