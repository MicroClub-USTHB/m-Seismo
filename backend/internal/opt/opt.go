// Package opt provides a JSON field type that tells apart a field that is
// absent, explicitly null, or set, as PATCH requests need.
package opt

import (
	"bytes"
	"encoding/json"
)

type Field[T any] struct {
	Set   bool // the key was present in the JSON object
	Null  bool // the key was present with a null value
	Value T
}

func (f *Field[T]) UnmarshalJSON(data []byte) error {
	f.Set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		f.Null = true
		return nil
	}
	return json.Unmarshal(data, &f.Value)
}

// Of returns a set, non-null field.
func Of[T any](v T) Field[T] {
	return Field[T]{Set: true, Value: v}
}

// PutIfSet records the field in m under key when it was present, as nil when
// it was null. Services use it to build audit payloads of what changed.
func (f Field[T]) PutIfSet(m map[string]any, key string) {
	if f.Set {
		m[key] = f.Ptr()
	}
}

// Ptr returns nil when the field is null, otherwise a pointer to the value.
func (f Field[T]) Ptr() *T {
	if f.Null {
		return nil
	}
	return &f.Value
}
