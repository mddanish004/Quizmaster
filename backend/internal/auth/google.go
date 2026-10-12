package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"golang.org/x/oauth2"
)

// GoogleClaims are the verified identity claims we retain.
type GoogleClaims struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
}

// OIDCVerifier exchanges codes and verifies ID tokens.
type OIDCVerifier interface {
	// VerifyCode exchanges code+verifier and returns verified claims for the expected nonce.
	VerifyCode(ctx context.Context, code, verifier, nonce string) (GoogleClaims, error)
}

// FakeOIDCVerifier is the deterministic test/mock verifier (MOCK_OIDC=true).
// It accepts codes of the form "mock:<sub>:<email>" and returns fixed claims.
type FakeOIDCVerifier struct{}

func (FakeOIDCVerifier) VerifyCode(_ context.Context, code, _, _ string) (GoogleClaims, error) {
	parts := strings.SplitN(code, ":", 3)
	if len(parts) != 3 || parts[0] != "mock" || parts[1] == "" {
		return GoogleClaims{}, errors.New("invalid mock code")
	}
	email := parts[2]
	if email == "" {
		email = parts[1] + "@example.com"
	}
	return GoogleClaims{Subject: "google-" + parts[1], Email: email, EmailVerified: true, Name: parts[1]}, nil
}

// RealOIDCVerifier implements authorization-code + PKCE against Google using
// the vetted go-oidc library (ADR-014): provider discovery, JWKS caching and
// rotation, and RS256 signature verification. It fails closed on any error.
type RealOIDCVerifier struct {
	// Issuer is https://accounts.google.com in production; override only for tests.
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	HTTP         *http.Client

	mu       sync.Mutex
	provider *oidc.Provider
}

func (v *RealOIDCVerifier) issuer() string {
	if v.Issuer != "" {
		return v.Issuer
	}
	return "https://accounts.google.com"
}

func (v *RealOIDCVerifier) http() *http.Client {
	if v.HTTP != nil {
		return v.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// providerFor discovers once and caches (go-oidc caches JWKS and refreshes
// automatically when an unknown key ID arrives — rotation-safe).
func (v *RealOIDCVerifier) providerFor(ctx context.Context) (*oidc.Provider, error) {
	v.mu.Lock()
	cached := v.provider
	v.mu.Unlock()
	if cached != nil {
		return cached, nil
	}
	dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	dctx = oidc.ClientContext(dctx, v.http())
	provider, err := oidc.NewProvider(dctx, v.issuer())
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	v.mu.Lock()
	v.provider = provider
	v.mu.Unlock()
	return provider, nil
}

// VerifyCode exchanges the code (PKCE-bound) and verifies the ID token:
// discovery issuer, RS256 signature via JWKS, iss/aud/exp (go-oidc), plus
// explicit header-alg, azp, and nonce checks here.
func (v *RealOIDCVerifier) VerifyCode(ctx context.Context, code, verifier, nonce string) (GoogleClaims, error) {
	if v.ClientID == "" {
		return GoogleClaims{}, errors.New("google client not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	provider, err := v.providerFor(ctx)
	if err != nil {
		return GoogleClaims{}, err
	}
	oauthCfg := oauth2.Config{
		ClientID:     v.ClientID,
		ClientSecret: v.ClientSecret,
		RedirectURL:  v.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{"openid", "email", "profile"},
	}
	ctx = oidc.ClientContext(ctx, v.http())
	token, err := oauthCfg.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return GoogleClaims{}, fmt.Errorf("token exchange: %w", err)
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || raw == "" {
		return GoogleClaims{}, errors.New("missing id_token")
	}
	if alg, err := tokenAlg(raw); err != nil || alg != "RS256" {
		return GoogleClaims{}, errors.New("unexpected id_token alg")
	}
	idToken, err := provider.Verifier(&oidc.Config{ClientID: v.ClientID}).Verify(ctx, raw)
	if err != nil {
		return GoogleClaims{}, fmt.Errorf("id_token verify: %w", err)
	}
	var claims struct {
		Subject       string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		Nonce         string `json:"nonce"`
		Azp           string `json:"azp"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return GoogleClaims{}, fmt.Errorf("claims: %w", err)
	}
	if claims.Subject == "" {
		return GoogleClaims{}, errors.New("missing sub")
	}
	if claims.Azp != "" && claims.Azp != v.ClientID {
		return GoogleClaims{}, errors.New("azp mismatch")
	}
	// Nonce binds this token to our transaction; both empty is a failure.
	if nonce == "" || claims.Nonce == "" || subtle.ConstantTimeCompare([]byte(claims.Nonce), []byte(nonce)) != 1 {
		return GoogleClaims{}, errors.New("nonce mismatch")
	}
	return GoogleClaims{Subject: claims.Subject, Email: claims.Email, EmailVerified: claims.EmailVerified, Name: claims.Name}, nil
}

// tokenAlg extracts the JOSE header alg without verifying anything.
func tokenAlg(raw string) (string, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return "", errors.New("malformed token")
	}
	hb, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", err
	}
	var header struct {
		Alg string `json:"alg"`
	}
	if err := json.Unmarshal(hb, &header); err != nil {
		return "", err
	}
	return header.Alg, nil
}

// OAuthTransaction is a short-lived login/link attempt.
type OAuthTransaction struct {
	ID         uuid.UUID
	StateHash  []byte
	NonceHash  []byte
	Nonce      string // plaintext binding value; row expires in 10 minutes
	Verifier   []byte // stored encrypted in production; plaintext in MVP with DB access controls
	ReturnPath string
	Kind       string
	LinkUserID *uuid.UUID
	ExpiresAt  time.Time
}

// NewOAuthTransaction mints state/nonce/verifier for one authorization redirect.
func NewOAuthTransaction(returnPath, kind string, linkUser *uuid.UUID) (tx OAuthTransaction, state, nonce, verifier, challenge string, err error) {
	newB64 := func(n int) (string, []byte, error) {
		b := make([]byte, n)
		if _, err := rand.Read(b); err != nil {
			return "", nil, err
		}
		return base64.RawURLEncoding.EncodeToString(b), b, nil
	}
	state, stateRaw, err := newB64(32)
	if err != nil {
		return tx, "", "", "", "", err
	}
	nonce, nonceRaw, err := newB64(32)
	if err != nil {
		return tx, "", "", "", "", err
	}
	verifier, verifierRaw, err := newB64(32)
	if err != nil {
		return tx, "", "", "", "", err
	}
	sum := sha256.Sum256(verifierRaw)
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	sh := sha256.Sum256(stateRaw)
	nh := sha256.Sum256(nonceRaw)
	if returnPath == "" || !strings.HasPrefix(returnPath, "/") {
		returnPath = "/app"
	}
	tx = OAuthTransaction{
		ID: uuid.New(), StateHash: sh[:], NonceHash: nh[:], Nonce: nonce, Verifier: verifierRaw,
		ReturnPath: returnPath, Kind: kind, LinkUserID: linkUser, ExpiresAt: time.Now().Add(10 * time.Minute),
	}
	return tx, state, nonce, verifier, challenge, nil
}

// GoogleAuthURL builds the authorization endpoint URL.
func GoogleAuthURL(clientID, redirectURL, state, nonce, challenge string) string {
	q := url.Values{
		"client_id":             {clientID},
		"redirect_uri":          {redirectURL},
		"response_type":         {"code"},
		"scope":                 {"openid email profile"},
		"state":                 {state},
		"nonce":                 {nonce},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	return "https://accounts.google.com/o/oauth2/v2/auth?" + q.Encode()
}
