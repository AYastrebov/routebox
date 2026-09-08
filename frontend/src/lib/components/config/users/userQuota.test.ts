import { describe, it, expect } from 'vitest';
import { GB } from '$lib/components/awg/peerQuota';
import { userQuotaUsage, userSuspendLabelKey, mergeQuotaFields } from './userQuota';
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
});
