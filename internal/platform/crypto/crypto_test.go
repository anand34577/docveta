package crypto

import (
	"strings"
	"testing"
	"time"
)

func TestPasswordRoundTrip(t *testing.T) {
	h, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$") {
		t.Fatalf("unexpected format %s", h)
	}
	if ok, _ := VerifyPassword("correct horse battery staple", h); !ok {
		t.Fatal("valid password rejected")
	}
	if ok, _ := VerifyPassword("wrong", h); ok {
		t.Fatal("invalid password accepted")
	}
	if _, err := VerifyPassword("x", "$bcrypt$..."); err == nil {
		t.Fatal("foreign hash should error")
	}
}

func TestEncryptDecrypt(t *testing.T) {
	k, _ := NewKeys([]byte(strings.Repeat("k", 32)))
	sealed := k.Encrypt([]byte("smtp-password"))
	got, err := k.Decrypt(sealed)
	if err != nil || string(got) != "smtp-password" {
		t.Fatalf("got %q %v", got, err)
	}
	other, _ := NewKeys([]byte(strings.Repeat("x", 32)))
	if _, err := other.Decrypt(sealed); err == nil {
		t.Fatal("decrypt with another key must fail")
	}
}

func TestSignedValue(t *testing.T) {
	k, _ := NewKeys([]byte(strings.Repeat("k", 32)))
	now := time.Now()
	tok := k.SignedValue("download", "doc-1", now.Add(time.Minute))
	if !k.VerifySignedValue("download", "doc-1", tok, now) {
		t.Fatal("valid signature rejected")
	}
	if k.VerifySignedValue("download", "doc-2", tok, now) || k.VerifySignedValue("other", "doc-1", tok, now) {
		t.Fatal("signature not bound to purpose/value")
	}
	if k.VerifySignedValue("download", "doc-1", tok, now.Add(2*time.Minute)) {
		t.Fatal("expired signature accepted")
	}
}

func TestTokens(t *testing.T) {
	a, disp := NewToken("dvt_pat")
	b, _ := NewToken("dvt_pat")
	if a == b || !strings.HasPrefix(a, "dvt_pat_") || !strings.HasPrefix(a, disp) || len(a) < 40 {
		t.Fatalf("bad tokens %s %s %s", a, b, disp)
	}
	if string(HashToken(a)) == string(HashToken(b)) {
		t.Fatal("hash collision")
	}
}
