package users

import "fmt"

// AddUsage attributes a batch of per-inbound-user byte deltas to the panel users
// that answer to those names, accumulating into UsedTx (upload) / UsedRx
// (download), and persists ONCE if anything moved.
//
// The key is the inbound user NAME the sampler saw, not a panel-user id: one
// panel user can hold several credentials under different names (userNames(u) =
// its own Name plus every binding Name), so every matching name is summed into
// the same user — that is what makes the quota a per-USER limit rather than a
// per-credential one. Names nobody claims are dropped (a credential outside the
// registry has no quota to spend).
//
// Returns changed=false and writes nothing when the batch attributes zero bytes,
// so the 30s ticker does not rewrite users.toml on an idle server (spec Q7: the
// write is atomic and only on change). A write failure rolls the in-memory
// counters back so memory never diverges from disk — the deltas of that one tick
// are lost, which is the same "counters stay flat" behaviour a failed sample has.
func (m *Manager) AddUsage(deltas map[string]struct{ Up, Down int64 }) (bool, error) {
	if len(deltas) == 0 {
		return false, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	type snapshot struct {
		u      *PanelUser
		rx, tx int64
	}
	var undo []snapshot
	changed := false
	for _, u := range m.byID {
		var up, down int64
		for _, name := range userNames(*u) {
			d, ok := deltas[name]
			if !ok {
				continue
			}
			up += d.Up
			down += d.Down
		}
		if up <= 0 && down <= 0 {
			continue
		}
		undo = append(undo, snapshot{u: u, rx: u.UsedRx, tx: u.UsedTx})
		if up > 0 {
			u.UsedTx += up
		}
		if down > 0 {
			u.UsedRx += down
		}
		changed = true
	}
	if !changed {
		return false, nil
	}
	if err := m.saveLocked(); err != nil {
		for _, s := range undo {
			s.u.UsedRx, s.u.UsedTx = s.rx, s.tx
		}
		return false, err
	}
	return true, nil
}

// ResetUsage zeroes a user's accumulated counters and stamps UsedResetAt (unix
// seconds), persisting. Spec Q9: this is the "Сбросить счётчик" button, and per
// Q19 it re-admits a quota-suspended user the moment the caller re-syncs the
// reject rule. Unknown id is an error (the caller has a 404 to return); a failed
// write rolls the counters back.
func (m *Manager) ResetUsage(id string, now int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.byID[id]
	if !ok {
		return fmt.Errorf("user %q not found", id)
	}
	prevRx, prevTx, prevAt := u.UsedRx, u.UsedTx, u.UsedResetAt
	u.UsedRx, u.UsedTx, u.UsedResetAt = 0, 0, now
	if err := m.saveLocked(); err != nil {
		u.UsedRx, u.UsedTx, u.UsedResetAt = prevRx, prevTx, prevAt
		return err
	}
	return nil
}
