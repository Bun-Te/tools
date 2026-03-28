<script lang="ts">
	import type { CompareItem } from '$lib/api';
	import { _ } from '$lib/i18n';

	interface Props {
		items: CompareItem[];
	}

	let { items }: Props = $props();

	function formatPercent(value: number): string {
		return (value * 100).toFixed(2) + '%';
	}

	function formatMultiplier(value: number): string {
		return value.toFixed(2) + 'x';
	}

	function formatDecimal(value: number): string {
		return value.toFixed(2);
	}

	/** Expected spins to once: p → `1 in 13.1M` style (one decimal for K/M/B). */
	function formatOneInOdds(probability: number): string {
		if (!Number.isFinite(probability) || probability <= 0) return '—';
		const n = 1 / probability;
		if (!Number.isFinite(n)) return '—';
		if (n >= 1_000_000_000) return `1 in ${(n / 1_000_000_000).toFixed(1)}B`;
		if (n >= 1_000_000) return `1 in ${(n / 1_000_000).toFixed(1)}M`;
		if (n >= 1_000) return `1 in ${(n / 1_000).toFixed(1)}K`;
		if (n >= 10) return `1 in ${Math.round(n).toLocaleString()}`;
		return `1 in ${n.toFixed(2)}`;
	}

	/** RTP on fixed 90%–98% band: column aligns for quick row compare vs absolute range. */
	function rtpBandFraction(rtp: number): number {
		return Math.max(0, Math.min(1, (rtp - 0.9) / 0.08));
	}

	let maxPayoutRange = $derived.by(() => {
		if (items.length === 0) return { min: 0, max: 1 };
		const vals = items.map((i) => i.max_payout);
		return { min: Math.min(...vals), max: Math.max(...vals) };
	});

	/** Max win fill: position within min–max across loaded modes (row compare). */
	function maxWinBandFraction(payout: number): number {
		const { min, max } = maxPayoutRange;
		if (max <= min) return 1;
		return Math.max(0, Math.min(1, (payout - min) / (max - min)));
	}

	function getVolatilityLabel(item: CompareItem): { label: string; color: string; displayValue: number } {
		// For bonus modes (cost > 1), use breakeven rate for classification
		if (item.cost > 1) {
			const br = item.breakeven_rate;
			if (br >= 0.50) return { label: 'Low', color: 'text-green-400', displayValue: item.cost_adj_volatility };
			if (br >= 0.30) return { label: 'Medium', color: 'text-yellow-400', displayValue: item.cost_adj_volatility };
			if (br >= 0.15) return { label: 'High', color: 'text-orange-400', displayValue: item.cost_adj_volatility };
			return { label: 'Extreme', color: 'text-red-400', displayValue: item.cost_adj_volatility };
		}
		// For base mode, use traditional CV-based classification
		const vol = item.volatility;
		if (vol < 3) return { label: 'Low', color: 'text-green-400', displayValue: vol };
		if (vol < 7) return { label: 'Medium', color: 'text-yellow-400', displayValue: vol };
		if (vol < 15) return { label: 'High', color: 'text-orange-400', displayValue: vol };
		return { label: 'Very High', color: 'text-red-400', displayValue: vol };
	}
</script>

<div>
	<div class="flex items-center gap-3 mb-6">
		<div class="w-1 h-5 bg-[var(--color-violet)] rounded-full"></div>
		<h3 class="font-display text-lg text-[var(--color-light)] tracking-wider">{$_('modeComparison.title')}</h3>
	</div>

	{#if items.length === 0}
		<div class="py-8 text-center text-slate-500">{$_('status.noData')}</div>
	{:else}
		<div class="overflow-x-auto">
			<table class="w-full">
				<thead>
					<tr class="text-left text-xs uppercase text-slate-500 tracking-wider">
						<th class="pb-3 font-medium">{$_('table.mode')}</th>
						<th class="pb-3 font-medium">
							<div class="ml-auto w-16 text-right">{$_('metrics.rtp')}</div>
						</th>
						<th class="pb-3 font-medium">
							<div class="ml-auto w-16 text-right leading-tight">{$_('metrics.hitRate')}</div>
						</th>
						<th class="pb-3 font-medium">
							<div class="ml-auto max-w-[7rem] text-right leading-tight text-[10px] normal-case">{$_('modeComparison.oddsAnyWin')}</div>
						</th>
						<th class="pb-3 font-medium">
							<div class="ml-auto w-28 text-right leading-tight">{$_('metrics.maxWin')}</div>
						</th>
						<th class="pb-3 font-medium">
							<div
								class="ml-auto max-w-[5.5rem] text-right leading-tight tracking-wide"
								title={$_('modeComparison.maxWinHitRateHint')}
							>
								{$_('modeComparison.maxWinHitRate')}
							</div>
						</th>
						<th class="pb-3 text-right font-medium">{$_('metrics.volatility')}</th>
					</tr>
				</thead>
				<tbody class="text-sm">
					{#each items as item}
						{@const vol = getVolatilityLabel(item)}
						<tr class="border-t border-slate-700/50 hover:bg-slate-700/20 transition-colors">
							<td class="py-3">
								<span class="font-semibold text-white capitalize">{item.mode}</span>
								{#if item.cost > 1}
									<span class="ml-1.5 text-[10px] px-1.5 py-0.5 rounded bg-violet-500/20 text-violet-400">{item.cost}x</span>
								{/if}
							</td>
							<td class="py-3 align-middle">
								<!-- Fixed-width stack + ml-auto: avoid inline-flex baseline drift between rows -->
								<div class="ml-auto flex w-16 flex-col gap-0.5">
									<div
										class="h-1 w-full rounded-full bg-slate-700/90 overflow-hidden shrink-0"
										title="RTP 90%–98%"
									>
										<div
											class="h-full rounded-full bg-emerald-500/85 min-w-px transition-[width] duration-300"
											style="width: {rtpBandFraction(item.rtp) * 100}%"
										></div>
									</div>
									<span class="block w-full text-right font-mono text-[11px] text-emerald-400 tabular-nums leading-none">{formatPercent(item.rtp)}</span>
								</div>
							</td>
							<td class="py-3 align-middle">
								<div class="ml-auto flex w-16 flex-col gap-0.5">
									<div
										class="h-1 w-full rounded-full bg-slate-700/90 overflow-hidden shrink-0"
										title="0–100% hit rate"
									>
										<div
											class="h-full min-w-px rounded-full bg-sky-400/90 transition-[width] duration-300"
											style="width: {Math.max(0, Math.min(1, item.hit_rate)) * 100}%"
										></div>
									</div>
									<span class="block w-full text-right font-mono text-[11px] text-sky-400 tabular-nums leading-none">{formatPercent(item.hit_rate)}</span>
								</div>
							</td>
							<td class="py-3 align-middle">
								<div class="ml-auto max-w-[7rem] text-right" title={$_('modeComparison.oddsAnyWin')}>
									<span class="font-mono text-[10px] leading-snug text-slate-400 tabular-nums">{formatOneInOdds(item.hit_rate)}</span>
								</div>
							</td>
							<td class="py-3 align-middle">
								<div class="ml-auto flex w-28 flex-col gap-0.5">
									<div
										class="h-1 w-full rounded-full bg-slate-700/90 overflow-hidden shrink-0"
										title="Max win vs min–max in this table"
									>
										<div
											class="h-full min-w-px rounded-full bg-amber-400/90 transition-[width] duration-300"
											style="width: {maxWinBandFraction(item.max_payout) * 100}%"
										></div>
									</div>
									<span class="block w-full text-right font-mono text-[11px] font-semibold text-amber-400 tabular-nums leading-none">{formatMultiplier(item.max_payout)}</span>
								</div>
							</td>
							<td class="py-3 align-middle">
								<div class="ml-auto max-w-[6.5rem] text-right" title={$_('modeComparison.maxWinHitRateHint')}>
									<span class="font-mono text-[10px] leading-snug text-slate-400 tabular-nums">{formatOneInOdds(item.max_win_hit_rate ?? 0)}</span>
								</div>
							</td>
							<td class="py-3 text-right">
								<span class="inline-flex items-center gap-1.5">
									<span class="{vol.color} font-mono font-medium">{formatDecimal(vol.displayValue)}</span>
									<span class="text-[10px] px-1.5 py-0.5 rounded-full bg-slate-700 text-slate-400">{vol.label}</span>
									{#if item.cost > 1}
										<span class="text-[9px] text-slate-500">({(item.breakeven_rate * 100).toFixed(0)}% BE)</span>
									{/if}
								</span>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
</div>
