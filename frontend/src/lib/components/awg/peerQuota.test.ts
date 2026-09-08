import { describe, it, expect } from 'vitest';
import {
	GB,
	gbToBytes,
	bytesToGb,
	gbFieldValue,
	quotaInputProblem,
	quotaSavePlan,
	quotaUsage,
	suspendLabelKey
} from './peerQuota';

describe('gbToBytes', () => {
	it('uses 1024^3, not 1000^3', () => expect(gbToBytes(1)).toBe(1_073_741_824));
	it('accepts fractions', () => expect(gbToBytes(0.5)).toBe(GB / 2));
	it('rounds to whole bytes, down when the fraction is below a half', () =>
		expect(gbToBytes(0.3)).toBe(322_122_547)); // 0.3 * 1024^3 = 322122547.2
	it('reads 0 as no limit', () => expect(gbToBytes(0)).toBe(0));
	it('reads NaN (empty input) as no limit', () => expect(gbToBytes(NaN)).toBe(0));
	it('never turns a negative into a limit', () => expect(gbToBytes(-5)).toBe(0));
});

describe('bytesToGb', () => {
	it('round-trips a whole GB', () => expect(bytesToGb(gbToBytes(10))).toBe(10));
	it('rounds to one decimal', () => expect(bytesToGb(1.5678 * GB)).toBe(1.6));
	// The reason it is display-only: 1.24 GB comes back as 1.2, and prefilling an
	// editable field with that would rewrite the stored limit on an untouched Save.
	it('rounds down at .04', () => expect(bytesToGb(1.24 * GB)).toBe(1.2));
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
	// The bar rounds, the verdict does not: a peer 4 bytes short of its limit
	// draws a full bar and is still in service.
	it('draws a full bar just under the limit without calling it exhausted', () => {
		const u = quotaUsage({ rx: 996, tx: 0, quota_bytes: 1000 });
		expect(u.pct).toBe(100);
		expect(u.exhausted).toBe(false);
	});
	it('rounds a fractional percentage to the nearest whole', () => {
		expect(quotaUsage({ rx: 2, tx: 0, quota_bytes: 3 }).pct).toBe(67);
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

describe('gbFieldValue', () => {
	// The reason it exists: the stored byte count is rounded, so the plain
	// division has noise in it and the field would show that noise.
	it('shows the round number the operator typed', () => {
		expect(gbToBytes(0.1) / GB).not.toBe(0.1); // guards the premise, not the helper
		expect(gbFieldValue(gbToBytes(0.1))).toBe(0.1);
		expect(gbFieldValue(gbToBytes(1.3))).toBe(1.3);
	});
	it('round-trips whatever an operator can type', () => {
		for (const gb of [0.1, 0.25, 1, 1.5, 2.7, 10, 512.25, 0.003]) {
			expect(gbFieldValue(gbToBytes(gb))).toBe(gb);
		}
	});
	// The property the prefill rests on: an untouched Save sends back the same
	// byte count it was filled from, so it cannot rewrite the stored limit.
	it('survives the trip back to bytes unchanged', () => {
		for (const bytes of [1, 1024, 107_374_182, 1_395_864_371, 7 * GB + 13]) {
			expect(gbToBytes(gbFieldValue(bytes) as number)).toBe(bytes);
		}
	});
	it('shows an empty field for no limit', () => {
		expect(gbFieldValue(0)).toBe(null);
		expect(gbFieldValue(-1)).toBe(null);
		expect(gbFieldValue(NaN)).toBe(null);
	});
});

// The two guards every quota field needs before it may PATCH. They were written
// twice (peer roster, users page) and pinned nowhere; here they are pinned once,
// and the pages only decide which toast to show.
describe('quotaInputProblem', () => {
	it('passes a real number through', () => expect(quotaInputProblem(false, 2.5)).toBe(null));
	it('passes an empty field through — that is "no limit", a real choice', () =>
		expect(quotaInputProblem(false, null)).toBe(null));
	it('passes a typed zero through — also "no limit"', () =>
		expect(quotaInputProblem(false, 0)).toBe(null));
	// The browser could not parse what is in the field ("1,5" on an English page
	// in Firefox): the binding says null, exactly like a cleared field, and a
	// cleared field means "remove the limit".
	it('catches unreadable input, which binds as null', () =>
		expect(quotaInputProblem(true, null)).toBe('invalid'));
	it('reports unreadable input before anything else', () =>
		expect(quotaInputProblem(true, -3)).toBe('invalid'));
	it('catches a negative, which would otherwise convert to "no limit"', () =>
		expect(quotaInputProblem(false, -0.5)).toBe('negative'));
});

describe('quotaSavePlan', () => {
	it('converts GB to whole bytes', () =>
		expect(quotaSavePlan(2, 0)).toEqual({ skip: false, bytes: 2 * GB }));
	it('reads an empty field as removing the limit', () =>
		expect(quotaSavePlan(null, 5 * GB)).toEqual({ skip: false, bytes: 0 }));
	// Compared in BYTES, not in GB: the field shows the stored limit divided by
	// 1024^3, and an operator who only came to look must not have it rewritten.
	it('skips the request when the value did not change', () =>
		expect(quotaSavePlan(1.3, gbToBytes(1.3))).toEqual({ skip: true }));
	it('skips an empty field on a peer that has no limit', () =>
		expect(quotaSavePlan(null, 0)).toEqual({ skip: true }));
	it('does not skip a real change', () =>
		expect(quotaSavePlan(1.3, gbToBytes(1.2))).toEqual({ skip: false, bytes: gbToBytes(1.3) }));
	// What the prefill exists for: filling from the store and saving untouched is
	// not a write at all.
	it('skips a value that came from gbFieldValue untouched', () => {
		const stored = 1_395_864_371;
		expect(quotaSavePlan(gbFieldValue(stored), stored)).toEqual({ skip: true });
	});
});
