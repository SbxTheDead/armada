package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/SbxTheDead/armada/internal/domain"
	"github.com/SbxTheDead/armada/internal/service"
	"github.com/SbxTheDead/armada/internal/store"
	"github.com/SbxTheDead/armada/internal/store/memory"
)

// fixedClock returns a controllable time source for deterministic tests.
type fixedClock struct{ t time.Time }

func (c *fixedClock) now() time.Time { return c.t }

func newFleet(t *testing.T, clk *fixedClock) (*service.Fleet, *memory.DB) {
	t.Helper()
	db := memory.New()
	seq := 0
	f := service.NewFleet(db.Systems, db.JoinTokens, db.Identities, db.Telemetry, service.Options{
		Now: clk.now,
		IDGen: func() string {
			seq++
			return "id-" + string(rune('a'+seq))
		},
		HeartbeatInterval: time.Minute,
	})
	return f, db
}

// joinOne creates a key with the given presets and joins one device through it,
// returning the resulting system.
func joinOne(t *testing.T, f *service.Fleet, in service.NewJoinTokenInput, facts domain.DeviceFacts) *domain.System {
	t.Helper()
	key, _, err := f.CreateJoinToken(context.Background(), in)
	if err != nil {
		t.Fatalf("create join token: %v", err)
	}
	res, err := f.JoinWithToken(context.Background(), key, facts)
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	return res.System
}

func TestHeartbeatDrivesHealth(t *testing.T) {
	clk := &fixedClock{t: time.Unix(1_700_000_000, 0).UTC()}
	f, _ := newFleet(t, clk)
	ctx := context.Background()

	sys := joinOne(t, f,
		service.NewJoinTokenInput{TenantID: "t1"},
		domain.DeviceFacts{MachineID: "m1", Hostname: "web-1", FQDN: "web1.example.com"},
	)
	if sys.Lifecycle != domain.LifecycleEnrolled {
		t.Fatalf("want enrolled after auto-join, got %s", sys.Lifecycle)
	}

	// Heartbeat flips health to healthy and records the agent version.
	if err := f.RecordHeartbeat(ctx, "t1", &domain.Heartbeat{SystemID: sys.ID, AgentVersion: "1.0.0"}); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	got, err := f.GetSystem(ctx, "t1", sys.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Health != domain.HealthHealthy {
		t.Fatalf("want healthy, got %s", got.Health)
	}
	if got.AgentVersion != "1.0.0" {
		t.Fatalf("agent version not recorded: %q", got.AgentVersion)
	}

	// Advance time past 3 intervals: system reads offline at query time.
	clk.t = clk.t.Add(4 * time.Minute)
	got, _ = f.GetSystem(ctx, "t1", sys.ID)
	if got.Health != domain.HealthOffline {
		t.Fatalf("want offline after missed heartbeats, got %s", got.Health)
	}
}

func TestAuthenticateAgent(t *testing.T) {
	clk := &fixedClock{t: time.Unix(1_700_000_000, 0).UTC()}
	f, _ := newFleet(t, clk)
	ctx := context.Background()

	key, _, _ := f.CreateJoinToken(ctx, service.NewJoinTokenInput{TenantID: "t1"})
	res, err := f.JoinWithToken(ctx, key, domain.DeviceFacts{MachineID: "m1", Hostname: "h1"})
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	id, err := f.AuthenticateAgent(ctx, res.APIKey)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if id.SystemID != res.System.ID {
		t.Fatalf("identity bound to wrong system: %s", id.SystemID)
	}
	if _, err := f.AuthenticateAgent(ctx, "bogus"); err == nil {
		t.Fatal("expected bogus key to be rejected")
	}
}

func TestListSystems_Filter(t *testing.T) {
	clk := &fixedClock{t: time.Unix(1_700_000_000, 0).UTC()}
	f, _ := newFleet(t, clk)
	ctx := context.Background()

	// Two keys with different region presets, one device each.
	_ = joinOne(t, f,
		service.NewJoinTokenInput{TenantID: "t1", Region: "eu"},
		domain.DeviceFacts{MachineID: "m-a", Hostname: "a", FQDN: "a.ex"},
	)
	_ = joinOne(t, f,
		service.NewJoinTokenInput{TenantID: "t1", Region: "us"},
		domain.DeviceFacts{MachineID: "m-b", Hostname: "b", FQDN: "b.ex"},
	)

	got, err := f.ListSystems(ctx, "t1", store.SystemFilter{Region: "eu"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].Name != "a" {
		t.Fatalf("region filter failed: %+v", got)
	}
}
