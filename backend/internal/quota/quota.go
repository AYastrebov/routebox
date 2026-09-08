// Package quota decides, from a limit and a pair of cumulative byte counters,
// whether a peer or a panel user is suspended and why.
//
// It is deliberately pure and dependency-free: the AWG peer store and the panel
// user store hold the same four numbers (quota, used rx, used tx, expiry) and
// must reach the SAME verdict from them, or the panel would show one entity
// suspended for a reason the other would not recognise. Both call in here.
package quota

// Reason is the single reason an entity is suspended, or ReasonNone when it is
// in service. It is a string so it can travel to the API/UI verbatim.
type Reason string

const (
	// ReasonNone means the entity is in service.
	ReasonNone Reason = ""
	// ReasonManual is an operator switching the entity off by hand.
	ReasonManual Reason = "manual"
	// ReasonQuota is the traffic limit being used up.
	ReasonQuota Reason = "quota"
	// ReasonExpired is the validity date having passed.
	ReasonExpired Reason = "expired"
)

// Exhausted reports whether used rx+tx has reached a non-zero quota.
//
// quotaBytes <= 0 means "no limit" (0 is how peers.toml and the user store spell
// it; a negative value cannot be entered and is read the same way rather than
// suspending everyone on a corrupt file). The comparison is >=, so a peer that
// has spent exactly its limit is done: the tick granularity is 30s, so the last
// sample is always a little over anyway, and treating "exactly" as "still has
// room" would let a 0-remaining peer keep running until the next byte.
//
// The sum is compared without ever forming it, so counters near math.MaxInt64
// (a corrupt store, or a live counter read as unsigned) cannot wrap negative and
// report an exhausted peer as having room left.
func Exhausted(quotaBytes, usedRx, usedTx int64) bool {
	if quotaBytes <= 0 {
		return false
	}
	if usedRx < 0 {
		usedRx = 0
	}
	if usedTx < 0 {
		usedTx = 0
	}
	return usedRx >= quotaBytes || usedTx >= quotaBytes-usedRx
}

// State returns the single suspension reason by priority manual → quota →
// expired → none (spec Q18: one reason, so the row shows the operator the thing
// they have to undo first).
//
// enabled=false means manually disabled — panel users have that toggle, AWG
// peers do not, so peer callers pass true.
//
// expiresAt 0 means "never". The boundary is strict — now >= expiresAt is
// expired — matching the awg semantics this replaces (SweepExpired, peerLines,
// the sing-box render).
func State(quotaBytes, usedRx, usedTx int64, enabled bool, expiresAt, now int64) Reason {
	switch {
	case !enabled:
		return ReasonManual
	case Exhausted(quotaBytes, usedRx, usedTx):
		return ReasonQuota
	case expiresAt != 0 && now >= expiresAt:
		return ReasonExpired
	}
	return ReasonNone
}
