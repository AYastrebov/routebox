// Peer traffic quota (#95). The panel enters a limit in GB and the API speaks
// bytes, so the conversion lives here — pure, next to peerExpiry, and pinned by
// tests: getting the unit wrong by 1000/1024 silently hands out 7% more or less
// traffic than the operator typed.

/** One GB as the panel means it: 1024^3 (spec Q10), matching formatBytes. */
export const GB = 1024 ** 3;

/** Peer fields the quota reads. Kept structural so tests need no full peer. */
export interface QuotaPeer {
	rx: number;
	tx: number;
	quota_bytes: number;
}

/**
 * GB (fractions allowed) -> whole bytes. Anything that is not a positive number
 * — empty input, NaN, a negative — is "no limit", i.e. 0: a negative is refused
 * by the API and must never be read here as "unlimited" by accident.
 */
export function gbToBytes(gb: number): number {
	if (!Number.isFinite(gb) || gb <= 0) return 0;
	return Math.round(gb * GB);
}

/**
 * Bytes -> GB rounded to one decimal, for the number input (step 0.1) and any
 * GB-shaped label. 0 stays 0 ("no limit").
 */
export function bytesToGb(bytes: number): number {
	if (!Number.isFinite(bytes) || bytes <= 0) return 0;
	return Math.round((bytes / GB) * 10) / 10;
}

export interface QuotaUsage {
	/** rx + tx, the sum the limit is spent against (spec Q2). */
	used: number;
	/** The limit itself; 0 = no limit, in which case the bar is not drawn. */
	quota: number;
	/** Bar width in percent, clamped to 0..100. 0 when there is no limit. */
	pct: number;
	/** Whether the allowance is spent — the same >= rule the backend suspends on. */
	exhausted: boolean;
}

/**
 * What the quota bar draws. Negative counters (a corrupt store, never a live
 * reading) are read as 0 rather than shrinking the bar below empty.
 */
export function quotaUsage(p: QuotaPeer): QuotaUsage {
	const rx = Math.max(0, p.rx || 0);
	const tx = Math.max(0, p.tx || 0);
	const used = rx + tx;
	const quota = Math.max(0, p.quota_bytes || 0);
	if (quota === 0) return { used, quota: 0, pct: 0, exhausted: false };
	// Round the width, not the verdict: 99.6% draws a full-looking bar but is
	// still in service, and `exhausted` is what colours it red.
	const pct = Math.min(100, Math.max(0, Math.round((used / quota) * 100)));
	return { used, quota, pct, exhausted: used >= quota };
}

/**
 * The i18n key naming the ONE reason a peer is out of service (spec Q18).
 * Empty string while it is in service — the caller draws no badge then.
 * `manual` cannot reach a peer (peers have no enable toggle), so an unknown
 * non-empty reason falls back to the plain "suspended" badge instead of
 * rendering a missing key.
 */
export function suspendLabelKey(p: { suspend_reason: string }): string {
	switch (p.suspend_reason) {
		case '':
			return '';
		case 'quota':
			return 'awg.suspendedQuota';
		case 'expired':
			return 'awg.suspendedExpired';
		default:
			return 'awg.suspended';
	}
}
