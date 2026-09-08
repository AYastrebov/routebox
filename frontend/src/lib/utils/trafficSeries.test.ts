import { describe, it, expect } from 'vitest';
import { seriesRates } from './trafficSeries';

describe('seriesRates', () => {
	it('fills idle minutes with zero and converts bytes to B/s', () => {
		const { down, up } = seriesRates(
			[{ ts: 60, upload: 600, download: 6000 }, { ts: 180, upload: 0, download: 120 }],
			0, 240, 60, 1000
		);
		expect(down).toEqual([0, 100, 0, 2]);
		expect(up).toEqual([0, 10, 0, 0]);
	});

	it('merges neighbouring slots to stay under maxPoints', () => {
		// 4 minute slots, cap 2 → pairs of 2 minutes = 120 s each
		const { down } = seriesRates(
			[{ ts: 0, upload: 0, download: 1200 }, { ts: 60, upload: 0, download: 1200 }, { ts: 180, upload: 0, download: 240 }],
			0, 240, 60, 2
		);
		expect(down).toEqual([20, 2]);
	});

	it('drops buckets outside the window and copes with no rows', () => {
		expect(seriesRates([{ ts: -60, upload: 1, download: 1 }, { ts: 999, upload: 1, download: 1 }], 0, 120, 60, 10).down).toEqual([0, 0]);
		expect(seriesRates(undefined, 0, 120, 60, 10).up).toEqual([0, 0]);
		expect(seriesRates([], 0, 0, 60, 10)).toEqual({ down: [], up: [] });
	});
});
