import { writable, get } from 'svelte/store';

export type SpeedUnit = 'bits' | 'bytes';

export const speedUnit = writable<SpeedUnit>('bytes');

export function formatBytes(bytes: number): string {
	if (bytes === 0) return '0 B';
	const k = 1024;
	const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
	// Fractions of a byte (an hour's average of one short burst) have a
	// negative log — clamp, or the unit reads "undefined" (#99).
	const i = Math.max(0, Math.min(sizes.length - 1, Math.floor(Math.log(bytes) / Math.log(k))));
	return `${(bytes / Math.pow(k, i)).toFixed(1)} ${sizes[i]}`;
}

export function formatSpeed(bytesPerSec: number): string {
	const unit = get(speedUnit);
	if (unit === 'bits') {
		const bits = bytesPerSec * 8;
		if (bits === 0) return '0 bps';
		const k = 1000;
		const sizes = ['bps', 'Kbps', 'Mbps', 'Gbps', 'Tbps'];
		const i = Math.max(0, Math.min(sizes.length - 1, Math.floor(Math.log(bits) / Math.log(k))));
		return `${(bits / Math.pow(k, i)).toFixed(1)} ${sizes[i]}`;
	}
	return `${formatBytes(bytesPerSec)}/s`;
}
