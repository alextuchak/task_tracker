package token

import (
	"errors"
	"testing"
	"time"
)

func TestIssueParseRoundtrip(t *testing.T) {
	j := NewJWT(Config{Secret: "0123456789abcdef0123456789abcdef", TTL: time.Hour})

	raw, err := j.Issue(42)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	userID, err := j.Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if userID != 42 {
		t.Fatalf("expected user id 42, got %d", userID)
	}
}

func TestParseGarbage(t *testing.T) {
	j := NewJWT(Config{Secret: "0123456789abcdef0123456789abcdef", TTL: time.Hour})

	if _, err := j.Parse("not-a-token"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
}

func TestParseExpired(t *testing.T) {
	j := NewJWT(Config{Secret: "0123456789abcdef0123456789abcdef", TTL: -time.Minute})

	raw, err := j.Issue(42)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := j.Parse(raw); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken for expired, got %v", err)
	}
}

func TestParseWrongSecret(t *testing.T) {
	issuer := NewJWT(Config{Secret: "0123456789abcdef0123456789abcdef", TTL: time.Hour})
	verifier := NewJWT(Config{Secret: "another-secret-another-secret-32b", TTL: time.Hour})

	raw, err := issuer.Issue(42)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := verifier.Parse(raw); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken for wrong secret, got %v", err)
	}
}
