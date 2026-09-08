package users

import (
	"os"
	"path/filepath"
	"testing"
)

// seedUsageManager builds a persisted manager holding two users: "alice" with an
// extra binding name, and "bob" with only its own name.
func seedUsageManager(t *testing.T) (*Manager, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "users.toml")
	m := NewManager(path)
	alice := &PanelUser{
		ID: "u-alice", Name: "alice", Enabled: true,
		Bindings: []Binding{
			{InboundTag: "vless-in", Credential: "c1", Name: "alice"},
			{InboundTag: "trojan-in", Credential: "c2", Name: "alice-trojan"},
		},
	}
	bob := &PanelUser{ID: "u-bob", Name: "bob", Enabled: true}
	if err := m.Put(alice); err != nil {
		t.Fatalf("Put alice: %v", err)
	}
	if err := m.Put(bob); err != nil {
		t.Fatalf("Put bob: %v", err)
	}
	return m, path
}

// TestAddUsage_SumsAcrossBindings is the ticket's first required test: sampler
// deltas land in Used* summed over every name a user answers to (own name plus
// each binding name), and unrelated names are ignored.
func TestAddUsage_SumsAcrossBindings(t *testing.T) {
	m, _ := seedUsageManager(t)

	changed, err := m.AddUsage(map[string]struct{ Up, Down int64 }{
		"alice":        {Up: 10, Down: 100},
		"alice-trojan": {Up: 5, Down: 50},
		"bob":          {Up: 1, Down: 2},
		"stranger":     {Up: 999, Down: 999},
	})
	if err != nil {
		t.Fatalf("AddUsage: %v", err)
	}
	if !changed {
		t.Fatalf("AddUsage reported no change, want changed")
	}

	alice, _ := m.Get("u-alice")
	if alice.UsedTx != 15 || alice.UsedRx != 150 {
		t.Fatalf("alice used = tx %d / rx %d, want tx 15 / rx 150", alice.UsedTx, alice.UsedRx)
	}
	bob, _ := m.Get("u-bob")
	if bob.UsedTx != 1 || bob.UsedRx != 2 {
		t.Fatalf("bob used = tx %d / rx %d, want tx 1 / rx 2", bob.UsedTx, bob.UsedRx)
	}

	// A second batch accumulates on top rather than replacing.
	if _, err := m.AddUsage(map[string]struct{ Up, Down int64 }{"alice": {Up: 1, Down: 1}}); err != nil {
		t.Fatalf("AddUsage 2: %v", err)
	}
	alice, _ = m.Get("u-alice")
	if alice.UsedTx != 16 || alice.UsedRx != 151 {
		t.Fatalf("after second batch alice used = tx %d / rx %d, want tx 16 / rx 151", alice.UsedTx, alice.UsedRx)
	}
}

// TestAddUsage_PersistsAndSkipsUnchanged proves the counters survive a reload and
// that a batch touching nobody (or carrying only zeros) writes nothing.
func TestAddUsage_PersistsAndSkipsUnchanged(t *testing.T) {
	m, path := seedUsageManager(t)
	if _, err := m.AddUsage(map[string]struct{ Up, Down int64 }{"alice": {Up: 7, Down: 9}}); err != nil {
		t.Fatalf("AddUsage: %v", err)
	}

	reloaded := NewManager(path)
	if err := reloaded.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	alice, ok := reloaded.Get("u-alice")
	if !ok {
		t.Fatalf("alice missing after reload")
	}
	if alice.UsedTx != 7 || alice.UsedRx != 9 {
		t.Fatalf("reloaded alice used = tx %d / rx %d, want tx 7 / rx 9", alice.UsedTx, alice.UsedRx)
	}

	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	changed, err := m.AddUsage(map[string]struct{ Up, Down int64 }{
		"stranger": {Up: 5, Down: 5},
		"alice":    {Up: 0, Down: 0},
	})
	if err != nil {
		t.Fatalf("AddUsage no-op: %v", err)
	}
	if changed {
		t.Fatalf("AddUsage with nothing to attribute reported changed")
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat 2: %v", err)
	}
	if !after.ModTime().Equal(before.ModTime()) || after.Size() != before.Size() {
		t.Fatalf("no-op AddUsage rewrote the file")
	}

	// An empty batch is also a no-op and must not error.
	if changed, err := m.AddUsage(nil); err != nil || changed {
		t.Fatalf("AddUsage(nil) = %v, %v; want false, nil", changed, err)
	}
}

// TestResetUsage zeroes both counters and stamps UsedResetAt, persisting.
func TestResetUsage(t *testing.T) {
	m, path := seedUsageManager(t)
	if _, err := m.AddUsage(map[string]struct{ Up, Down int64 }{"alice": {Up: 7, Down: 9}}); err != nil {
		t.Fatalf("AddUsage: %v", err)
	}
	if err := m.ResetUsage("u-alice", 1700000000); err != nil {
		t.Fatalf("ResetUsage: %v", err)
	}
	alice, _ := m.Get("u-alice")
	if alice.UsedTx != 0 || alice.UsedRx != 0 || alice.UsedResetAt != 1700000000 {
		t.Fatalf("after reset alice = tx %d / rx %d / at %d, want 0/0/1700000000",
			alice.UsedTx, alice.UsedRx, alice.UsedResetAt)
	}

	reloaded := NewManager(path)
	if err := reloaded.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if a, _ := reloaded.Get("u-alice"); a.UsedResetAt != 1700000000 || a.UsedRx != 0 {
		t.Fatalf("reset not persisted: %+v", a)
	}

	if err := m.ResetUsage("nope", 1); err == nil {
		t.Fatalf("ResetUsage on an unknown id must error")
	}
}

// TestLoadLegacyUsersFile_ZeroQuotaAndUsage is the ticket's "old users file reads
// (zeros)" test: a file written before quotas existed loads cleanly with every
// new field at its zero value, i.e. no limit and nothing used.
func TestLoadLegacyUsersFile_ZeroQuotaAndUsage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.toml")
	doc := `
[[users]]
id = "legacy1"
name = "old"
enabled = true
expires_at = 0
token = "tok"
token_disabled = false

  [[users.bindings]]
  inbound_tag = "vless-in"
  credential = "cred"
  protocol = "vless"
  name = "old"
  flow = ""
`
	if err := os.WriteFile(path, []byte(doc), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	m := NewManager(path)
	if err := m.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	u, ok := m.Get("legacy1")
	if !ok {
		t.Fatalf("legacy user not loaded")
	}
	if u.QuotaBytes != 0 || u.UsedRx != 0 || u.UsedTx != 0 || u.UsedResetAt != 0 {
		t.Fatalf("legacy user must read zeros, got quota=%d rx=%d tx=%d at=%d",
			u.QuotaBytes, u.UsedRx, u.UsedTx, u.UsedResetAt)
	}
	if !IsEffectivelyActive(u, 1700000000) {
		t.Fatalf("legacy user with zero quota must stay active")
	}
}
