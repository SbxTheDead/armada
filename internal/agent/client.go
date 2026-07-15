// Package agent implements the management agent's control-plane client and run
// loop. The agent's job is strictly observe-and-report plus running
// administrator-approved tasks; it holds no persistence, stealth, or evasion
// behaviour by design.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/SbxTheDead/armada/internal/domain"
)

// Client talks to armada-server over HTTPS (HTTP in dev). It carries the
// agent's bearer API key on authenticated calls.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
	version string
}

// NewClient constructs a control-plane client.
func NewClient(baseURL, apiKey, version string) *Client {
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		version: version,
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SetAPIKey updates the bearer key after enrollment.
func (c *Client) SetAPIKey(key string) { c.apiKey = key }

// Enroll redeems an enrollment token and returns the issued system ID and API
// key. It is called once, before the agent has credentials.
func (c *Client) Enroll(ctx context.Context, token, fqdn string) (systemID, apiKey string, err error) {
	body := map[string]string{"token": token, "fqdn": fqdn}
	var resp struct {
		SystemID string `json:"system_id"`
		APIKey   string `json:"api_key"`
	}
	if err := c.do(ctx, http.MethodPost, "/agent/v1/enroll", body, false, &resp); err != nil {
		return "", "", err
	}
	return resp.SystemID, resp.APIKey, nil
}

// SendHeartbeat posts a liveness beacon with current metrics.
func (c *Client) SendHeartbeat(ctx context.Context, hb domain.Heartbeat) error {
	hb.AgentVersion = c.version
	return c.do(ctx, http.MethodPost, "/agent/v1/heartbeat", hb, true, nil)
}

// SendInventory uploads a full inventory snapshot.
func (c *Client) SendInventory(ctx context.Context, inv domain.Inventory) error {
	return c.do(ctx, http.MethodPost, "/agent/v1/inventory", inv, true, nil)
}

// do performs a JSON request, optionally authenticated, and decodes a JSON
// response into out (which may be nil).
func (c *Client) do(ctx context.Context, method, path string, in any, auth bool, out any) error {
	var reader io.Reader
	if in != nil {
		buf, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if auth {
		if c.apiKey == "" {
			return fmt.Errorf("no API key set for authenticated call to %s", path)
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("%s %s: server returned %d: %s", method, path, resp.StatusCode, bytes.TrimSpace(msg))
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}
