package search

import (
	"encoding/base64"
	"testing"

	"github.com/google/uuid"
)

func TestDecodeCursor(t *testing.T) {
	k := "2026-10-02T12:00:00.000000"
	good := encodeCursor(cursor{Key: &k, ID: uuid.NewString()})
	if _, err := decodeCursor(*good); err != nil {
		t.Errorf("valid keyset cursor refused: %v", err)
	}
	if c, err := decodeCursor(*encodeCursor(cursor{Offset: 50})); err != nil || c.Offset != 50 {
		t.Errorf("offset cursor: %+v %v", c, err)
	}
	// Tampered cursors are a bad request, not a database error.
	for name, raw := range map[string]string{
		"not base64":      "%%%",
		"not json":        base64.RawURLEncoding.EncodeToString([]byte("nope")),
		"id not a uuid":   base64.RawURLEncoding.EncodeToString([]byte(`{"k":"x","i":"1'; --"}`)),
		"negative offset": base64.RawURLEncoding.EncodeToString([]byte(`{"o":-5}`)),
	} {
		if _, err := decodeCursor(raw); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
