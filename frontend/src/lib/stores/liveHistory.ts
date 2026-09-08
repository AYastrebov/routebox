// The dashboard's last-minute samples, kept at module level so leaving the page
// and coming back does not start every sparkline from zero (#99).
// ponytail: plain memory, filled only while the dashboard is open — a route
// change leaves a gap that is not drawn. A server-side time series (needed for
// the 24 h view anyway) replaces this.
export const liveHistory = {
	down: [] as number[],
	up: [] as number[],
	cpu: [] as number[],
	mem: [] as number[]
};
