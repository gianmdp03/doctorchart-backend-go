// Package mailclient is a dependency-free HTTP client for the internal email API.
// It never retries automatically: a lost delivery acknowledgement can be ambiguous.
package mailclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Message struct {
	ID       string `json:"message_id"`
	Category string `json:"category,omitempty"`
	To       string `json:"to"`
	Subject  string `json:"subject"`
	HTML     string `json:"html,omitempty"`
	Text     string `json:"text,omitempty"`
}
type Result struct {
	ID     string `json:"message_id"`
	Status string `json:"status"`
}
type APIError struct {
	Status int
	Code   string
}

func (e *APIError) Error() string { return fmt.Sprintf("mail service HTTP %d (%s)", e.Status, e.Code) }

type Client struct {
	endpoint, key string
	http          *http.Client
}

func New(baseURL, key string, transport *http.Client) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("invalid mail service URL")
	}
	if len(key) < 32 || len(key) > 4096 {
		return nil, errors.New("invalid mail service credential")
	}
	for _, r := range key {
		if r < 33 || r > 126 {
			return nil, errors.New("invalid mail service credential")
		}
	}
	c := http.Client{Timeout: 25 * time.Second}
	if transport != nil {
		c = *transport
		if c.Timeout <= 0 {
			c.Timeout = 25 * time.Second
		}
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{strings.TrimRight(baseURL, "/") + "/v1/emails", key, &c}, nil
}

func (c *Client) Send(ctx context.Context, m Message) (Result, error) {
	var result Result
	if len(m.HTML)+len(m.Text) > 512<<10 {
		return result, errors.New("email body exceeds limit")
	}
	body, err := json.Marshal(m)
	if err != nil {
		return result, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return result, errors.New("invalid mail request")
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return result, errors.New("mail service request failed; delivery may be unknown")
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 8193))
	if err != nil || len(data) > 8192 {
		return result, errors.New("invalid mail service response")
	}
	if res.StatusCode != http.StatusOK {
		var p struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.Unmarshal(data, &p)
		code := "request_failed"
		switch p.Error.Code {
		case "unauthorized", "mail_forbidden", "invalid_mail", "invalid_json", "body_too_large", "unsupported_media_type", "mail_busy", "smtp_failed", "mail_disabled":
			code = p.Error.Code
		}
		return result, &APIError{res.StatusCode, code}
	}
	if json.Unmarshal(data, &result) != nil || result.ID != m.ID || result.Status != "accepted_by_provider" {
		return Result{}, errors.New("invalid mail delivery acknowledgement")
	}
	return result, nil
}
