<script lang="ts">
	import { onDestroy } from 'svelte';
	import { Chart, registerables } from 'chart.js';
	import type { Scale } from 'chart.js';
	import type { PayoutBucket } from '$lib/api';
	import { _ } from '$lib/i18n';

	Chart.register(...registerables);

	let { buckets, formatRange }: { buckets: PayoutBucket[]; formatRange: (b: PayoutBucket) => string } = $props();

	let canvasRef = $state<HTMLCanvasElement | null>(null);
	let chart: Chart | null = null;
	let useLogScale = $state(true);

	function barColor(rangeStart: number): string {
		if (rangeStart === 0) return 'rgba(107, 114, 128, 0.9)';
		if (rangeStart < 1) return 'rgba(96, 165, 250, 0.9)';
		if (rangeStart < 5) return 'rgba(74, 222, 128, 0.9)';
		if (rangeStart < 20) return 'rgba(250, 204, 21, 0.9)';
		if (rangeStart < 100) return 'rgba(251, 146, 60, 0.9)';
		if (rangeStart < 1000) return 'rgba(248, 113, 113, 0.9)';
		if (rangeStart < 10000) return 'rgba(236, 72, 153, 0.9)';
		return 'rgba(192, 132, 252, 0.9)';
	}

	function barBorderColor(rangeStart: number): string {
		if (rangeStart === 0) return 'rgb(156, 163, 175)';
		if (rangeStart < 1) return 'rgb(147, 197, 253)';
		if (rangeStart < 5) return 'rgb(134, 239, 172)';
		if (rangeStart < 20) return 'rgb(253, 224, 71)';
		if (rangeStart < 100) return 'rgb(253, 186, 116)';
		if (rangeStart < 1000) return 'rgb(252, 165, 165)';
		if (rangeStart < 10000) return 'rgb(244, 114, 182)';
		return 'rgb(216, 180, 254)';
	}

	/** Log axis: Chart.js default ticks are too dense; use powers of 10 only. */
	function applySparseLogYTicks(axis: Scale) {
		const minV = axis.min;
		const maxV = axis.max;
		if (typeof minV !== 'number' || typeof maxV !== 'number') return;
		if (!Number.isFinite(minV) || !Number.isFinite(maxV) || minV <= 0 || maxV <= 0) return;

		const minExp = Math.ceil(Math.log10(minV) - 1e-12);
		const maxExp = Math.floor(Math.log10(maxV) + 1e-12);
		const out: { value: number; major: boolean; significand: number }[] = [];

		if (minExp > maxExp) {
			out.push({ value: minV, major: true, significand: 0 });
			if (maxV !== minV) out.push({ value: maxV, major: true, significand: 0 });
			axis.ticks = out;
			return;
		}

		for (let e = minExp; e <= maxExp; e++) {
			const v = Math.pow(10, e);
			if (v >= minV * (1 - 1e-10) && v <= maxV * (1 + 1e-10)) {
				out.push({ value: v, major: true, significand: 0 });
			}
		}

		if (out.length === 0) {
			out.push({ value: minV, major: true, significand: 0 });
			if (maxV !== minV) out.push({ value: maxV, major: true, significand: 0 });
		}

		axis.ticks = out;
	}

	function buildSeries() {
		if (!buckets?.length) {
			return { labels: [] as string[], counts: [] as number[], rangeStarts: [] as number[], total: 0 };
		}
		const ordered = [...buckets].sort((a, b) => a.range_start - b.range_start);
		const total = ordered.reduce((s, b) => s + b.count, 0);
		return {
			labels: ordered.map((b) => formatRange(b)),
			counts: ordered.map((b) => b.count),
			rangeStarts: ordered.map((b) => b.range_start),
			total
		};
	}

	function createChart() {
		if (!canvasRef) return;

		const { labels, counts, rangeStarts, total } = buildSeries();
		if (labels.length === 0) return;

		if (chart) {
			chart.destroy();
			chart = null;
		}

		const ctx = canvasRef.getContext('2d');
		if (!ctx) return;

		const displayCounts = useLogScale
			? counts.map((c) => (c > 0 ? c : null))
			: counts;

		const booksLabel = $_('table.books');

		// Chart.js canvas does not resolve CSS variables — use theme hex from layout.css
		const tickColor = '#c8c8d8';
		const tooltipTitleColor = '#f0f0f5';
		const tooltipBodyColor = '#c8c8d8';

		chart = new Chart(ctx, {
			type: 'bar',
			data: {
				labels,
				datasets: [
					{
						label: booksLabel,
						data: displayCounts,
						backgroundColor: rangeStarts.map(barColor),
						borderColor: rangeStarts.map(barBorderColor),
						borderWidth: 1,
						borderRadius: 6,
						borderSkipped: false
					}
				]
			},
			options: {
				responsive: true,
				maintainAspectRatio: false,
				animation: {
					duration: 520,
					easing: 'easeOutQuart'
				},
				interaction: {
					mode: 'index',
					intersect: false
				},
				plugins: {
					legend: { display: false },
					tooltip: {
						backgroundColor: 'rgba(26, 26, 31, 0.96)',
						titleColor: tooltipTitleColor,
						bodyColor: tooltipBodyColor,
						borderColor: 'rgba(200, 200, 216, 0.25)',
						borderWidth: 1,
						padding: 12,
						cornerRadius: 8,
						titleFont: { family: 'ui-monospace, monospace', size: 12, weight: 'bold' },
						bodyFont: { family: 'ui-monospace, monospace', size: 11 },
						callbacks: {
							label: (context) => {
								const idx = context.dataIndex;
								const raw = counts[idx] ?? 0;
								const pct = total > 0 ? ((raw / total) * 100).toFixed(2) : '0';
								return [
									`${booksLabel}: ${raw.toLocaleString()}`,
									$_('distribution.shareOfBooks', { values: { value: pct } })
								];
							}
						}
					}
				},
				scales: {
					x: {
						grid: { display: false },
						ticks: {
							color: tickColor,
							font: { family: 'ui-monospace, monospace', size: 9 },
							maxRotation: 52,
							minRotation: 0,
							autoSkip: true,
							maxTicksLimit: 24
						}
					},
					y: {
						type: useLogScale ? 'logarithmic' : 'linear',
						min: useLogScale ? undefined : 0,
						...(useLogScale ? { afterBuildTicks: (axis: Scale) => applySparseLogYTicks(axis) } : {}),
						grid: { color: 'rgba(148, 163, 184, 0.09)' },
						border: { display: false },
						ticks: {
							color: tickColor,
							font: { family: 'ui-monospace, monospace', size: 10 },
							callback: (val) => {
								if (typeof val !== 'number') return String(val);
								if (useLogScale && val < 1 && val > 0) return val.toFixed(2);
								if (val >= 1_000_000) return (val / 1_000_000).toFixed(val % 1_000_000 === 0 ? 0 : 1) + 'M';
								if (val >= 1_000) return (val / 1_000).toFixed(val % 1_000 === 0 ? 0 : 1) + 'K';
								return val.toLocaleString();
							}
						}
					}
				}
			}
		});
	}

	onDestroy(() => {
		if (chart) {
			chart.destroy();
			chart = null;
		}
	});

	$effect(() => {
		void buckets;
		void formatRange;
		void useLogScale;
		if (canvasRef) {
			createChart();
		}
	});
</script>

<div
	class="rounded-xl border border-slate-700/50 bg-slate-800/30 overflow-hidden mb-8"
	aria-label={$_('distribution.chartOverview')}
>
	<div class="flex flex-wrap items-center justify-between gap-3 px-4 py-3 border-b border-slate-700/40">
		<div class="flex items-center gap-2">
			<div class="w-1 h-4 rounded-full bg-[var(--color-cyan)]/80"></div>
			<h4 class="font-mono text-sm text-[var(--color-light)] tracking-wide">
				{$_('distribution.chartOverview')}
			</h4>
		</div>
		<label class="flex cursor-pointer items-center gap-2 text-xs font-mono text-[var(--color-mist)] select-none">
			<input
				type="checkbox"
				bind:checked={useLogScale}
				class="h-3 w-3 rounded border-white/20 bg-[var(--color-graphite)] text-[var(--color-cyan)] focus:ring-[var(--color-cyan)]/40"
			/>
			{$_('distribution.logScale')}
		</label>
	</div>
	<div class="relative h-[min(22rem,50vh)] w-full px-2 pb-3 pt-2">
		<canvas bind:this={canvasRef} class="max-h-full"></canvas>
	</div>
</div>
