// Package crypto contains the small set of cryptographic primitives Docveta uses:
// random tokens, token hashing, Argon2id password hashing, AES-GCM encryption of
// stored secrets and HMAC-signed URLs.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/hkdf"
)

var b64 = base64.RawURLEncoding

// NewToken returns a high-entropy token "<prefix>_<43 chars>" and its first
// characters for display (e.g. "dvt_pat_AbC1").
func NewToken(prefix string) (token, display string) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	token = prefix + "_" + b64.EncodeToString(b)
	return token, token[:len(prefix)+5]
}

// HashToken returns the SHA-256 of a token. Tokens are high entropy, so a fast hash is
// appropriate (unlike passwords).
func HashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// ---------------------------------------------------------------------------
// Passwords (Argon2id, PHC string format)
// ---------------------------------------------------------------------------

// Parameters follow OWASP's Argon2id recommendation (m=19 MiB, t=2, p=1), which
// stays fast enough on ARM SBCs.
const (
	argonTime    = 2
	argonMemory  = 19 * 1024
	argonThreads = 1
	argonKeyLen  = 32
)

func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

var ErrInvalidHash = errors.New("invalid password hash")

// VerifyPassword checks password against a PHC-formatted Argon2id hash in constant time.
func VerifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, ErrInvalidHash
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false, ErrInvalidHash
	}
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return false, ErrInvalidHash
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil {
		return false, ErrInvalidHash
	}
	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// DummyVerify burns the same CPU as a real verification so that login timing does not
// reveal whether an account exists.
func DummyVerify(password string) {
	argon2.IDKey([]byte(password), []byte("docveta-dummy-salt"), argonTime, argonMemory, argonThreads, argonKeyLen)
}

// ---------------------------------------------------------------------------
// Keys derived from DOCVETA_SECRET_KEY
// ---------------------------------------------------------------------------

type Keys struct {
	enc  cipher.AEAD
	sign []byte
}

func NewKeys(secret []byte) (*Keys, error) {
	encKey := derive(secret, "docveta/v1/encryption", 32)
	block, err := aes.NewCipher(encKey)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Keys{enc: aead, sign: derive(secret, "docveta/v1/signing", 32)}, nil
}

func derive(secret []byte, info string, n int) []byte {
	out := make([]byte, n)
	r := hkdf.New(sha256.New, secret, nil, []byte(info))
	if _, err := r.Read(out); err != nil {
		panic(err)
	}
	return out
}

// Encrypt seals plaintext with AES-256-GCM; output is nonce||ciphertext.
func (k *Keys) Encrypt(plaintext []byte) []byte {
	nonce := make([]byte, k.enc.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic(err)
	}
	return k.enc.Seal(nonce, nonce, plaintext, nil)
}

var ErrDecrypt = errors.New("cannot decrypt secret (was DOCVETA_SECRET_KEY changed?)")

func (k *Keys) Decrypt(sealed []byte) ([]byte, error) {
	ns := k.enc.NonceSize()
	if len(sealed) < ns {
		return nil, ErrDecrypt
	}
	out, err := k.enc.Open(nil, sealed[:ns], sealed[ns:], nil)
	if err != nil {
		return nil, ErrDecrypt
	}
	return out, nil
}

// Sign returns an HMAC-SHA256 signature over parts, base64url encoded.
func (k *Keys) Sign(parts ...string) string {
	m := hmac.New(sha256.New, k.sign)
	for _, p := range parts {
		var l [4]byte
		binary.BigEndian.PutUint32(l[:], uint32(len(p)))
		m.Write(l[:])
		m.Write([]byte(p))
	}
	return b64.EncodeToString(m.Sum(nil))
}

// SignedValue produces "<expUnix>.<sig>" binding a purpose and a value until exp.
func (k *Keys) SignedValue(purpose, value string, exp time.Time) string {
	e := strconv.FormatInt(exp.Unix(), 10)
	return e + "." + k.Sign(purpose, value, e)
}

// VerifySignedValue validates a token produced by SignedValue.
func (k *Keys) VerifySignedValue(purpose, value, token string, now time.Time) bool {
	e, sig, ok := strings.Cut(token, ".")
	if !ok {
		return false
	}
	exp, err := strconv.ParseInt(e, 10, 64)
	if err != nil || now.Unix() > exp {
		return false
	}
	return hmac.Equal([]byte(sig), []byte(k.Sign(purpose, value, e)))
}
