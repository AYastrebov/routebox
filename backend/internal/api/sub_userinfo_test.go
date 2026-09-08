package api

import (
	"net/http"
	"path/filepath"
	"testing"

	"routebox/backend/internal/traffic"
)

func TestFormatUserinfo(t *testing.T) {
	cases := []struct {
		up, down, total, expire int64
		want                    string
	}{
		{0, 0, 0, 0, "upload=0; download=0; total=0; expire=0"},
		{123, 456, 0, 0, "upload=123; download=456; total=0; expire=0"},
		{111, 222, 0, 1893456000, "upload=111; download=222; total=0; expire=1893456000"},
	}
	for _, c := range cases {
		if got := formatUserinfo(c.up, c.down, c.total, c.expire); got != c.want {
			t.Errorf("formatUserinfo(%d,%d,%d,%d) = %q, want %q",
				c.up, c.down, c.total, c.expire, got, c.want)
		}
	}
}

// TestSub_UserinfoHeader_FromTrafficStore proves the Subscription-Userinfo
// header is actually wired on the 200 path: the bytes come from h.traffic
// summed across the user's names, total is pinned to 0 (no quota), and expire
// reflects the registry user's ExpiresAt.
func TestSub_UserinfoHeader_FromTrafficStore(t *testing.T) {
	h, um := newSubHandler(t, "vpn.example.com")

	// Seed the reconciled user (Name "alice") with a non-zero ExpiresAt.
	u := um.List()[0]
	u.ExpiresAt = 1893456000
	if err := um.Put(&u); err != nil {
		t.Fatal(err)
	}

	// Wire a traffic store seeded under the user's display name.
	store, err := traffic.OpenStore(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()
	if err := store.UpsertUser(60, "alice", 111, 222); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	h.traffic = store

	rec := serveSub(h, u.Token, "203.0.113.11")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	got := rec.Header().Get("Subscription-Userinfo")
	want := "upload=111; download=222; total=0; expire=1893456000"
	if got != want {
		t.Errorf("Subscription-Userinfo = %q, want %q", got, want)
	}
}

// TestSub_UserinfoHeader_NoTraffic proves the header is present with zeroed
// usage when the user has no recorded traffic (and no ExpiresAt set).
func TestSub_UserinfoHeader_NoTraffic(t *testing.T) {
	h, um := newSubHandler(t, "vpn.example.com")

	store, err := traffic.OpenStore(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()
	h.traffic = store

	rec := serveSub(h, tokenOf(t, um), "203.0.113.12")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	got := rec.Header().Get("Subscription-Userinfo")
	want := "upload=0; download=0; total=0; expire=0"
	if got != want {
		t.Errorf("Subscription-Userinfo = %q, want %q", got, want)
	}
}

// TestSub_UserinfoHeader_TotalIsQuota is the ticket's third required test
// (spec Q12): total= carries the panel user's quota, not a hard-coded 0.
func TestSub_UserinfoHeader_TotalIsQuota(t *testing.T) {
	h, um := newSubHandler(t, "vpn.example.com")

	u := um.List()[0]
	u.QuotaBytes = 10 << 30 // 10 GiB
	u.ExpiresAt = 1893456000
	if err := um.Put(&u); err != nil {
		t.Fatal(err)
	}

	rec := serveSub(h, u.Token, "203.0.113.13")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	want := "upload=0; download=0; total=10737418240; expire=1893456000"
	if got := rec.Header().Get("Subscription-Userinfo"); got != want {
		t.Errorf("Subscription-Userinfo = %q, want %q", got, want)
	}
}

// TestSub_UserinfoHeader_UsesQuotaCountersWhenQuotaSet pins the pairing the
// header's three numbers only make sense as: with a quota set, upload/download
// are the quota's own counters (since the last reset), NOT the lifetime SQLite
// totals. Seeded so the two disagree — a reset user whose lifetime traffic is
// large — because that is exactly when a client would otherwise draw a full bar
// for a user in service.
func TestSub_UserinfoHeader_UsesQuotaCountersWhenQuotaSet(t *testing.T) {
	h, um := newSubHandler(t, "vpn.example.com")

	u := um.List()[0]
	u.QuotaBytes = 5000
	if err := um.Put(&u); err != nil {
		t.Fatal(err)
	}
	if _, err := um.AddUsage(map[string]struct{ Up, Down int64 }{
		u.Name: {Up: 10, Down: 20},
	}); err != nil {
		t.Fatalf("AddUsage: %v", err)
	}

	// A lifetime history far bigger than the post-reset counters.
	store, err := traffic.OpenStore(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()
	if err := store.UpsertUser(60, u.Name, 900000, 800000); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	h.traffic = store

	rec := serveSub(h, u.Token, "203.0.113.15")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	want := "upload=10; download=20; total=5000; expire=0"
	if got := rec.Header().Get("Subscription-Userinfo"); got != want {
		t.Errorf("Subscription-Userinfo = %q, want %q", got, want)
	}
}

// TestSub_UserinfoHeader_QuotaExhausted_EmptyBodyStillReportsTotal proves the
// suspended (quota) path emits the same total alongside the counters that spent
// it, so a client shows a full bar rather than "nothing used, no limit" while
// the subscription body is empty.
func TestSub_UserinfoHeader_QuotaExhausted_EmptyBodyStillReportsTotal(t *testing.T) {
	h, um := newSubHandler(t, "vpn.example.com")

	u := um.List()[0]
	u.QuotaBytes = 1000
	if err := um.Put(&u); err != nil {
		t.Fatal(err)
	}
	// Counters are written by the accounting path only — Put deliberately
	// preserves them, so seeding them through Put would be a no-op.
	if _, err := um.AddUsage(map[string]struct{ Up, Down int64 }{
		u.Name: {Up: 400, Down: 600},
	}); err != nil {
		t.Fatalf("AddUsage: %v", err)
	}

	rec := serveSub(h, u.Token, "203.0.113.14")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); body != "" {
		t.Errorf("quota-exhausted user must get an empty body, got %q", body)
	}
	want := "upload=400; download=600; total=1000; expire=0"
	if got := rec.Header().Get("Subscription-Userinfo"); got != want {
		t.Errorf("Subscription-Userinfo = %q, want %q", got, want)
	}
}
