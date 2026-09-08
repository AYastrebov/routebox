import { describe, it, expect } from 'vitest';
import { GB, gbToBytes, bytesToGb, quotaUsage, suspendLabelKey } from './peerQuota';

describe('gbToBytes', () => {
	it('uses 1024^3, not 1000^3', () => expect(gbToBytes(1)).toBe(1_073_741_824));
	it('accepts fractions', () => expect(gbToBytes(0.5)).toBe(GB / 2));
	it('rounds to whole bytes', () => expect(Number.isInteger(gbToBytes(0.3))).toBe(true));
	it('reads 0 as no limit', () => expect(gbToBytes(0)).toBe(0));
	it('reads NaN (empty input) as no limit', () => expect(gbToBytes(NaN)).toBe(0));
	it('never turns a negative into a limit', () => expect(gbToBytes(-5)).toBe(0));
});

describe('bytesToGb', () => {
	it('round-trips a whole GB', () => expect(bytesToGb(gbToBytes(10))).toBe(10));
	it('rounds to one decimal', () => expect(bytesToGb(1.5678 * GB)).toBe(1.6));
	it('keeps 0 as 0', () => expect(bytesToGb(0)).toBe(0));
	it('reads a negative as 0', () => expect(bytesToGb(-1)).toBe(0));
});

describe('quotaUsage', () => {
	it('sums rx and tx against the limit', () => {
		expect(quotaUsage({ rx: 2 * GB, tx: 3 * GB, quota_bytes: 10 * GB })).toEqual({
			used: 5 * GB,
			quota: 10 * GB,
			pct: 50,
			exhausted: false
		});
	});
	it('reports no limit as quota 0 and draws nothing', () => {
		const u = quotaUsage({ rx: GB, tx: GB, quota_bytes: 0 });
		expect(u.quota).toBe(0);
		expect(u.pct).toBe(0);
		expect(u.exhausted).toBe(false);
		expect(u.used).toBe(2 * GB);
	});
	it('is exhausted exactly at the limit', () => {
		expect(quotaUsage({ rx: 5 * GB, tx: 5 * GB, quota_bytes: 10 * GB }).exhausted).toBe(true);
	});
	it('clamps the bar at 100% when over', () => {
		const u = quotaUsage({ rx: 30 * GB, tx: 0, quota_bytes: 10 * GB });
		expect(u.pct).toBe(100);
		expect(u.exhausted).toBe(true);
	});
	it('treats negative counters as zero', () => {
		expect(quotaUsage({ rx: -100, tx: -100, quota_bytes: GB })).toMatchObject({ used: 0, pct: 0 });
	});
});

describe('suspendLabelKey', () => {
	it('is empty while in service', () => expect(suspendLabelKey({ suspend_reason: '' })).toBe(''));
	it('names the quota', () =>
		expect(suspendLabelKey({ suspend_reason: 'quota' })).toBe('awg.suspendedQuota'));
	it('names the date', () =>
		expect(suspendLabelKey({ suspend_reason: 'expired' })).toBe('awg.suspendedExpired'));
	it('falls back to the plain badge for an unknown reason', () =>
		expect(suspendLabelKey({ suspend_reason: 'manual' })).toBe('awg.suspended'));
});
