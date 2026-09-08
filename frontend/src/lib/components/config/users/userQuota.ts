// Panel-user traffic quota (#95), the pure half of it. The peer roster already
// has the GB math and the bar arithmetic (awg/peerQuota.ts) — a user's counters
// simply live under different field names, so this file maps the shapes and
// keeps the two pages on ONE implementation of "how full is the allowance".

import { quotaUsage, type QuotaUsage } from '$lib/components/awg/peerQuota';
import type { PanelUser, SuspendReason } from '$lib/types';

/** The quota half of a user row: what a bar and a merge need, nothing else. */
export interface UserQuotaFields {
	quota_bytes: number;
	used_rx: number;
	used_tx: number;
	used_reset_at: number;
	suspend_reason: SuspendReason;
}

/**
 * The user's allowance, in the peer roster's terms: rx/tx there, used_rx/used_tx
 * here, the same rx + tx sum spent against the same limit (spec Q2). Tolerates
 * missing counters because a PENDING user (a draft credential with no registry
 * entry) carries none.
 */
export function userQuotaUsage(u: Partial<UserQuotaFields>): QuotaUsage {
	return quotaUsage({
		rx: u.used_rx ?? 0,
		tx: u.used_tx ?? 0,
		quota_bytes: u.quota_bytes ?? 0
	});
}

/**
 * The i18n key naming the ONE reason a user is out of service (spec Q18).
 * Empty while in service — the caller draws the "active" badge then.
 *
 * The quota vocabulary is shared with the peer roster on purpose: the strings
 * are identical, and one text edited in one place is one text everywhere. Only
 * `manual` is user-only (peers have no enable toggle), and it reuses the badge
 * the page already had for a disabled user.
 */
export function userSuspendLabelKey(u: { suspend_reason?: string }): string {
	switch (u.suspend_reason) {
		case '':
		case undefined:
			return '';
		case 'manual':
			return 'users.disabledLabel';
		case 'quota':
			return 'awg.suspendedQuota';
		case 'expired':
			return 'users.expired';
		default:
			return 'awg.suspended';
	}
}

/**
 * Fold a PATCH / traffic-reset answer back into the row it came from.
 *
 * ONLY the quota fields travel. Those answers are built from the registry alone
 * and carry `upload`/`download` as 0 — those two are the SQLite traffic history
 * that only the list query fills in — so a wholesale replace would blank the
 * traffic column of the row the operator just edited (review of ticket 06).
 * `warning` is deliberately left behind too: it describes THAT request, and the
 * page shows it as a notification, not as row state.
 */
export function mergeQuotaFields(row: PanelUser, updated: Partial<UserQuotaFields>): PanelUser {
	return {
		...row,
		// ?? and not ||: 0 is a real answer everywhere here — a cleared limit, a
		// reset counter — and must overwrite what the row had.
		quota_bytes: updated.quota_bytes ?? row.quota_bytes,
		used_rx: updated.used_rx ?? row.used_rx,
		used_tx: updated.used_tx ?? row.used_tx,
		used_reset_at: updated.used_reset_at ?? row.used_reset_at,
		suspend_reason: updated.suspend_reason ?? row.suspend_reason
	};
}

/**
 * What a row's quota field should show after the list was re-fetched.
 *
 * Every reload re-runs the prefill — a toggle, an expiry change, an added
 * binding and the apply/discard bar all call load() — and a prefill that simply
 * overwrote the field would throw away a limit the operator was in the middle of
 * typing. So the stored value only wins while the field still shows what the
 * PREVIOUS prefill put there (`prefilled`): the moment the two differ, the
 * difference is the operator's and it stays.
 *
 * `current`/`prefilled` are undefined for a row seen for the first time.
 */
export function quotaDraftValue(
	stored: number | null,
	current: number | null | undefined,
	prefilled: number | null | undefined
): number | null {
	if (current === undefined || current === prefilled) return stored;
	return current;
}
