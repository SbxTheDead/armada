package httpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/SbxTheDead/armada/internal/httpapi"
	"github.com/SbxTheDead/armada/internal/service"
	"github.com/SbxTheDead/armada/internal/store/memory"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	db := memory.New()
	fleet := service.NewFleet(db.Systems, db.JoinTokens, db.Identities, db.Telemetry, service.Options{
		HeartbeatInterval: time.Minute,
	})
	srv := httpapi.New(httpapi.Config{Fleet: fleet})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

// doJSON is a tiny helper for issuing JSON requests in tests.
func doJSON(t *testing.T, method, url string, body any, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, data
}

func TestHealthz(t *testing.T) {
	ts := newTestServer(t)
	resp, _ := doJSON(t, http.MethodGet, ts.URL+"/healthz", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz status = %d", resp.StatusCode)
	}
}

func TestOperator_RequiresTenant(t *testing.T) {
	ts := newTestServer(t)
	resp, _ := doJSON(t, http.MethodGet, ts.URL+"/api/v1/systems", nil, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400 without tenant, got %d", resp.StatusCode)
	}
}

func TestJoinFlowOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	tenant := map[string]string{"X-Tenant-ID": "acme"}

	// Operator creates a reusable join key.
	resp, data := doJSON(t, http.MethodPost, ts.URL+"/api/v1/join-tokens", map[string]any{
		"name": "fleet", "region": "eu", "tags": []string{"iot"},
	}, tenant)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create join token = %d: %s", resp.StatusCode, data)
	}
	var jt struct {
		Token string `json:"token"`
	}
	mustJSON(t, data, &jt)
	if jt.Token == "" {
		t.Fatal("no join token returned")
	}

	// Device joins with facts (no prior registration).
	resp, data = doJSON(t, http.MethodPost, ts.URL+"/agent/v1/join", map[string]any{
		"join_token": jt.Token, "machine_id": "m-http-1", "hostname": "node-1",
		"fqdn": "node1.acme.internal", "os": "linux", "arch": "arm64",
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("join = %d: %s", resp.StatusCode, data)
	}
	var enr struct {
		SystemID string `json:"system_id"`
		APIKey   string `json:"api_key"`
	}
	mustJSON(t, data, &enr)
	if enr.APIKey == "" || enr.SystemID == "" {
		t.Fatalf("join did not return credentials: %s", data)
	}

	// The agent heartbeats with its bearer key and the system goes healthy.
	resp, data = doJSON(t, http.MethodPost, ts.URL+"/agent/v1/heartbeat", map[string]any{
		"agent_version": "1.2.3",
	}, map[string]string{"Authorization": "Bearer " + enr.APIKey})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("heartbeat = %d: %s", resp.StatusCode, data)
	}

	// A bogus bearer key is rejected.
	resp, _ = doJSON(t, http.MethodPost, ts.URL+"/agent/v1/heartbeat", map[string]any{}, map[string]string{
		"Authorization": "Bearer not-a-real-key",
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401 for bad bearer, got %d", resp.StatusCode)
	}

	// The auto-registered system carries the key's presets and reads healthy.
	resp, data = doJSON(t, http.MethodGet, ts.URL+"/api/v1/systems/"+enr.SystemID, nil, tenant)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get system = %d: %s", resp.StatusCode, data)
	}
	var sys struct {
		Region       string `json:"region"`
		Lifecycle    string `json:"lifecycle"`
		Health       string `json:"health"`
		AgentVersion string `json:"agent_version"`
	}
	mustJSON(t, data, &sys)
	if sys.Region != "eu" || sys.Lifecycle != "enrolled" {
		t.Fatalf("presets/lifecycle wrong: %s", data)
	}
	if sys.Health != "healthy" || sys.AgentVersion != "1.2.3" {
		t.Fatalf("heartbeat not reflected: %s", data)
	}

	// A bogus join key is cleanly rejected with 401 (not 500).
	resp, _ = doJSON(t, http.MethodPost, ts.URL+"/agent/v1/join", map[string]any{
		"join_token": "not-a-real-key", "machine_id": "m-http-2",
	}, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401 for bogus join key, got %d", resp.StatusCode)
	}
}

func mustJSON(t *testing.T, data []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
}
