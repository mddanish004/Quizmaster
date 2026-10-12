package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeIssuer is a minimal OIDC provider for verifier regression tests.
type fakeIssuer struct {
	t         *testing.T
	srv       *httptest.Server
	key       *rsa.PrivateKey
	kid       string
	issuer    string
	mintFail  bool
	extraKeys map[string]*rsa.PrivateKey
}

func newFakeIssuer(t *testing.T) *fakeIssuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	fi := &fakeIssuer{t: t, key: key, kid: "k1", extraKeys: map[string]*rsa.PrivateKey{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 fi.issuer,
			"authorization_endpoint": fi.issuer + "/auth",
			"token_endpoint":         fi.issuer + "/token",
			"jwks_uri":               fi.issuer + "/jwks",
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		keys := []any{jwk(fi.kid, &fi.key.PublicKey)}
		for kid, k := range fi.extraKeys {
			keys = append(keys, jwk(kid, &k.PublicKey))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": keys})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if fi.mintFail {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		// Test hook: a code of the form "nonce:<value>" mints that nonce claim,
		// letting service-level tests bind the token to their transaction.
		// Anything else mints the default fixture nonce.
		_ = r.ParseForm()
		nonce := "test-nonce"
		if code := r.Form.Get("code"); strings.HasPrefix(code, "nonce:") {
			nonce = strings.TrimPrefix(code, "nonce:")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "x", "token_type": "Bearer",
			"id_token": fi.mint(fi.kid, fi.key, &claimOverrides{nonce: nonce}),
		})
	})
	fi.srv = httptest.NewServer(mux)
	fi.issuer = fi.srv.URL
	return fi
}

func jwk(kid string, pub *rsa.PublicKey) map[string]any {
	return map[string]any{
		"kty": "RSA", "kid": kid, "use": "sig", "alg": "RS256",
		"n": base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}
}

type claimOverrides struct {
	alg     string
	key     *rsa.PrivateKey
	kid     string
	issuer  string
	aud     string
	nonce   string
	sub     string
	email   string
	expired bool
	noNonce bool
	azp     string
}

// mint signs an ID token; nil overrides mean defaults.
func (fi *fakeIssuer) mint(kid string, key *rsa.PrivateKey, o *claimOverrides) string {
	alg, issuer, aud, nonce, sub, email, azp := "RS256", fi.issuer, "test-client", "test-nonce", "sub-123", "ada@example.com", ""
	exp := time.Now().Add(time.Hour)
	if o != nil {
		if o.alg != "" {
			alg = o.alg
		}
		if o.issuer != "" {
			issuer = o.issuer
		}
		if o.aud != "" {
			aud = o.aud
		}
		if o.nonce != "" || o.noNonce {
			nonce = o.nonce
		}
		if o.sub != "" {
			sub = o.sub
		}
		if o.email != "" {
			email = o.email
		}
		if o.expired {
			exp = time.Now().Add(-time.Hour)
		}
		if o.azp != "" {
			azp = o.azp
		}
	}
	header, _ := json.Marshal(map[string]string{"alg": alg, "kid": kid, "typ": "JWT"})
	claims := map[string]any{
		"iss": issuer, "aud": aud, "sub": sub, "email": email,
		"email_verified": true, "name": "Ada", "exp": exp.Unix(), "iat": time.Now().Add(-time.Minute).Unix(),
	}
	if nonce != "" {
		claims["nonce"] = nonce
	}
	if azp != "" {
		claims["azp"] = azp
	}
	payload, _ := json.Marshal(claims)
	signing := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	if alg == "none" {
		return signing + "."
	}
	h := hashSHA256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, cryptoHashSHA256, h)
	if err != nil {
		fi.t.Fatal(err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func verifierFor(fi *fakeIssuer) *RealOIDCVerifier {
	return &RealOIDCVerifier{
		Issuer: fi.issuer, ClientID: "test-client", ClientSecret: "secret",
		RedirectURL: "http://localhost/cb", HTTP: fi.srv.Client(),
	}
}

func TestOIDCValidFlow(t *testing.T) {
	fi := newFakeIssuer(t)
	defer fi.srv.Close()
	v := verifierFor(fi)
	claims, err := v.VerifyCode(context.Background(), "any-code", "verifier", "test-nonce")
	if err != nil {
		t.Fatalf("valid: %v", err)
	}
	if claims.Subject != "sub-123" || claims.Email != "ada@example.com" || !claims.EmailVerified {
		t.Fatalf("claims: %+v", claims)
	}
}

func TestOIDCRejections(t *testing.T) {
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(fi *fakeIssuer) (code, nonce string, mutateToken func() string){
		"bad signature": func(fi *fakeIssuer) (string, string, func() string) {
			return "c", "test-nonce", func() string { return fi.mint("k1", other, nil) }
		},
		"wrong audience": func(fi *fakeIssuer) (string, string, func() string) {
			return "c", "test-nonce", func() string { return fi.mint("k1", fi.key, &claimOverrides{aud: "other-client"}) }
		},
		"wrong issuer": func(fi *fakeIssuer) (string, string, func() string) {
			return "c", "test-nonce", func() string { return fi.mint("k1", fi.key, &claimOverrides{issuer: "https://evil.example"}) }
		},
		"expired": func(fi *fakeIssuer) (string, string, func() string) {
			return "c", "test-nonce", func() string { return fi.mint("k1", fi.key, &claimOverrides{expired: true}) }
		},
		"wrong nonce": func(fi *fakeIssuer) (string, string, func() string) {
			return "c", "attacker-nonce", func() string { return fi.mint("k1", fi.key, nil) }
		},
		"missing nonce": func(fi *fakeIssuer) (string, string, func() string) {
			return "c", "test-nonce", func() string { return fi.mint("k1", fi.key, &claimOverrides{noNonce: true}) }
		},
		"unknown key": func(fi *fakeIssuer) (string, string, func() string) {
			return "c", "test-nonce", func() string { return fi.mint("ghost", other, nil) }
		},
		"none alg": func(fi *fakeIssuer) (string, string, func() string) {
			return "c", "test-nonce", func() string { return fi.mint("", nil, &claimOverrides{alg: "none"}) }
		},
		"azp mismatch": func(fi *fakeIssuer) (string, string, func() string) {
			return "c", "test-nonce", func() string { return fi.mint("k1", fi.key, &claimOverrides{azp: "other-client"}) }
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			fi := newFakeIssuer(t)
			defer fi.srv.Close()
			_, nonce, mint := setup(fi)
			// Serve the minted token from the token endpoint.
			old := fi.mint
			_ = old
			fi.srv.Config.Handler = rotatingHandler(fi, mint)
			v := verifierFor(fi)
			if _, err := v.VerifyCode(context.Background(), "c", "verifier", nonce); err == nil {
				t.Fatalf("%s: accepted", name)
			}
		})
	}
}

// rotatingHandler serves a fixed id_token regardless of code (test helper).
func rotatingHandler(fi *fakeIssuer, mint func() string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": fi.issuer, "token_endpoint": fi.issuer + "/token", "jwks_uri": fi.issuer + "/jwks",
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		keys := []any{jwk(fi.kid, &fi.key.PublicKey)}
		for kid, k := range fi.extraKeys {
			keys = append(keys, jwk(kid, &k.PublicKey))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": keys})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "x", "token_type": "Bearer", "id_token": mint()})
	})
	return mux
}

func TestOIDCJWKSRotation(t *testing.T) {
	fi := newFakeIssuer(t)
	defer fi.srv.Close()
	v := verifierFor(fi)
	// Prime the cache with a valid verification.
	if _, err := v.VerifyCode(context.Background(), "c", "v", "test-nonce"); err != nil {
		t.Fatalf("prime: %v", err)
	}
	// Rotate: new key published, old removed; token signed by the new key.
	k2, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	delete(fi.extraKeys, "unused")
	fi.extraKeys["k2"] = k2
	fi.srv.Config.Handler = rotatingHandler(fi, func() string { return fi.mint("k2", k2, nil) })
	// Remove k1 from the set to prove refresh (keep only k2).
	fi.key = k2
	fi.kid = "k2"
	if _, err := v.VerifyCode(context.Background(), "c", "v", "test-nonce"); err != nil {
		t.Fatalf("rotation: %v", err)
	}
}

func TestOIDCExchangeFailureFailsClosed(t *testing.T) {
	fi := newFakeIssuer(t)
	defer fi.srv.Close()
	fi.mintFail = true
	v := verifierFor(fi)
	if _, err := v.VerifyCode(context.Background(), "bad", "v", "test-nonce"); err == nil {
		t.Fatal("exchange failure accepted")
	}
}

func TestOIDCUnconfiguredFailsClosed(t *testing.T) {
	v := &RealOIDCVerifier{Issuer: "https://accounts.google.com", RedirectURL: "x"}
	if _, err := v.VerifyCode(context.Background(), "c", "v", "n"); err == nil {
		t.Fatal("unconfigured client accepted")
	}
}

func TestStartURLCarriesNonceAndPKCE(t *testing.T) {
	svc := oidcTestService(t)
	url, err := svc.StartGoogleLogin(context.Background(), "/app")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"nonce=", "code_challenge=", "code_challenge_method=S256", "state="} {
		if !strings.Contains(url, want) {
			t.Fatalf("auth URL missing %s: %s", want, url)
		}
	}
}

// TestLinkFlowWithRealVerifier drives link-start → callback through the real
// (fake-issuer-backed) verifier, proving nonce/plumbing on the link path, and
// proves a second user cannot steal the same Google subject.
func TestLinkFlowWithRealVerifier(t *testing.T) {
	fi := newFakeIssuer(t)
	defer fi.srv.Close()
	pool := testutil_OpenPool(t)
	svc := oidcTestServiceWithVerifier(t, pool, &RealOIDCVerifier{
		Issuer: fi.issuer, ClientID: "test-client", ClientSecret: "s",
		RedirectURL: "http://localhost/cb", HTTP: fi.srv.Client(),
	})
	ctx := context.Background()
	// Password user adopts the Google subject.
	u, err := registerPasswordUser(t, svc, ctx, "linker@example.com")
	if err != nil {
		t.Fatal(err)
	}
	linkURL, err := svc.StartGoogleLink(ctx, u.ID, "/settings")
	if err != nil {
		t.Fatal(err)
	}
	state := queryParam(t, linkURL, "state")
	code := "nonce:" + txNonceForState(t, svc, ctx, state)
	if err := svc.LinkGoogleToUser(ctx, u.ID, state, code); err != nil {
		t.Fatalf("link: %v", err)
	}
	// Same subject now logs in directly.
	loginURL, err := svc.StartGoogleLogin(ctx, "/app")
	if err != nil {
		t.Fatal(err)
	}
	loginState := queryParam(t, loginURL, "state")
	got, err := svc.CallbackGoogle(ctx, loginState, "nonce:"+txNonceForState(t, svc, ctx, loginState))
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	if got.Auth.User.ID != u.ID {
		t.Fatalf("linked login landed on %s, want %s", got.Auth.User.ID, u.ID)
	}
	// Another user attempting to link the same subject is rejected.
	u2, err := registerPasswordUser(t, svc, ctx, "victim@example.com")
	if err != nil {
		t.Fatal(err)
	}
	linkURL2, _ := svc.StartGoogleLink(ctx, u2.ID, "/settings")
	state2 := queryParam(t, linkURL2, "state")
	if err := svc.LinkGoogleToUser(ctx, u2.ID, state2, "nonce:"+txNonceForState(t, svc, ctx, state2)); err != ErrLinkConflict {
		t.Fatalf("takeover: %v", err)
	}
}
