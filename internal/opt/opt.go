// Package opt provides a JSON field type that distinguishes "absent", "null" and a
// value — needed for PATCH endpoints where null means "clear this field".
package opt

import (
	"bytes"
	"encoding/json"
)

type Field[T any] struct {
	Set   bool // present in the JSON body
	Null  bool // present and null
	Value T
}

func (f *Field[T]) UnmarshalJSON(b []byte) error {
	f.Set = true
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		f.Null = true
		return nil
	}
	return json.Unmarshal(b, &f.Value)
}

// Ptr returns nil when absent or null, otherwise a pointer to the value.
func (f Field[T]) Ptr() *T {
	if !f.Set || f.Null {
		return nil
	}
	v := f.Value
	return &v
}

func Of[T any](v T) Field[T] { return Field[T]{Set: true, Value: v} }
