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
	"net/url"
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

// Join redeems a reusable join token, self-registering the device from its
// reported facts. Returns the issued system ID and bearer API key.
func (c *Client) Join(ctx context.Context, joinToken string, facts domain.DeviceFacts) (systemID, apiKey string, err error) {
	if facts.AgentVersion == "" {
		facts.AgentVersion = c.version
	}
	body := struct {
		JoinToken string `json:"join_token"`
		domain.DeviceFacts
	}{JoinToken: joinToken, DeviceFacts: facts}
	var resp struct {
		SystemID string `json:"system_id"`
		APIKey   string `json:"api_key"`
	}
	if err := c.do(ctx, http.MethodPost, "/agent/v1/join", body, false, &resp); err != nil {
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

// ClaimTasks polls for pending tasks assigned to this device (marking them
// dispatched server-side).
func (c *Client) ClaimTasks(ctx context.Context) ([]domain.Task, error) {
	var resp struct {
		Tasks []domain.Task `json:"tasks"`
	}
	if err := c.do(ctx, http.MethodGet, "/agent/v1/tasks", nil, true, &resp); err != nil {
		return nil, err
	}
	return resp.Tasks, nil
}

// CompleteTask reports the outcome of running a task.
func (c *Client) CompleteTask(ctx context.Context, taskID string, exitCode int, output, errMsg string) error {
	body := map[string]any{"exit_code": exitCode, "output": output, "error": errMsg}
	return c.do(ctx, http.MethodPost, "/agent/v1/tasks/"+taskID+"/result", body, true, nil)
}

// FetchModule downloads a Python module's bytes by name.
func (c *Client) FetchModule(ctx context.Context, name string) ([]byte, error) {
	return c.fetchModule(ctx, "/agent/v1/modules/"+name)
}

// FetchNativeBinary downloads the native module build matching the given
// OS/arch (the caller passes its own runtime.GOOS/GOARCH).
func (c *Client) FetchNativeBinary(ctx context.Context, name, goos, goarch string) ([]byte, error) {
	q := url.Values{"os": {goos}, "arch": {goarch}}
	return c.fetchModule(ctx, "/agent/v1/modules/"+name+"?"+q.Encode())
}

func (c *Client) fetchModule(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch module: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("fetch module: server returned %d: %s", resp.StatusCode, bytes.TrimSpace(msg))
	}
	return io.ReadAll(resp.Body)
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
