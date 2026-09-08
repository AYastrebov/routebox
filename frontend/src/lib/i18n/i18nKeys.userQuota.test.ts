import { describe, it, expect } from 'vitest';
import en from './locales/en.json';
import ru from './locales/ru.json';
import { userSuspendLabelKey } from '../components/config/users/userQuota';

// Keys the panel-user quota UI (#95) renders. The quota vocabulary is the peer
// roster's, reused rather than copied — identical strings, one place to edit —
// so this file also pins the CROSS-SECTION use: renaming an awg.* key would
// otherwise leave the users page printing a raw dotted path.
const REQUIRED_KEYS = [
	'awg.quotaGb',
	'awg.quotaNoLimit',
	'awg.quotaHint',
	'awg.quotaUsed',
	'awg.quotaInvalid',
	'awg.quotaNegative',
	'awg.quotaUnchanged',
	'awg.saveQuota',
	'awg.quotaSaved',
	'awg.quotaFailed',
	'awg.resetCounter',
	'awg.resetCounterConfirm',
	'awg.trafficReset',
	'awg.trafficResetFailed',
	'awg.suspendCheckHint',
	'awg.suspendedQuota',
	'awg.suspended',
	'awg.transferCumulative',
	'users.active',
	'users.pending',
	'users.disabledLabel',
	'users.expired'
];

function lookup(obj: unknown, path: string): unknown {
	return path
		.split('.')
		.reduce<unknown>(
			(o, k) => (o && typeof o === 'object' ? (o as Record<string, unknown>)[k] : undefined),
			obj
		);
}

describe('i18n: panel-user quota keys', () => {
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

	// The field's tooltip is the only place the operator is told the limit is not
	// enforced to the byte (spec Q5); the badge's tooltip repeats it where the
	// suspension is announced. An edit that drops the interval breaks the promise.
	it('the quota hint and the suspended badge both name the 30 s tick', () => {
		for (const key of ['awg.quotaHint', 'awg.suspendCheckHint']) {
			expect(lookup(en, key)).toContain('30 s');
			expect(lookup(ru, key)).toContain('30 с');
		}
	});

	// Every key the badge can ask for must resolve, the unknown-reason fallback
	// included — the badge is what tells the operator which lever to pull.
	it('every user suspend label key resolves in both locales', () => {
		for (const reason of ['manual', 'quota', 'expired', 'martian']) {
			const key = userSuspendLabelKey({ suspend_reason: reason });
			expect(key).not.toBe('');
			expect(typeof lookup(en, key)).toBe('string');
			expect(typeof lookup(ru, key)).toBe('string');
		}
	});
});
