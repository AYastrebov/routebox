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
 * GB (fractions allowed) -> whole bytes, for the API.
 *
 * Anything that is not a positive finite number — an empty field, NaN, a
 * negative — comes back as 0, and 0 on the wire means REMOVE THE LIMIT. That is
 * right for an empty field and wrong for a negative or for unparseable text, so
 * the caller must refuse those BEFORE calling (saveQuota/addPeer do): this
 * function cannot tell "the operator cleared the limit" from "the operator typed
 * something the browser could not read", and silently turning the second into
 * the first hands a client unlimited traffic under a success toast.
 */
export function gbToBytes(gb: number): number {
	if (!Number.isFinite(gb) || gb <= 0) return 0;
	return Math.round(gb * GB);
}

/**
 * Bytes -> GB rounded to one decimal. DISPLAY ONLY — never prefill an editable
 * field with this: 1.25 GB comes back as 1.3, and an untouched Save would then
 * write the rounded value over the stored one (a quota under 0.05 GB would come
 * back as 0, i.e. no limit at all). The quota row prefills with the raw
 * bytes / GB and compares in bytes.
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

/**
 * Bytes -> the number a GB input is PREFILLED with, or null for "no limit".
 *
 * Unlike bytesToGb this never changes the limit: it returns the SHORTEST GB
 * value that converts back to the same byte count, so an untouched Save writes
 * back exactly what was stored. The plain division cannot be shown as it is —
 * 0.1 GB is stored as 107374182 bytes (the half byte is rounded away) and
 * divides back to 0.099999999627471, which is what the field would then show.
 * Widening the precision one digit at a time and stopping at the first value
 * that round-trips gives 0.1 here and 1.3 for 1395864371, without ever handing
 * back a number that means a different quota.
 */
export function gbFieldValue(bytes: number): number | null {
	if (!Number.isFinite(bytes) || bytes <= 0) return null;
	const exact = bytes / GB;
	const target = Math.round(bytes);
	for (let digits = 1; digits <= 15; digits++) {
		const candidate = Number(exact.toPrecision(digits));
		if (gbToBytes(candidate) === target) return candidate;
	}
	// Unreachable for anything the API can store (a double survives 15 digits);
	// the exact quotient is still the safe answer, only an ugly one.
	return exact;
}

/** What is wrong with what the operator typed, or null when nothing is. */
export type QuotaInputProblem = 'invalid' | 'negative' | null;

/**
 * The guard every quota field runs BEFORE it may PATCH, because 0 on the wire
 * means REMOVE THE LIMIT.
 *
 * `badInput` is the element's `validity.badInput`: the binding alone cannot tell
 * the two ways of being empty apart — text the browser could not parse ("1,5" on
 * an English page in Firefox) binds as null, exactly like a cleared field. A
 * negative is the other way of accidentally meaning "unlimited", since gbToBytes
 * folds it to 0. An empty field and a typed 0 are real choices and pass.
 *
 * PURE: the caller turns the answer into its own toast.
 */
export function quotaInputProblem(badInput: boolean, gb: number | null): QuotaInputProblem {
	if (badInput) return 'invalid';
	if (gb !== null && gb < 0) return 'negative';
	return null;
}

/** Either "send these bytes" or "there is nothing to send". */
export type QuotaSavePlan = { skip: true } | { skip: false; bytes: number };

/**
 * What Save should do with the field's value against the stored limit.
 *
 * The comparison is in BYTES: the field is prefilled with the stored limit
 * divided by 1024^3, so comparing in GB would call an untouched field a change
 * and write the round-trip back over the stored value. Skipping means no request
 * at all — the operator who only came to look changes nothing.
 */
export function quotaSavePlan(gb: number | null, storedBytes: number): QuotaSavePlan {
	const bytes = gbToBytes(gb ?? 0);
	if (bytes === (storedBytes || 0)) return { skip: true };
	return { skip: false, bytes };
}
