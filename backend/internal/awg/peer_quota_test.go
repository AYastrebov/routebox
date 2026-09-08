package awg

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"routebox/backend/internal/quota"
)

// Peer.Suspension is the only verdict the package renders about a peer being out
// of service, so the priority order (spec Q18) is pinned here rather than at each
// call site.
func TestPeerSuspensionPriority(t *testing.T) {
	tests := []struct {
		name string
		p    Peer
		now  int64
		want quota.Reason
	}{
		{"fresh peer", Peer{}, 1000, quota.ReasonNone},
		{"limit not reached", Peer{QuotaBytes: 100, UsedRx: 40, UsedTx: 50}, 1000, quota.ReasonNone},
		{"limit reached exactly", Peer{QuotaBytes: 100, UsedRx: 40, UsedTx: 60}, 1000, quota.ReasonQuota},
		{"no limit, heavy use", Peer{UsedRx: 1 << 40, UsedTx: 1 << 40}, 1000, quota.ReasonNone},
		{"expired", Peer{ExpiresAt: 1000}, 1000, quota.ReasonExpired},
		{"not yet expired", Peer{ExpiresAt: 1001}, 1000, quota.ReasonNone},
		{"quota outranks expiry", Peer{QuotaBytes: 10, UsedTx: 10, ExpiresAt: 1}, 1000, quota.ReasonQuota},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.Suspension(tt.now); got != tt.want {
				t.Fatalf("Suspension = %q, want %q", got, tt.want)
			}
			if got := tt.p.Suspended(tt.now); got != (tt.want != quota.ReasonNone) {
				t.Fatalf("Suspended = %v for reason %q", got, tt.want)
			}
		})
	}
}

// A peers.toml written before quotas existed must load as "no limit, nothing
// spent" — not as a peer that is instantly out of service.
func TestStoreLoadsPeersTomlWithoutQuotaFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "peers.toml")
	legacy := "server_key = \"k\"\n\n[[peers]]\n" +
		"public_key = \"P\"\nprivate_key = \"S\"\naddress = \"10.10.0.2/32\"\n" +
		"name = \"phone\"\ncreated_at = 100\nexpires_at = 0\n"
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewStore(path)
	if err := s.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	p, ok := s.Get("P")
	if !ok {
		t.Fatal("legacy peer not loaded")
	}
	if p.QuotaBytes != 0 || p.UsedRx != 0 || p.UsedTx != 0 || p.UsedResetAt != 0 {
		t.Fatalf("legacy peer must default to no limit and no usage: %+v", p)
	}
	if p.Suspended(1 << 40) {
		t.Fatal("a legacy peer that never expires must stay in service")
	}
}

func TestPeerLinesExcludesQuotaExhausted(t *testing.T) {
	m := newTestManager(t, newFakeRunner())
	m.store.now = func() int64 { return 1000 }
	_ = m.store.Put(Peer{PublicKey: "under", Address: "10.10.0.2/32", QuotaBytes: 100, UsedRx: 99})
	_ = m.store.Put(Peer{PublicKey: "spent", Address: "10.10.0.3/32", QuotaBytes: 100, UsedRx: 60, UsedTx: 40})
	_ = m.store.Put(Peer{PublicKey: "unlimited", Address: "10.10.0.4/32", UsedRx: 1 << 40})

	got := map[string]bool{}
	for _, pl := range m.peerLines() {
		got[pl.PublicKey] = true
	}
	if !got["under"] || !got["unlimited"] || got["spent"] {
		t.Fatalf("peerLines must drop only the peer over its quota: %v", got)
	}
}

// Same contract as TestSweepExpiredSuspendsLivePeer, for the other reason: a peer
// that used up its limit comes off the interface and out of the conf, and keeps
// its secret so raising the limit puts it back.
func TestSweepExpiredSuspendsQuotaExhaustedPeer(t *testing.T) {
	f := newFakeRunner()
	m := newTestManager(t, f)
	seedConf(t, m)
	m.store.now = func() int64 { return 2000 }
	_ = m.store.Put(Peer{PublicKey: "spent", PresharedKey: "p", Address: "10.10.0.2/32", QuotaBytes: 1024, UsedRx: 1000, UsedTx: 24})
	f.outputs["awg show awg-rb0"] = "peer: spent\n"
	m.appendPeerToConf(PeerLine{Name: "x", PublicKey: "spent", PSK: "p", AllowedIP: "10.10.0.2/32"})

	m.SweepExpired(context.Background())

	if !f.sawContains("awg set awg-rb0 peer spent remove") {
		t.Fatalf("expected live remove; calls=%v", f.calls)
	}
	if data, _ := os.ReadFile(m.confPath); strings.Contains(string(data), "PublicKey = spent") {
		t.Fatalf("quota-exhausted peer still in conf:\n%s", data)
	}
	if _, ok := m.store.Get("spent"); !ok {
		t.Fatal("SweepExpired must keep the store secret of a quota-suspended peer")
	}
}

// Expiry and quota are independent (spec Q16): extending the date of a peer that
// has spent its allowance must not put it back on the interface.
func TestSetPeerLimitsDoesNotReadmitQuotaExhausted(t *testing.T) {
	f := newFakeRunner()
	m := newTestManager(t, f)
	seedConf(t, m)
	m.store.now = func() int64 { return 1000 }
	_ = m.store.Put(Peer{
		PublicKey: validPub, PresharedKey: "psk", Address: "10.10.0.2/32", Name: "bob",
		ExpiresAt: 500, QuotaBytes: 1024, UsedRx: 2048,
	})

	if err := m.SetPeerLimits(context.Background(), validPub, 5000, 1024); err != nil {
		t.Fatalf("SetPeerLimits: %v", err)
	}
	if got, _ := m.store.Get(validPub); got.ExpiresAt != 5000 {
		t.Fatalf("the new date must still be stored: %+v", got)
	}
	if f.sawContains("awg set awg-rb0 peer " + validPub) {
		t.Fatalf("a peer over its quota must stay off the interface; calls=%v", f.calls)
	}
	if data, _ := os.ReadFile(m.confPath); strings.Contains(string(data), "PublicKey = "+validPub) {
		t.Fatalf("a peer over its quota must stay out of the conf:\n%s", data)
	}
}

// The sing-box backend renders the endpoint's peer list from the same store, so
// a quota-exhausted peer must vanish from the spec exactly as an expired one does
// (TestSingbox_ExpiredPeerOmittedFromSpec).
func TestSingbox_QuotaExhaustedPeerOmittedFromSpec(t *testing.T) {
	m, fs, _ := newSingboxMgr(t)
	if _, err := m.AddPeer(context.Background(), "bob"); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}
	if len(fs.lastSpec.Peers) != 1 {
		t.Fatalf("setup: want the peer in the spec, got %d", len(fs.lastSpec.Peers))
	}
	p, _ := m.store.Get(fs.lastSpec.Peers[0].PublicKey)
	p.QuotaBytes, p.UsedRx, p.UsedTx = 1024, 1000, 24
	if err := m.store.Put(p); err != nil {
		t.Fatal(err)
	}
	if err := m.singboxSync(); err != nil {
		t.Fatalf("singboxSync: %v", err)
	}
	if len(fs.lastSpec.Peers) != 0 {
		t.Fatalf("peer over its quota must be omitted from the spec, got %d", len(fs.lastSpec.Peers))
	}
}
