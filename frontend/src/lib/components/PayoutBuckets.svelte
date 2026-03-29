<script lang="ts">
	import type { PayoutBucket } from '$lib/api';
	import { _ } from '$lib/i18n';

	interface Props {
		buckets: PayoutBucket[];
	}

	let { buckets }: Props = $props();

	const MIN_VISIBLE_BAR_PX = 6;
	let barMetric = $state<'rtp' | 'hit'>('rtp');
	let maxRangeEnd = $derived(Math.max(...buckets.map((b) => b.range_end)));

	let hasRtpShare = $derived(
		buckets.length > 0 &&
			buckets.every(
				(b) => typeof b.rtp_contribution === 'number' && Number.isFinite(b.rtp_contribution)
			)
	);
	let totalRtpContribution = $derived(
		hasRtpShare ? buckets.reduce((s, b) => s + (b.rtp_contribution ?? 0), 0) : 0
	);
	let maxMetricValue = $derived(
		buckets.length > 0
			? Math.max(
					...buckets.map((b) => {
						if (barMetric === 'rtp' && hasRtpShare && totalRtpContribution > 0) {
							return Math.max(0, (b.rtp_contribution ?? 0) / totalRtpContribution);
						}
						return Math.max(0, b.probability);
					})
				)
			: 0
	);

	function formatRtpSharePercent(bucket: PayoutBucket): string {
		if (!hasRtpShare || totalRtpContribution <= 0) return '—';
		const p = ((bucket.rtp_contribution ?? 0) / totalRtpContribution) * 100;
		return p.toFixed(1) + '%';
	}

	function formatHitRatePercent(bucket: PayoutBucket): string {
		const p = bucket.probability * 100;
		if (!Number.isFinite(p) || p <= 0) return '0.0%';
		if (p < 0.01) return '<0.01%';
		if (p < 1) return p.toFixed(2) + '%';
		return p.toFixed(1) + '%';
	}

	function formatNumber(v: number): string {
		if (v >= 1_000_000) return (v / 1_000_000).toFixed(v % 1_000_000 === 0 ? 0 : 1) + 'M';
		if (v >= 1_000) return (v / 1_000).toFixed(v % 1_000 === 0 ? 0 : 1) + 'K';
		if (v >= 10) return v.toFixed(0);
		if (v >= 1) return v.toFixed(v % 1 === 0 ? 0 : 1);
		if (v > 0) return v.toFixed(2);
		return '0';
	}

	function formatRange(bucket: PayoutBucket, lossLabel: string): string {
		if (bucket.range_start === 0 && bucket.range_end === 0) {
			return lossLabel;
		}
		// Last bucket (extends to max)
		if (bucket.range_end >= maxRangeEnd * 0.99) {
			return `${formatNumber(bucket.range_start)}x+`;
		}
		return `${formatNumber(bucket.range_start)}x - ${formatNumber(bucket.range_end)}x`;
	}

	function formatOdds(probability: number): string {
		if (probability === 0) return '-';
		const odds = 1 / probability;
		if (odds >= 1_000_000) {
			return '1 in ' + (odds / 1_000_000).toFixed(1) + 'M';
		}
		if (odds >= 1_000) {
			return '1 in ' + (odds / 1_000).toFixed(1) + 'K';
		}
		if (odds >= 10) {
			return '1 in ' + odds.toFixed(0);
		}
		return '1 in ' + odds.toFixed(2);
	}

	function getBarWidth(bucket: PayoutBucket): string {
		const value =
			barMetric === 'rtp' && hasRtpShare && totalRtpContribution > 0
				? (bucket.rtp_contribution ?? 0) / totalRtpContribution
				: bucket.probability;
		if (!Number.isFinite(value) || value <= 0) return '0%';

		const base = maxMetricValue;
		if (!Number.isFinite(base) || base <= 0) return '0%';

		const widthPct = Math.min(100, (value / base) * 100);
		return `clamp(${MIN_VISIBLE_BAR_PX}px, ${widthPct.toFixed(3)}%, 100%)`;
	}

	function getBarColor(rangeStart: number): string {
		if (rangeStart === 0) return 'bg-gray-500';
		if (rangeStart < 1) return 'bg-blue-400';
		if (rangeStart < 5) return 'bg-green-400';
		if (rangeStart < 20) return 'bg-yellow-400';
		if (rangeStart < 100) return 'bg-orange-400';
		if (rangeStart < 1000) return 'bg-red-400';
		if (rangeStart < 10000) return 'bg-pink-500';
		return 'bg-purple-500';
	}
</script>

<div class="h-full flex flex-col">
	<div class="flex items-center gap-3 mb-6 shrink-0">
		<div class="w-1 h-5 bg-[var(--color-cyan)] rounded-full"></div>
		<h3 class="font-display text-lg text-[var(--color-light)] tracking-wider">{$_('distribution.title')}</h3>
		<div class="ml-auto inline-flex items-center rounded-md border border-slate-600/70 bg-slate-900/70 p-0.5">
			<button
				type="button"
				class="px-2 py-1 text-[10px] font-mono tracking-wide transition-colors rounded-sm {barMetric === 'rtp' ? 'bg-cyan-400/20' : ''}"
				class:text-cyan-200={barMetric === 'rtp'}
				class:text-slate-500={!hasRtpShare}
				class:text-slate-400={hasRtpShare && barMetric !== 'rtp'}
				disabled={!hasRtpShare}
				onclick={() => {
					if (hasRtpShare) barMetric = 'rtp';
				}}
			>
				RTP
			</button>
			<button
				type="button"
				class="px-2 py-1 text-[10px] font-mono tracking-wide transition-colors rounded-sm {barMetric === 'hit' ? 'bg-cyan-400/20' : ''}"
				class:text-cyan-200={barMetric === 'hit'}
				class:text-slate-400={barMetric !== 'hit'}
				onclick={() => (barMetric = 'hit')}
			>
				HIT RATE
			</button>
		</div>
	</div>

	{#if buckets.length === 0}
		<div class="py-8 text-center text-slate-500">{$_('status.noData')}</div>
	{:else}
		<div class="flex-1 overflow-y-auto pr-1 space-y-2 scrollbar-thin min-h-0">
			{#each buckets as bucket (`${bucket.range_start}:${bucket.range_end}`)}
				<div class="flex items-center gap-3 group">
					<div class="w-32 shrink-0 text-right text-sm text-slate-400 font-mono">
						{formatRange(bucket, $_('distribution.loss'))}
					</div>
					<div class="relative h-7 flex-1 overflow-hidden rounded-lg border border-slate-600/50 bg-slate-800/70">
						<div
							class="h-full transition-all duration-500 ease-out shadow-[inset_0_0_0_1px_rgba(255,255,255,0.15)] {getBarColor(bucket.range_start)} group-hover:opacity-95"
							style="width: {getBarWidth(bucket)}"
						></div>
						<div class="absolute inset-0 bg-gradient-to-r from-white/5 via-transparent to-white/10"></div>
					</div>
					<div
						class="w-14 shrink-0 text-right text-xs font-mono tabular-nums"
						title={barMetric === 'rtp' ? $_('distribution.rtpShareHint') : $_('table.hitRate')}
					>
						<span class="text-[var(--color-emerald)]">
							{barMetric === 'rtp' ? formatRtpSharePercent(bucket) : formatHitRatePercent(bucket)}
						</span>
					</div>
					<div class="w-24 shrink-0 text-right text-sm">
						<span class="text-white font-medium">{formatOdds(bucket.probability)}</span>
					</div>
					<div class="w-16 shrink-0 text-right text-xs text-slate-500 font-mono">
						{bucket.count.toLocaleString()}
					</div>
				</div>
			{/each}
		</div>

		<div class="mt-4 flex items-center justify-end gap-6 text-xs text-slate-500 shrink-0">
			<span
				class="w-14 text-right"
				title={barMetric === 'rtp' ? $_('distribution.rtpShareHint') : $_('table.hitRate')}
			>
				{barMetric === 'rtp' ? $_('distribution.rtpShare') : $_('metrics.hitRate')}
			</span>
			<span>{$_('table.odds')}</span>
			<span class="w-16 text-right">{$_('table.count')}</span>
		</div>
	{/if}
</div>
