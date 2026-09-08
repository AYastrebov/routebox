// sparklinePath builds an SVG path for values across [0,width]x[0,height], with
// the largest value at the top (y=0) and smallest at the bottom (y=height). A
// flat series renders at vertical center. Returns '' for <2 points. PURE.
export function sparklinePath(values: number[], width: number, height: number): string {
	if (values.length < 2) return '';
	const min = Math.min(...values);
	const max = Math.max(...values);
	const span = max - min;
	const stepX = width / (values.length - 1);
	const y = (v: number) => (span === 0 ? height / 2 : height - ((v - min) / span) * height);
	return values.map((v, i) => `${i === 0 ? 'M' : 'L'} ${i * stepX} ${y(v)}`).join(' ');
}

// areaPaths builds the line and the closed area under it for a series drawn
// against a FIXED max (baseline y=height, max at y=0) so several strips share a
// scale, or one strip keeps its scale while values stream in. Values above max
// clamp to the top. The line is a Catmull-Rom curve through the points (#99),
// with control points kept inside [0,height] so a lone spike does not dip the
// curve below the baseline or over the top. PURE.
export function areaPaths(
	values: number[],
	max: number,
	width: number,
	height: number
): { line: string; area: string } {
	if (values.length < 2) return { line: '', area: '' };
	const stepX = width / (values.length - 1);
	const y = (v: number) => (max <= 0 ? height : height - (Math.min(v, max) / max) * height);
	const pts = values.map((v, i) => ({ x: i * stepX, y: y(v) }));
	const at = (i: number) => pts[Math.max(0, Math.min(pts.length - 1, i))];
	const f = (n: number) => String(Math.round(n * 100) / 100);
	const cy = (n: number) => f(Math.max(0, Math.min(height, n)));
	let line = `M ${f(pts[0].x)} ${f(pts[0].y)}`;
	for (let i = 0; i < pts.length - 1; i++) {
		const [p0, p1, p2, p3] = [at(i - 1), at(i), at(i + 1), at(i + 2)];
		line += ` C ${f(p1.x + (p2.x - p0.x) / 6)} ${cy(p1.y + (p2.y - p0.y) / 6)}`;
		line += ` ${f(p2.x - (p3.x - p1.x) / 6)} ${cy(p2.y - (p3.y - p1.y) / 6)} ${f(p2.x)} ${f(p2.y)}`;
	}
	const area = `${line} L ${width} ${height} L 0 ${height} Z`;
	return { line, area };
}

// splitUnit separates "5.39 KB/s" into its number and unit so the number can
// be set large and the unit small. PURE.
export function splitUnit(s: string): { value: string; unit: string } {
	const i = s.indexOf(' ');
	if (i < 0) return { value: s, unit: '' };
	return { value: s.slice(0, i), unit: s.slice(i + 1) };
}
