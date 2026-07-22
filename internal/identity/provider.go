package identity

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var ErrInvalidToken = errors.New("invalid token")

const (
	issuer   = "task-tracker"
	audience = "task-tracker-api"
)

func NewProvider(cfg Config) *Provider {
	return &Provider{secret: []byte(cfg.Secret), ttl: cfg.TTL}
}

type Provider struct {
	secret []byte
	ttl    time.Duration
}

func (p *Provider) Issue(userID int64) (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Issuer:    issuer,
		Audience:  jwt.ClaimStrings{audience},
		Subject:   strconv.FormatInt(userID, 10),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(p.ttl)),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(p.secret)
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}
	return signed, nil
}

func (p *Provider) Parse(raw string) (Principal, error) {
	parsed, err := jwt.ParseWithClaims(raw, &jwt.RegisteredClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("%w: unexpected signing method %s", ErrInvalidToken, t.Method.Alg())
		}
		return p.secret, nil
	}, jwt.WithIssuer(issuer), jwt.WithAudience(audience))
	if err != nil {
		return Principal{}, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	claims, ok := parsed.Claims.(*jwt.RegisteredClaims)
	if !ok {
		return Principal{}, ErrInvalidToken
	}
	userID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil {
		return Principal{}, fmt.Errorf("%w: bad subject: %w", ErrInvalidToken, err)
	}
	return Principal{UserID: userID}, nil
}
