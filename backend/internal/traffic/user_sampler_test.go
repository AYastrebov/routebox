package traffic

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"routebox/backend/internal/v2stats"
)

// fakeUserQuerier feeds sampleOnce a scripted error-then-success sequence.
type fakeUserQuerier struct {
	snaps []map[string]v2stats.Counters
	errs  []error
	i     int
}

func (f *fakeUserQuerier) QueryUsersTimeout(time.Duration) (map[string]v2stats.Counters, error) {
	idx := f.i
	f.i++
	if idx < len(f.errs) && f.errs[idx] != nil {
		return nil, f.errs[idx]
	}
	if idx < len(f.snaps) {
		return f.snaps[idx], nil
	}
	return map[string]v2stats.Counters{}, nil
}

// TestSampleOnce_ErrorTickNoStateChange_RecoverRecordsDelta locks the log-gate
// contract: an error tick mutates neither lastSeen nor the store and flips the gate
// to "failed"; the next successful tick clears the gate and records the delta
// against the established baseline (no panic, no negative).
func TestSampleOnce_ErrorTickNoStateChange_RecoverRecordsDelta(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()
	s := NewUserSampler(store)

	q := &fakeUserQuerier{
		snaps: []map[string]v2stats.Counters{
			{"alice": {Uplink: 100, Downlink: 200}}, // tick 0: baseline (success)
			nil,                                     // tick 1: error
			{"alice": {Uplink: 150, Downlink: 260}}, // tick 2: recover, +50/+60
		},
		errs: []error{nil, errors.New("unavailable"), nil},
	}

	// tick 0: baseline established, full volume recorded.
	if failed := s.sampleOnce(q, time.Second, false); failed {
		t.Fatalf("tick0 success should leave gate open")
	}
	base := s.lastSeen["alice"]
	if base.Uplink != 100 || base.Downlink != 200 {
		t.Fatalf("baseline = %+v, want 100/200", base)
	}

	// tick 1: error — gate flips, lastSeen untouched, store unchanged.
	if failed := s.sampleOnce(q, time.Second, false); !failed {
		t.Fatalf("error tick must set gate failed=true")
	}
	if got := s.lastSeen["alice"]; got != base {
		t.Fatalf("error tick mutated lastSeen: %+v != %+v", got, base)
	}
	up, down, _ := store.QueryUserTotals(0, time.Now().Unix()+60, "alice")
	if up != 100 || down != 200 {
		t.Fatalf("error tick mutated store: up/down = %d/%d, want 100/200", up, down)
	}

	// tick 2: recovery — gate clears, +50/+60 recorded against baseline.
	if failed := s.sampleOnce(q, time.Second, true); failed {
		t.Fatalf("recovery tick must clear gate (failed=false)")
	}
	up, down, _ = store.QueryUserTotals(0, time.Now().Unix()+60, "alice")
	if up != 150 || down != 260 {
		t.Fatalf("after recovery up/down = %d/%d, want 150/260 (100+50, 200+60)", up, down)
	}
}

func TestUserDeltas_FirstSnapshotIsFullVolume(t *testing.T) {
	s := NewUserSampler(nil)
	d := s.computeUserDeltas(map[string]v2stats.Counters{"alice": {Uplink: 100, Downlink: 200}})
	if d["alice"].Upload != 100 || d["alice"].Download != 200 {
		t.Fatalf("got %+v, want alice 100/200", d)
	}
}

func TestUserDeltas_GrowthEmitsDelta(t *testing.T) {
	s := NewUserSampler(nil)
	s.computeUserDeltas(map[string]v2stats.Counters{"alice": {Uplink: 100, Downlink: 200}})
	d := s.computeUserDeltas(map[string]v2stats.Counters{"alice": {Uplink: 150, Downlink: 260}})
	if d["alice"].Upload != 50 || d["alice"].Download != 60 {
		t.Fatalf("got %+v, want 50/60", d["alice"])
	}
}

func TestUserDeltas_NoChangeNoEntry(t *testing.T) {
	s := NewUserSampler(nil)
	s.computeUserDeltas(map[string]v2stats.Counters{"alice": {Uplink: 100, Downlink: 200}})
	d := s.computeUserDeltas(map[string]v2stats.Counters{"alice": {Uplink: 100, Downlink: 200}})
	if len(d) != 0 {
		t.Fatalf("got %+v, want empty", d)
	}
}

func TestUserDeltas_CounterResetCountsCurrentNeverNegative(t *testing.T) {
	s := NewUserSampler(nil)
	s.computeUserDeltas(map[string]v2stats.Counters{"alice": {Uplink: 1000, Downlink: 1000}})
	// sing-box restarted: counter dropped. Emit current value, never negative.
	d := s.computeUserDeltas(map[string]v2stats.Counters{"alice": {Uplink: 30, Downlink: 40}})
	if d["alice"].Upload != 30 || d["alice"].Download != 40 {
		t.Fatalf("got %+v, want 30/40 (reset)", d["alice"])
	}
	// next tick 30->130 must be a normal +100 delta against the NEW baseline.
	d = s.computeUserDeltas(map[string]v2stats.Counters{"alice": {Uplink: 130, Downlink: 40}})
	if d["alice"].Upload != 100 || d["alice"].Download != 0 {
		t.Fatalf("got %+v, want +100/0 after rebaseline", d["alice"])
	}
}

func TestUserDeltas_VanishedUserEvictedThenFreshOnReturn(t *testing.T) {
	s := NewUserSampler(nil)
	s.computeUserDeltas(map[string]v2stats.Counters{"alice": {Uplink: 100, Downlink: 100}})
	s.computeUserDeltas(map[string]v2stats.Counters{}) // alice gone
	if _, ok := s.lastSeen["alice"]; ok {
		t.Fatal("alice should be evicted from lastSeen")
	}
	// alice returns → treated as new (full value), not a giant delta vs old.
	d := s.computeUserDeltas(map[string]v2stats.Counters{"alice": {Uplink: 5, Downlink: 0}})
	if d["alice"].Upload != 5 {
		t.Fatalf("reappeared alice = %+v, want 5", d["alice"])
	}
}

func TestUserDeltas_NewUserAlongsideExisting(t *testing.T) {
	s := NewUserSampler(nil)
	s.computeUserDeltas(map[string]v2stats.Counters{"alice": {Uplink: 100}})
	d := s.computeUserDeltas(map[string]v2stats.Counters{
		"alice": {Uplink: 120},
		"bob":   {Uplink: 7},
	})
	if d["alice"].Upload != 20 {
		t.Errorf("alice = %+v, want +20", d["alice"])
	}
	if d["bob"].Upload != 7 {
		t.Errorf("bob = %+v, want full 7", d["bob"])
	}
}

// TestSampleOnce_OnDeltasSink proves the optional sink receives exactly the
// deltas that go to SQLite, so the panel-user accounting (quota) sees the same
// numbers as the history store.
func TestSampleOnce_OnDeltasSink(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()
	s := NewUserSampler(store)

	var got []map[string]UserDelta
	s.OnDeltas = func(d map[string]UserDelta) { got = append(got, d) }

	q := &fakeUserQuerier{snaps: []map[string]v2stats.Counters{
		{"alice": {Uplink: 10, Downlink: 20}},
		{"alice": {Uplink: 15, Downlink: 20}},
		{"alice": {Uplink: 15, Downlink: 20}}, // no change → no call
	}}
	s.sampleOnce(q, time.Second, false)
	s.sampleOnce(q, time.Second, false)
	s.sampleOnce(q, time.Second, false)

	if len(got) != 2 {
		t.Fatalf("sink called %d times, want 2 (zero-delta tick must not call): %v", len(got), got)
	}
	if got[0]["alice"] != (UserDelta{Upload: 10, Download: 20}) {
		t.Fatalf("first delta = %+v", got[0])
	}
	if got[1]["alice"] != (UserDelta{Upload: 5, Download: 0}) {
		t.Fatalf("second delta = %+v", got[1])
	}
}

// TestSampleOnce_NilStore_StillFeedsSink locks the "runnable without SQLite"
// contract: a nil store skips the upserts but the sink still gets the deltas.
func TestSampleOnce_NilStore_StillFeedsSink(t *testing.T) {
	s := NewUserSampler(nil)
	var got map[string]UserDelta
	s.OnDeltas = func(d map[string]UserDelta) { got = d }

	q := &fakeUserQuerier{snaps: []map[string]v2stats.Counters{
		{"bob": {Uplink: 3, Downlink: 4}},
	}}
	if failed := s.sampleOnce(q, time.Second, false); failed {
		t.Fatalf("nil store must not fail the tick")
	}
	if got["bob"] != (UserDelta{Upload: 3, Download: 4}) {
		t.Fatalf("sink with nil store = %+v", got)
	}
}

// TestRun_NilStore_StillSamples proves Run no longer bails out when there is no
// SQLite store — the quota accounting must keep working without traffic.db.
func TestRun_NilStore_StillSamples(t *testing.T) {
	s := NewUserSampler(nil)
	done := make(chan map[string]UserDelta, 1)
	s.OnDeltas = func(d map[string]UserDelta) {
		select {
		case done <- d:
		default:
		}
	}
	q := &fakeUserQuerier{snaps: []map[string]v2stats.Counters{
		{"carol": {Uplink: 1, Downlink: 2}},
	}}
	stop := make(chan struct{})
	defer close(stop)
	go s.Run(q, 1, 0, stop)

	select {
	case d := <-done:
		if d["carol"] != (UserDelta{Upload: 1, Download: 2}) {
			t.Fatalf("delta = %+v", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("Run with a nil store never sampled")
	}
}
