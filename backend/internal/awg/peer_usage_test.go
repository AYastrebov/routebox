package awg

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"routebox/backend/internal/quota"
)

// seedUsagePeer stores one peer and returns it. Kernel tests key the store by a
// short literal (the store takes any string); only the peer ops validate keys.
func seedUsagePeer(t *testing.T, m *Manager, p Peer) {
	t.Helper()
	if err := m.store.Put(p); err != nil {
		t.Fatalf("seed peer: %v", err)
	}
}

// The tick folds the LIVE counters' deltas into the stored cumulative ones, and
// an interface restart (counters back to near-zero) must not roll the stored
// totals back — that is the whole reason the numbers are kept beside the peer
// instead of read off `awg show` (spec Q7).
func TestSweepAccumulatesUsageDeltas(t *testing.T) {
	ctx := context.Background()
	f := newFakeRunner()
	m := newTestManager(t, f)
	seedConf(t, m)
	m.store.now = func() int64 { return 2000 }
	seedUsagePeer(t, m, Peer{PublicKey: "P", PresharedKey: "p", Address: "10.10.0.2/32"})

	// First snapshot after process start: the interface may have been up without
	// us, so the whole current value is ours to count.
	f.outputs["awg show awg-rb0 transfer"] = "P\t100\t50\n"
	m.SweepExpired(ctx)
	if got, _ := m.store.Get("P"); got.UsedRx != 100 || got.UsedTx != 50 {
		t.Fatalf("first tick: used = %d/%d, want 100/50", got.UsedRx, got.UsedTx)
	}

	// Steady state: only the delta.
	f.outputs["awg show awg-rb0 transfer"] = "P\t250\t80\n"
	m.SweepExpired(ctx)
	if got, _ := m.store.Get("P"); got.UsedRx != 250 || got.UsedTx != 80 {
		t.Fatalf("second tick: used = %d/%d, want 250/80", got.UsedRx, got.UsedTx)
	}

	// Interface restarted: the live counter is lower than the last snapshot, so
	// the current value IS the delta — the stored totals never go down.
	f.outputs["awg show awg-rb0 transfer"] = "P\t30\t10\n"
	m.SweepExpired(ctx)
	if got, _ := m.store.Get("P"); got.UsedRx != 280 || got.UsedTx != 90 {
		t.Fatalf("after a counter reset: used = %d/%d, want 280/90", got.UsedRx, got.UsedTx)
	}
}

// A peer that drops out of the live snapshot (suspended, or the interface
// forgot it) contributes no delta, and its last value is forgotten so that
// re-admission counts from its fresh counter instead of a stale high-water mark.
func TestSweepForgetsPeerMissingFromSnapshot(t *testing.T) {
	ctx := context.Background()
	f := newFakeRunner()
	m := newTestManager(t, f)
	seedConf(t, m)
	m.store.now = func() int64 { return 2000 }
	seedUsagePeer(t, m, Peer{PublicKey: "P", PresharedKey: "p", Address: "10.10.0.2/32"})

	f.outputs["awg show awg-rb0 transfer"] = "P\t500\t500\n"
	m.SweepExpired(ctx)

	f.outputs["awg show awg-rb0 transfer"] = "" // gone from the interface
	m.SweepExpired(ctx)
	if got, _ := m.store.Get("P"); got.UsedRx != 500 || got.UsedTx != 500 {
		t.Fatalf("an absent peer must contribute no delta: used = %d/%d", got.UsedRx, got.UsedTx)
	}

	// Back with a fresh counter: the full value counts, not "700 minus 500".
	f.outputs["awg show awg-rb0 transfer"] = "P\t7\t7\n"
	m.SweepExpired(ctx)
	if got, _ := m.store.Get("P"); got.UsedRx != 507 || got.UsedTx != 507 {
		t.Fatalf("re-admitted peer must count from its fresh counter: used = %d/%d, want 507/507", got.UsedRx, got.UsedTx)
	}
}

// peers.toml holds client private keys on a router's flash. A tick that moved no
// bytes must not rewrite it — the sweep runs every 30s forever.
func TestSweepDoesNotRewritePeersTomlWithoutChanges(t *testing.T) {
	ctx := context.Background()
	f := newFakeRunner()
	m := newTestManager(t, f)
	seedConf(t, m)
	m.store.now = func() int64 { return 2000 }
	seedUsagePeer(t, m, Peer{PublicKey: "P", PresharedKey: "p", Address: "10.10.0.2/32"})

	f.outputs["awg show awg-rb0 transfer"] = "P\t100\t50\n"
	m.SweepExpired(ctx) // this one DOES change the counters

	path := m.store.GetPath()
	old := time.Unix(1000000, 0)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	m.SweepExpired(ctx) // same snapshot: nothing to add
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !st.ModTime().Equal(old) {
		t.Fatalf("peers.toml was rewritten on a tick with no traffic (mtime %v)", st.ModTime())
	}
}

// The suspension pass reads the counters this same tick folded in, so a peer
// that crosses its limit comes off the interface immediately, not one tick later.
func TestSweepSuspendsPeerCrossingQuotaSameTick(t *testing.T) {
	ctx := context.Background()
	f := newFakeRunner()
	m := newTestManager(t, f)
	seedConf(t, m)
	m.store.now = func() int64 { return 2000 }
	seedUsagePeer(t, m, Peer{PublicKey: "P", PresharedKey: "p", Address: "10.10.0.2/32", QuotaBytes: 100})
	m.appendPeerToConf(PeerLine{Name: "x", PublicKey: "P", PSK: "p", AllowedIP: "10.10.0.2/32"})
	f.outputs["awg show awg-rb0"] = "peer: P\n"
	f.outputs["awg show awg-rb0 transfer"] = "P\t60\t50\n" // 110 >= 100

	m.SweepExpired(ctx)

	if got, _ := m.store.Get("P"); got.Suspension(2000) != quota.ReasonQuota {
		t.Fatalf("peer must be over quota after the tick: %+v", got)
	}
	if !f.sawContains("awg set awg-rb0 peer P remove") {
		t.Fatalf("expected the live remove in the same tick; calls=%v", f.calls)
	}
	if data, _ := os.ReadFile(m.confPath); strings.Contains(string(data), "PublicKey = P") {
		t.Fatalf("quota-exhausted peer still in conf:\n%s", data)
	}
}

// Q19: raising the limit puts the peer back on the interface at save time — the
// operator must not have to wait for a tick that would not admit it anyway.
func TestSetPeerLimitsRaisingQuotaAdmitsImmediately(t *testing.T) {
	ctx := context.Background()
	f := newFakeRunner()
	m := newTestManager(t, f)
	seedConf(t, m)
	m.store.now = func() int64 { return 1000 }
	seedUsagePeer(t, m, Peer{
		PublicKey: validPub, PresharedKey: "psk", Address: "10.10.0.2/32", Name: "bob",
		QuotaBytes: 100, UsedRx: 200,
	})

	if err := m.SetPeerLimits(ctx, validPub, 0, 1000); err != nil {
		t.Fatalf("SetPeerLimits: %v", err)
	}
	got, _ := m.store.Get(validPub)
	if got.QuotaBytes != 1000 || got.ExpiresAt != 0 {
		t.Fatalf("both limits must be persisted: %+v", got)
	}
	if !f.sawContains("awg set awg-rb0 peer " + validPub) {
		t.Fatalf("expected an immediate re-admit; calls=%v", f.calls)
	}
	data, _ := os.ReadFile(m.confPath)
	if n := strings.Count(string(data), "PublicKey = "+validPub); n != 1 {
		t.Fatalf("expected exactly one conf block, got %d:\n%s", n, data)
	}
}

// Q20: lowering the limit below what is already spent is allowed and takes the
// peer off the interface at save time.
func TestSetPeerLimitsLoweringQuotaSuspendsImmediately(t *testing.T) {
	ctx := context.Background()
	f := newFakeRunner()
	m := newTestManager(t, f)
	seedConf(t, m)
	m.store.now = func() int64 { return 1000 }
	seedUsagePeer(t, m, Peer{
		PublicKey: validPub, PresharedKey: "psk", Address: "10.10.0.2/32", Name: "bob",
		UsedRx: 500, UsedTx: 500,
	})
	m.appendPeerToConf(PeerLine{Name: "bob", PublicKey: validPub, PSK: "psk", AllowedIP: "10.10.0.2/32"})

	if err := m.SetPeerLimits(ctx, validPub, 0, 100); err != nil {
		t.Fatalf("SetPeerLimits: %v", err)
	}
	if !f.sawContains("awg set awg-rb0 peer " + validPub + " remove") {
		t.Fatalf("expected an immediate suspend; calls=%v", f.calls)
	}
	if data, _ := os.ReadFile(m.confPath); strings.Contains(string(data), "PublicKey = "+validPub) {
		t.Fatalf("peer must be out of the conf:\n%s", data)
	}
}

// RenewPeer is the back-compat wrapper: it must keep the peer's quota untouched
// while it moves the date (spec Q16 — two independent tools).
func TestRenewPeerKeepsQuota(t *testing.T) {
	ctx := context.Background()
	f := newFakeRunner()
	m := newTestManager(t, f)
	seedConf(t, m)
	m.store.now = func() int64 { return 1000 }
	seedUsagePeer(t, m, Peer{
		PublicKey: validPub, PresharedKey: "psk", Address: "10.10.0.2/32", Name: "bob",
		ExpiresAt: 500, QuotaBytes: 4096, UsedRx: 10,
	})

	if err := m.RenewPeer(ctx, validPub, 5000); err != nil {
		t.Fatalf("RenewPeer: %v", err)
	}
	got, _ := m.store.Get(validPub)
	if got.ExpiresAt != 5000 || got.QuotaBytes != 4096 || got.UsedRx != 10 {
		t.Fatalf("renewal must move only the date: %+v", got)
	}
}

// Q9/Q19: resetting the counter zeroes it, stamps the reset moment, and returns
// the peer to service in the same call.
func TestResetPeerUsageZeroesAndAdmits(t *testing.T) {
	ctx := context.Background()
	f := newFakeRunner()
	m := newTestManager(t, f)
	seedConf(t, m)
	m.store.now = func() int64 { return 4242 }
	seedUsagePeer(t, m, Peer{
		PublicKey: validPub, PresharedKey: "psk", Address: "10.10.0.2/32", Name: "bob",
		QuotaBytes: 100, UsedRx: 90, UsedTx: 90,
	})

	if err := m.ResetPeerUsage(ctx, validPub); err != nil {
		t.Fatalf("ResetPeerUsage: %v", err)
	}
	got, _ := m.store.Get(validPub)
	if got.UsedRx != 0 || got.UsedTx != 0 {
		t.Fatalf("counters not zeroed: %+v", got)
	}
	if got.UsedResetAt != 4242 {
		t.Fatalf("UsedResetAt = %d, want the reset moment 4242", got.UsedResetAt)
	}
	if got.QuotaBytes != 100 {
		t.Fatalf("reset must not clear the limit: %+v", got)
	}
	if got.Suspended(4242) {
		t.Fatal("the peer must be back in service after a reset")
	}
	if !f.sawContains("awg set awg-rb0 peer " + validPub) {
		t.Fatalf("expected an immediate re-admit; calls=%v", f.calls)
	}
}

func TestResetPeerUsageUnknownPeer(t *testing.T) {
	m := newTestManager(t, newFakeRunner())
	seedConf(t, m)
	if err := m.ResetPeerUsage(context.Background(), otherValidPub); err != ErrPeerNotFound {
		t.Fatalf("want ErrPeerNotFound, got %v", err)
	}
}

// The roster carries the STORED cumulative numbers, not the live ones: the panel
// draws a quota bar, and a bar that resets whenever the interface restarts would
// tell the operator the peer got its allowance back.
func TestListPeersReportsStoredUsageAndQuota(t *testing.T) {
	ctx := context.Background()
	f := newFakeRunner()
	m := newTestManager(t, f)
	seedConf(t, m)
	m.store.now = func() int64 { return 2000 }
	seedUsagePeer(t, m, Peer{
		PublicKey: "P", PresharedKey: "p", Address: "10.10.0.2/32", Name: "bob",
		QuotaBytes: 1000, UsedRx: 700, UsedTx: 400, UsedResetAt: 123,
	})
	f.outputs["awg show awg-rb0 transfer"] = "P\t5\t5\n" // live counters differ

	peers := m.ListPeers(ctx)
	if len(peers) != 1 {
		t.Fatalf("want 1 peer, got %d", len(peers))
	}
	got := peers[0]
	if got.Rx != 700 || got.Tx != 400 {
		t.Fatalf("rx/tx must be the stored cumulative counters, got %d/%d", got.Rx, got.Tx)
	}
	if got.QuotaBytes != 1000 || got.UsedResetAt != 123 {
		t.Fatalf("quota fields missing from the summary: %+v", got)
	}
	if got.SuspendReason != quota.ReasonQuota {
		t.Fatalf("SuspendReason = %q, want %q", got.SuspendReason, quota.ReasonQuota)
	}
}

func TestListPeersActivePeerHasNoSuspendReason(t *testing.T) {
	m := newTestManager(t, newFakeRunner())
	seedConf(t, m)
	m.store.now = func() int64 { return 2000 }
	seedUsagePeer(t, m, Peer{PublicKey: "P", Address: "10.10.0.2/32", Name: "bob"})

	peers := m.ListPeers(context.Background())
	if len(peers) != 1 || peers[0].SuspendReason != quota.ReasonNone {
		t.Fatalf("an active peer must report no reason: %+v", peers)
	}
}

// The sing-box backend has no awg-rb0 to query: the same accounting runs off the
// fork's per-peer stats route (spec Q11), with no fallback to traffic history.
func TestSingboxSweepAccountsPeerStats(t *testing.T) {
	ctx := context.Background()
	m, _, _ := newSingboxMgr(t)
	m.store.now = func() int64 { return 2000 }
	seedUsagePeer(t, m, Peer{PublicKey: "P", PresharedKey: "p", Address: "10.10.0.2/32"})

	stat := PeerStat{RxBytes: 100, TxBytes: 40}
	m.SetPeerStats(func() (map[string]PeerStat, error) {
		return map[string]PeerStat{"P": stat}, nil
	})

	m.SweepExpired(ctx)
	if got, _ := m.store.Get("P"); got.UsedRx != 100 || got.UsedTx != 40 {
		t.Fatalf("first tick: used = %d/%d, want 100/40", got.UsedRx, got.UsedTx)
	}

	stat = PeerStat{RxBytes: 30, TxBytes: 90} // endpoint restarted: rx counter reset
	m.SweepExpired(ctx)
	if got, _ := m.store.Get("P"); got.UsedRx != 130 || got.UsedTx != 90 {
		t.Fatalf("second tick: used = %d/%d, want 130/90", got.UsedRx, got.UsedTx)
	}
}

// A peer that used up its allowance drops out of the rendered endpoint on the
// tick that notices, exactly as an expired one does.
func TestSingboxSweepSuspendsQuotaExhaustedPeer(t *testing.T) {
	ctx := context.Background()
	m, fs, _ := newSingboxMgr(t)
	if _, err := m.AddPeer(ctx, "bob"); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}
	pub := fs.lastSpec.Peers[0].PublicKey
	p, _ := m.store.Get(pub)
	p.QuotaBytes = 100
	if err := m.store.Put(p); err != nil {
		t.Fatal(err)
	}
	m.SetPeerStats(func() (map[string]PeerStat, error) {
		return map[string]PeerStat{pub: {RxBytes: 80, TxBytes: 80}}, nil
	})

	m.SweepExpired(ctx)

	if got, _ := m.store.Get(pub); got.UsedRx != 80 || got.UsedTx != 80 {
		t.Fatalf("counters not folded in: %+v", got)
	}
	if len(fs.lastSpec.Peers) != 0 {
		t.Fatalf("peer over its quota must be gone from the endpoint, got %d", len(fs.lastSpec.Peers))
	}
}

// No stats route (an amnezia-box that predates it, or a proxy that is not
// answering) means no accounting this tick — never a guess, and never a
// fallback to the SQLite traffic history (spec Q11).
func TestSingboxSweepWithoutStatsRouteSkipsAccounting(t *testing.T) {
	ctx := context.Background()
	m, _, _ := newSingboxMgr(t)
	m.store.now = func() int64 { return 2000 }
	seedUsagePeer(t, m, Peer{PublicKey: "P", PresharedKey: "p", Address: "10.10.0.2/32", UsedRx: 7})
	m.SetPeerStats(func() (map[string]PeerStat, error) {
		return nil, ErrAwgPeerStatsUnsupported
	})

	m.SweepExpired(ctx)

	if got, _ := m.store.Get("P"); got.UsedRx != 7 || got.UsedTx != 0 {
		t.Fatalf("a failed fetch must leave the counters alone: %+v", got)
	}
}

// The dedupe gate mirrors traffic.Sampler's: a route that stays broken logs once,
// not every 30 seconds forever.
func TestSingboxSweepStatsErrorLogsOnce(t *testing.T) {
	ctx := context.Background()
	m, _, _ := newSingboxMgr(t)
	seedUsagePeer(t, m, Peer{PublicKey: "P", Address: "10.10.0.2/32"})
	m.SetPeerStats(func() (map[string]PeerStat, error) {
		return nil, errors.New("connection refused")
	})

	m.SweepExpired(ctx)
	m.mu.Lock()
	first := m.lastUsageStatsErr
	m.mu.Unlock()
	if first == "" {
		t.Fatal("the first failure must be recorded so the second can be silenced")
	}
	m.SweepExpired(ctx)
	m.mu.Lock()
	second := m.lastUsageStatsErr
	m.mu.Unlock()
	if second != first {
		t.Fatalf("the gate must stay closed while the error is unchanged: %q -> %q", first, second)
	}
}
