package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/SbxTheDead/armada/internal/domain"
	"github.com/SbxTheDead/armada/internal/service"
	"github.com/SbxTheDead/armada/internal/store"
)

func TestJoinWithToken_AutoRegistersAndDedupes(t *testing.T) {
	clk := &fixedClock{t: time.Unix(1_700_000_000, 0).UTC()}
	f, _ := newFleet(t, clk)
	ctx := context.Background()

	// Reusable auto-approve key with group presets.
	key, _, err := f.CreateJoinToken(ctx, service.NewJoinTokenInput{
		TenantID: "t1", Name: "fleet", Region: "eu", Tags: []string{"iot"},
		Approval: domain.ApprovalAuto,
	})
	if err != nil {
		t.Fatalf("create join token: %v", err)
	}

	// Device A joins.
	resA, err := f.JoinWithToken(ctx, key, domain.DeviceFacts{
		MachineID: "machine-A", Hostname: "node-a", FQDN: "a.internal", OS: "linux", Arch: "arm64",
	})
	if err != nil {
		t.Fatalf("device A join: %v", err)
	}
	if resA.System.Lifecycle != domain.LifecycleEnrolled {
		t.Fatalf("auto-approve should enroll, got %s", resA.System.Lifecycle)
	}
	if resA.System.Region != "eu" || len(resA.System.Tags) != 1 || resA.System.Tags[0] != "iot" {
		t.Fatalf("presets not applied: %+v", resA.System)
	}
	if resA.System.Arch != "arm64" {
		t.Fatalf("facts not applied: %+v", resA.System)
	}

	// Device B (different machine) joins with the SAME key.
	resB, err := f.JoinWithToken(ctx, key, domain.DeviceFacts{
		MachineID: "machine-B", Hostname: "node-b", FQDN: "b.internal", OS: "linux", Arch: "amd64",
	})
	if err != nil {
		t.Fatalf("device B join: %v", err)
	}
	if resB.System.ID == resA.System.ID {
		t.Fatal("distinct machines must get distinct systems")
	}

	// Device A re-runs the installer (same machine-id): must re-attach, no dupe.
	resA2, err := f.JoinWithToken(ctx, key, domain.DeviceFacts{
		MachineID: "machine-A", Hostname: "node-a", FQDN: "a.internal", OS: "linux", Arch: "arm64",
	})
	if err != nil {
		t.Fatalf("device A re-join: %v", err)
	}
	if resA2.System.ID != resA.System.ID {
		t.Fatal("re-join of same machine must reuse its system (no duplicate)")
	}

	// Exactly two systems exist in the tenant.
	all, _ := f.ListSystems(ctx, "t1", store.SystemFilter{})
	if len(all) != 2 {
		t.Fatalf("want 2 systems after 3 joins (A twice), got %d", len(all))
	}

	// The key counted 3 uses.
	toks, _ := f.ListJoinTokens(ctx, "t1")
	if len(toks) != 1 || toks[0].Uses != 3 {
		t.Fatalf("want 1 key with 3 uses, got %+v", toks)
	}
}

func TestJoinWithToken_ManualApprovalGate(t *testing.T) {
	clk := &fixedClock{t: time.Unix(1_700_000_000, 0).UTC()}
	f, _ := newFleet(t, clk)
	ctx := context.Background()

	key, _, _ := f.CreateJoinToken(ctx, service.NewJoinTokenInput{
		TenantID: "t1", Approval: domain.ApprovalManual,
	})
	res, err := f.JoinWithToken(ctx, key, domain.DeviceFacts{MachineID: "m1", Hostname: "h1"})
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	if res.System.Lifecycle != domain.LifecyclePending {
		t.Fatalf("manual key should leave device pending, got %s", res.System.Lifecycle)
	}
	approved, err := f.ApproveSystem(ctx, "t1", res.System.ID)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if approved.Lifecycle != domain.LifecycleEnrolled {
		t.Fatalf("approve should enroll, got %s", approved.Lifecycle)
	}
}

func TestJoinWithToken_RevokedAndCapEnforced(t *testing.T) {
	clk := &fixedClock{t: time.Unix(1_700_000_000, 0).UTC()}
	f, _ := newFleet(t, clk)
	ctx := context.Background()

	// max-uses = 1.
	key, tok, _ := f.CreateJoinToken(ctx, service.NewJoinTokenInput{TenantID: "t1", MaxUses: 1})
	if _, err := f.JoinWithToken(ctx, key, domain.DeviceFacts{MachineID: "m1"}); err != nil {
		t.Fatalf("first join: %v", err)
	}
	// Second distinct machine exceeds the cap.
	if _, err := f.JoinWithToken(ctx, key, domain.DeviceFacts{MachineID: "m2"}); err == nil {
		t.Fatal("expected max-uses cap to reject the second join")
	}

	// Revocation blocks a fresh key too.
	key2, _, _ := f.CreateJoinToken(ctx, service.NewJoinTokenInput{TenantID: "t1"})
	if err := f.RevokeJoinToken(ctx, "t1", mustTokenID(t, f, key2)); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := f.JoinWithToken(ctx, key2, domain.DeviceFacts{MachineID: "m3"}); err == nil {
		t.Fatal("expected revoked key to be rejected")
	}
	_ = tok
}

// mustTokenID finds a join token's ID by listing (tests don't get it from the
// plaintext-returning create path directly here).
func mustTokenID(t *testing.T, f *service.Fleet, plaintext string) string {
	t.Helper()
	toks, _ := f.ListJoinTokens(context.Background(), "t1")
	// The most recently created token is the last in creation order.
	if len(toks) == 0 {
		t.Fatal("no tokens")
	}
	return toks[len(toks)-1].ID
}
