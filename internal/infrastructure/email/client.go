package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/sony/gobreaker/v2"
)

type Client struct {
	http    *http.Client
	breaker *gobreaker.CircuitBreaker[struct{}]
	baseURL string
}

func NewClient(cfg Config) *Client {
	settings := gobreaker.Settings{
		Name:    "email-service",
		Timeout: cfg.OpenFor,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= cfg.MaxFailures
		},
	}
	return &Client{
		http:    &http.Client{Timeout: cfg.Timeout},
		breaker: gobreaker.NewCircuitBreaker[struct{}](settings),
		baseURL: cfg.BaseURL,
	}
}

type invitePayload struct {
	Email  string `json:"email"`
	TeamID int64  `json:"team_id"`
}

func (c *Client) SendInvite(ctx context.Context, to string, teamID int64) error {
	_, err := c.breaker.Execute(func() (struct{}, error) {
		body, err := json.Marshal(invitePayload{Email: to, TeamID: teamID})
		if err != nil {
			return struct{}{}, fmt.Errorf("marshal invite: %w", err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			c.baseURL+"/invites", bytes.NewReader(body))
		if err != nil {
			return struct{}{}, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.http.Do(req)
		if err != nil {
			return struct{}{}, fmt.Errorf("send invite: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode >= http.StatusBadRequest {
			return struct{}{}, fmt.Errorf("email service status %d", resp.StatusCode)
		}
		return struct{}{}, nil
	})
	return err
}
