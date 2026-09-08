import { describe, it, expect } from 'vitest';
import { sparklinePath } from './sparkline';

describe('sparklinePath', () => {
	it('returns empty string for <2 points', () => {
		expect(sparklinePath([], 100, 20)).toBe('');
		expect(sparklinePath([5], 100, 20)).toBe('');
	});

	it('maps a flat series to a horizontal line at mid-height', () => {
		const d = sparklinePath([4, 4, 4], 100, 20);
		expect(d.startsWith('M 0 10')).toBe(true);
		expect(d).toContain('L 100 10');
	});

	it('maps min to bottom and max to top', () => {
		// width=100,height=20: first(0)→y=20(bottom), last(10)→y=0(top)
		expect(sparklinePath([0, 10], 100, 20)).toBe('M 0 20 L 100 0');
	});
});

import { areaPaths, splitUnit } from './sparkline';

describe('areaPaths', () => {
	it('returns empty paths for <2 points', () => {
		expect(areaPaths([5], 10, 100, 20)).toEqual({ line: '', area: '' });
	});

	// Every "C c1x c1y c2x c2y x y" segment ends on a data point; the control
	// points in between are what make the curve.
	const anchors = (line: string) =>
		line.split(' C ').slice(1).map((seg) => seg.split(' ').slice(4).map(Number));

	it('scales against a fixed max with the baseline at the bottom', () => {
		// max=10: 0→bottom(20), 10→top(0), 5→middle(10)
		const { line, area } = areaPaths([0, 10, 5], 10, 100, 20);
		expect(line.startsWith('M 0 20 C ')).toBe(true);
		expect(anchors(line)).toEqual([[50, 0], [100, 10]]);
		expect(area).toBe(`${line} L 100 20 L 0 20 Z`);
	});

	it('never draws above the top when a value exceeds max', () => {
		const { line } = areaPaths([0, 20], 10, 100, 20);
		expect(anchors(line)).toEqual([[100, 0]]);
	});

	it('treats max 0 as flat baseline', () => {
		expect(areaPaths([0, 0], 0, 100, 20).line).toBe('M 0 20 C 16.67 20 83.33 20 100 20');
	});

	it('keeps the curve inside the box around a lone spike (#99)', () => {
		const { line } = areaPaths([0, 0, 10, 0, 0], 10, 100, 20);
		const ys = line.match(/-?[\d.]+/g)!.map(Number).filter((_, i) => i % 2 === 1);
		expect(Math.min(...ys)).toBeGreaterThanOrEqual(0);
		expect(Math.max(...ys)).toBeLessThanOrEqual(20);
	});
});

describe('splitUnit', () => {
	it('separates the number from its unit', () => {
		expect(splitUnit('5.39 KB/s')).toEqual({ value: '5.39', unit: 'KB/s' });
		expect(splitUnit('18.90 GB')).toEqual({ value: '18.90', unit: 'GB' });
		expect(splitUnit('0 B')).toEqual({ value: '0', unit: 'B' });
	});
	it('returns the whole string as value when there is no unit', () => {
		expect(splitUnit('42')).toEqual({ value: '42', unit: '' });
	});
});
