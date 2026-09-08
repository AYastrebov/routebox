import type { TrafficSeriesPoint } from '$lib/types';

// seriesRates turns the sparse per-bucket byte series of /traffic/history into
// two dense B/s arrays over [startTs, endTs): one slot per `step` seconds,
// idle slots as 0, then merged into at most maxPoints slots so a day of minute
// buckets does not become a 1440-point path. Buckets outside the window are
// dropped. PURE.
export function seriesRates(
	rows: TrafficSeriesPoint[] | undefined,
	startTs: number,
	endTs: number,
	step: number,
	maxPoints: number
): { down: number[]; up: number[] } {
	if (step <= 0 || endTs <= startTs) return { down: [], up: [] };
	const slots = Math.ceil((endTs - startTs) / step);
	const merge = Math.max(1, Math.ceil(slots / maxPoints));
	const n = Math.ceil(slots / merge);
	const down = new Array<number>(n).fill(0);
	const up = new Array<number>(n).fill(0);
	for (const r of rows ?? []) {
		const i = Math.floor((r.ts - startTs) / step / merge);
		if (i < 0 || i >= n) continue;
		down[i] += r.download;
		up[i] += r.upload;
	}
	const secs = step * merge;
	return { down: down.map((b) => b / secs), up: up.map((b) => b / secs) };
}
