import { describe, it, expect } from 'vitest';
import { GB } from '$lib/components/awg/peerQuota';
import {
	userQuotaUsage,
	userSuspendLabelKey,
	mergeQuotaFields,
	quotaDraftValue
} from './userQuota';
import type { PanelUser } from '$lib/types';

const row: PanelUser = {
	id: 'u1',
	name: 'alice',
	enabled: true,
	expires_at: 0,
	pending: false,
	bindings: [],
	upload: 7_000,
	download: 9_000,
	quota_bytes: 10 * GB,
	used_rx: 2 * GB,
	used_tx: 1 * GB,
	used_reset_at: 1_700_000_000,
	suspend_reason: ''
};

describe('userQuotaUsage', () => {
	it('spends the limit against rx + tx', () => {
		const u = userQuotaUsage(row);
		expect(u.used).toBe(3 * GB);
		expect(u.quota).toBe(10 * GB);
		expect(u.pct).toBe(30);
		expect(u.exhausted).toBe(false);
	});
	it('draws no bar without a limit', () => {
		expect(userQuotaUsage({ ...row, quota_bytes: 0 })).toMatchObject({ quota: 0, pct: 0 });
	});
	it('is exhausted at the limit, the same rule the backend suspends on', () => {
		expect(userQuotaUsage({ ...row, used_rx: 10 * GB, used_tx: 0 }).exhausted).toBe(true);
	});
	// A pending user is a draft credential with no registry entry behind it, so
	// the quota fields are simply absent from its row.
	it('survives a row with no counters at all', () => {
		expect(userQuotaUsage({})).toMatchObject({ used: 0, quota: 0, pct: 0, exhausted: false });
	});
});

describe('userSuspendLabelKey', () => {
	it('is empty while in service', () => expect(userSuspendLabelKey({ suspend_reason: '' })).toBe(''));
	it('names a manual disable', () =>
		expect(userSuspendLabelKey({ suspend_reason: 'manual' })).toBe('users.disabledLabel'));
	it('names the quota', () =>
		expect(userSuspendLabelKey({ suspend_reason: 'quota' })).toBe('awg.suspendedQuota'));
	it('names the date', () =>
		expect(userSuspendLabelKey({ suspend_reason: 'expired' })).toBe('users.expired'));
	it('falls back to the plain badge for a reason it does not know', () =>
		expect(userSuspendLabelKey({ suspend_reason: 'martian' })).toBe('awg.suspended'));
	it('draws no badge for a row without the field', () =>
		expect(userSuspendLabelKey({})).toBe(''));
});

describe('mergeQuotaFields', () => {
	// The point of the whole function (review of ticket 06): PATCH and reset
	// answer from the registry, where the SQLite history totals are zero.
	it('keeps the traffic column the answer does not know about', () => {
		const merged = mergeQuotaFields(row, {
			quota_bytes: 20 * GB,
			used_rx: 0,
			used_tx: 0,
			used_reset_at: 1_700_000_100,
			suspend_reason: ''
		});
		expect(merged.upload).toBe(7_000);
		expect(merged.download).toBe(9_000);
	});
	it('takes the quota fields from the answer, zeros included', () => {
		const merged = mergeQuotaFields(row, {
			quota_bytes: 0,
			used_rx: 0,
			used_tx: 0,
			used_reset_at: 1_700_000_100,
			suspend_reason: ''
		});
		expect(merged.quota_bytes).toBe(0);
		expect(merged.used_rx).toBe(0);
		expect(merged.used_tx).toBe(0);
		expect(merged.used_reset_at).toBe(1_700_000_100);
	});
	it('takes a new suspension reason', () => {
		expect(mergeQuotaFields(row, { suspend_reason: 'quota' }).suspend_reason).toBe('quota');
	});
	it('leaves everything else of the row alone', () => {
		const merged = mergeQuotaFields(row, { quota_bytes: 5 * GB });
		expect(merged.id).toBe('u1');
		expect(merged.name).toBe('alice');
		expect(merged.enabled).toBe(true);
		expect(merged.bindings).toBe(row.bindings);
		expect(merged.used_rx).toBe(2 * GB); // absent from the answer = unchanged
	});
	it('does not mutate the row it was given', () => {
		mergeQuotaFields(row, { quota_bytes: 5 * GB });
		expect(row.quota_bytes).toBe(10 * GB);
	});

	// The answer as the handler really builds it: a full user view off the
	// registry, where the SQLite history totals are zero and a deferred
	// enforcement rides along in `warning`.
	it('folds a real PATCH answer in without blanking the traffic column', () => {
		const answer: PanelUser = {
			...row,
			upload: 0,
			download: 0,
			quota_bytes: 20 * GB,
			used_rx: 4 * GB,
			used_tx: 1 * GB,
			used_reset_at: 1_700_000_100,
			suspend_reason: '',
			warning: 'the change is saved but not in force yet: draft pending'
		};
		const merged = mergeQuotaFields(row, answer);
		expect(merged.upload).toBe(7_000);
		expect(merged.download).toBe(9_000);
		expect(merged.quota_bytes).toBe(20 * GB);
		expect(merged.used_rx).toBe(4 * GB);
		// The warning describes THAT request and is shown as a notification; it
		// must not settle into the row and outlive it.
		expect(merged.warning).toBeUndefined();
	});

	// Raising the limit or resetting the counter puts the user back in service,
	// and the row has to stop saying "suspended" without a full reload.
	it('clears a suspension the answer says is over', () => {
		const suspended: PanelUser = { ...row, suspend_reason: 'quota' };
		const merged = mergeQuotaFields(suspended, {
			quota_bytes: 20 * GB,
			used_rx: 10 * GB,
			used_tx: 0,
			used_reset_at: 1_700_000_100,
			suspend_reason: ''
		});
		expect(merged.suspend_reason).toBe('');
	});
});

describe('quotaDraftValue', () => {
	// Any list reload (a toggle, an expiry, the apply bar) re-runs the prefill.
	it('fills an untouched field from the store', () => {
		expect(quotaDraftValue(5, undefined, undefined)).toBe(5);
	});
	it('takes a new stored value when the field still shows the old one', () => {
		expect(quotaDraftValue(9, 5, 5)).toBe(9);
	});
	// The one that matters: a half-typed limit must survive an unrelated reload.
	it('keeps what the operator typed', () => {
		expect(quotaDraftValue(5, 12, 5)).toBe(12);
	});
	it('keeps a field the operator cleared', () => {
		expect(quotaDraftValue(5, null, 5)).toBe(null);
	});
	it('fills a field that was cleared by the store and never touched', () => {
		expect(quotaDraftValue(null, null, null)).toBe(null);
	});
});
