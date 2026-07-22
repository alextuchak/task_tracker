package identity

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSecret = "0123456789abcdef0123456789abcdef"

func TestIssueParseRoundtrip(t *testing.T) {
	p := NewProvider(Config{Secret: testSecret, TTL: time.Hour})

	raw, err := p.Issue(42)
	require.NoError(t, err)

	principal, err := p.Parse(raw)
	require.NoError(t, err)
	assert.Equal(t, Principal{UserID: 42}, principal)
}

func TestParseGarbage(t *testing.T) {
	p := NewProvider(Config{Secret: testSecret, TTL: time.Hour})

	_, err := p.Parse("not-a-token")

	require.ErrorIs(t, err, ErrInvalidToken)
}

func TestParseExpired(t *testing.T) {
	p := NewProvider(Config{Secret: testSecret, TTL: -time.Minute})

	raw, err := p.Issue(42)
	require.NoError(t, err)

	_, err = p.Parse(raw)
	require.ErrorIs(t, err, ErrInvalidToken)
}

func TestParseForeignIssuer(t *testing.T) {
	p := NewProvider(Config{Secret: testSecret, TTL: time.Hour})
	foreign := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer:    "another-service",
		Audience:  jwt.ClaimStrings{audience},
		Subject:   "42",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	})
	raw, err := foreign.SignedString([]byte(testSecret))
	require.NoError(t, err)

	_, err = p.Parse(raw)

	require.ErrorIs(t, err, ErrInvalidToken)
}

func TestParseForeignAudience(t *testing.T) {
	p := NewProvider(Config{Secret: testSecret, TTL: time.Hour})
	foreign := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer:    issuer,
		Audience:  jwt.ClaimStrings{"another-api"},
		Subject:   "42",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	})
	raw, err := foreign.SignedString([]byte(testSecret))
	require.NoError(t, err)

	_, err = p.Parse(raw)

	require.ErrorIs(t, err, ErrInvalidToken)
}

func TestParseWrongSecret(t *testing.T) {
	issuer := NewProvider(Config{Secret: testSecret, TTL: time.Hour})
	verifier := NewProvider(Config{Secret: "another-secret-another-secret-32b", TTL: time.Hour})

	raw, err := issuer.Issue(42)
	require.NoError(t, err)

	_, err = verifier.Parse(raw)
	require.ErrorIs(t, err, ErrInvalidToken)
}
