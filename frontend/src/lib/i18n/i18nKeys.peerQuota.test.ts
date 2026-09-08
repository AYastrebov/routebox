import { describe, it, expect } from 'vitest';
import en from './locales/en.json';
import ru from './locales/ru.json';
import { suspendLabelKey } from '../components/awg/peerQuota';

// Keys of the peer traffic quota (#95). A missing one renders the raw dotted
// path on the row that says whether a client still has traffic left.
const REQUIRED_KEYS = [
	'awg.quota',
	'awg.quotaGb',
	'awg.quotaGbPlaceholder',
	'awg.quotaNoLimit',
	'awg.quotaHint',
	'awg.quotaUsed',
	'awg.saveQuota',
	'awg.quotaSaved',
	'awg.quotaFailed',
	'awg.resetCounter',
	'awg.resetCounterConfirm',
	'awg.trafficReset',
	'awg.trafficResetFailed',
	'awg.transferCumulative',
	'awg.suspendCheckHint',
	'awg.suspendedQuota',
	'awg.suspendedExpired'
];

function lookup(obj: unknown, path: string): unknown {
	return path.split('.').reduce<unknown>((o, k) => (o && typeof o === 'object' ? (o as Record<string, unknown>)[k] : undefined), obj);
}

describe('i18n: peer quota keys', () => {
	for (const key of REQUIRED_KEYS) {
		it(`${key} exists in en and ru`, () => {
			expect(typeof lookup(en, key)).toBe('string');
			expect(typeof lookup(ru, key)).toBe('string');
		});
	}

	it('quotaUsed carries both placeholders in both locales', () => {
		for (const loc of [en, ru]) {
			expect(lookup(loc, 'awg.quotaUsed')).toContain('{used}');
			expect(lookup(loc, 'awg.quotaUsed')).toContain('{quota}');
		}
	});

	// The hint is the only place the operator is told the limit is not enforced
	// to the byte (spec Q5) — an edit that drops the interval breaks the promise.
	it('quotaHint names the 30 s tick in both locales', () => {
		expect(lookup(en, 'awg.quotaHint')).toContain('30 s');
		expect(lookup(ru, 'awg.quotaHint')).toContain('30 с');
	});

	// Same promise on the suspended badge's tooltip: a peer goes off within a
	// tick of spending its last byte, not on the byte.
	it('suspendCheckHint names the 30 s tick in both locales', () => {
		expect(lookup(en, 'awg.suspendCheckHint')).toContain('30 s');
		expect(lookup(ru, 'awg.suspendCheckHint')).toContain('30 с');
	});

	// Every key suspendLabelKey can return must resolve, including the fallback.
	it('every suspend label key resolves in both locales', () => {
		for (const reason of ['quota', 'expired', 'manual']) {
			const key = suspendLabelKey({ suspend_reason: reason });
			expect(typeof lookup(en, key)).toBe('string');
			expect(typeof lookup(ru, key)).toBe('string');
		}
	});
});
