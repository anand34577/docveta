package identity

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/oauth2"

	"github.com/anand34577/docveta/internal/platform/crypto"
)

// fakeIssuer serves an OpenID configuration whose issuer is base+suffix.
func fakeIssuer(t *testing.T, suffix string) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/.well-known/openid-configuration") {
			http.NotFound(w, r)
			return
		}
		iss := srv.URL + "/application/o/docveta" + suffix
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": iss, "authorization_endpoint": iss + "/authorize", "token_endpoint": iss + "/token",
			"jwks_uri": iss + "/jwks", "id_token_signing_alg_values_supported": []string{"RS256"},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDiscoverTrailingSlash(t *testing.T) {
	for _, published := range []string{"/", ""} {
		srv := fakeIssuer(t, published)
		want := srv.URL + "/application/o/docveta" + published
		for _, typed := range []string{srv.URL + "/application/o/docveta/", srv.URL + "/application/o/docveta"} {
			_, got, err := discover(typed)
			if err != nil {
				t.Fatalf("published %q, typed %q: %v", published, typed, err)
			}
			if got != want {
				t.Errorf("published %q, typed %q: issuer %q, want %q", published, typed, got, want)
			}
		}
	}
}

func TestAppCode(t *testing.T) {
	keys, err := crypto.NewKeys([]byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{keys: keys}
	verifier := oauth2.GenerateVerifier()
	code := s.AppCode("dvs_session", oauth2.S256ChallengeFromVerifier(verifier))
	if tok, err := s.RedeemAppCode(code, verifier); err != nil || tok != "dvs_session" {
		t.Fatalf("redeem: %q, %v", tok, err)
	}
	if _, err := s.RedeemAppCode(code, oauth2.GenerateVerifier()); err == nil {
		t.Fatal("a different verifier redeemed the code")
	}
	if _, err := s.RedeemAppCode(code[:len(code)-2]+"AA", verifier); err == nil {
		t.Fatal("a tampered code was accepted")
	}
}

func TestClaimVerified(t *testing.T) {
	for v, want := range map[any]bool{true: true, false: false, "true": true, "TRUE": true, "false": false, 1: false} {
		if got := claimVerified(map[string]any{"email_verified": v}); got != want {
			t.Errorf("%v: got %v", v, got)
		}
	}
}
