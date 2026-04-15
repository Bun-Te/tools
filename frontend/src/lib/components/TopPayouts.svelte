<script lang="ts">
	import type { AllModesComplianceResult, ComplianceCheck } from '$lib/api';
	import { _ } from '$lib/i18n';

	interface Props {
		mode: string | null;
		data: AllModesComplianceResult | null;
		ready: boolean;
	}

	let { mode, data, ready }: Props = $props();

	function rowTone(check: ComplianceCheck): string {
		if (check.passed) return 'text-slate-500';
		if (check.severity === 'error') return 'text-[var(--color-coral)]';
		if (check.severity === 'warning') return 'text-amber-400/85';
		return 'text-slate-400';
	}

	function titleClass(check: ComplianceCheck): string {
		if (check.passed) return 'text-[var(--color-light)]';
		return rowTone(check);
	}

	function valueClass(check: ComplianceCheck): string {
		if (check.passed) return 'text-emerald-400/90';
		return rowTone(check);
	}

	let modeResult = $derived(mode && data?.mode_results?.[mode] ? data.mode_results[mode] : null);
	let starTier = $derived(modeResult?.star_tier ?? 0);
	let tierLimits = $derived(modeResult?.tier_limits ?? null);

	function tierToneClass(tier: number): string {
		if (tier === 3) return 'text-emerald-400';
		if (tier === 2) return 'text-[var(--color-gold)]';
		if (tier === 1) return 'text-amber-400/85';
		return 'text-[var(--color-coral)]';
	}

	function formatUSD(n: number): string {
		if (n >= 1_000_000) return `$${(n / 1_000_000).toFixed(n % 1_000_000 === 0 ? 0 : 1)}M`;
		if (n >= 1_000) return `$${(n / 1_000).toFixed(n % 1_000 === 0 ? 0 : 1)}K`;
		return `$${n.toFixed(0)}`;
	}

	function formatMultiplier(n: number): string {
		if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}Mx`;
		if (n >= 1_000) return `${(n / 1_000).toFixed(n % 1_000 === 0 ? 0 : 1)}Kx`;
		return `${n}x`;
	}
</script>

<div class="h-full flex flex-col min-h-0">
	<div class="flex items-center gap-3 mb-4 shrink-0">
		<div class="w-1 h-5 bg-[var(--color-gold)] rounded-full"></div>
		<h3 class="font-display text-xl text-[var(--color-light)] tracking-wider flex-1 min-w-0">
			{$_('nav.compliance')}
		</h3>
		{#if modeResult}
			<span
				class="h-2 w-2 shrink-0 rounded-full {modeResult.passed ? 'bg-emerald-400' : 'bg-[var(--color-coral)]'}"
				title={modeResult.passed ? $_('compliance.allChecksPassed') : $_('compliance.complianceIssuesFound')}
				aria-label={modeResult.passed ? $_('compliance.allChecksPassed') : $_('compliance.complianceIssuesFound')}
			></span>
		{/if}
	</div>

	{#if !ready}
		<div class="py-8 text-center text-slate-500 text-sm">{$_('status.loading')}</div>
	{:else if !data}
		<div class="py-8 text-center text-slate-500 text-sm">{$_('status.failedToLoadCompliance')}</div>
	{:else if !mode}
		<div class="py-8 text-center text-slate-500 text-sm">{$_('status.selectMode')}</div>
	{:else if !modeResult}
		<div class="py-8 text-center text-slate-500 text-sm">{$_('status.noData')}</div>
	{:else}
		<div class="flex-1 min-h-0 max-h-[70vh] overflow-y-auto overscroll-contain pr-2 pb-2 -mr-1">
			<section aria-label={mode}>
				<div class="flex items-baseline justify-between gap-2 mb-4">
					<span class="text-xs font-mono uppercase tracking-widest text-slate-400 capitalize truncate min-w-0">
						{mode}
					</span>
					<span
						class="text-[11px] font-mono tabular-nums shrink-0 {modeResult.passed
							? 'text-emerald-500/90'
							: 'text-[var(--color-coral)]'}"
					>
						{modeResult.passed
							? `${modeResult.passed_count}/${modeResult.checks.length}`
							: `−${modeResult.failed_count}`}
					</span>
				</div>

				<div class="mb-4 rounded-xl border border-slate-700/35 bg-slate-900/25 p-3.5">
					<div class="flex items-baseline justify-between gap-3 mb-2">
						<span class="text-[11px] font-mono uppercase tracking-widest text-slate-500">
							{$_('compliance.starTierLabel')}
						</span>
						<span class="text-sm font-display tracking-wide {tierToneClass(starTier)}">
							{#if starTier === 0}
								{$_('compliance.starTierIneligible')}
							{:else}
								{$_('compliance.starTierStar', { values: { tier: starTier } })}
							{/if}
						</span>
					</div>
					<p class="text-[11px] leading-relaxed text-slate-500">
						{$_('compliance.starTierHint')}
					</p>
					{#if tierLimits}
						<dl class="mt-3 grid grid-cols-2 gap-x-4 gap-y-1.5 text-[11px] font-mono text-slate-500">
							<div class="flex justify-between col-span-2 sm:col-span-1">
								<dt>{$_('compliance.tierMaxExposure')}</dt>
								<dd class="text-slate-400">{formatUSD(tierLimits.max_exposure_usd)}</dd>
							</div>
							<div class="flex justify-between col-span-2 sm:col-span-1">
								<dt>{$_('compliance.tierMaxBet')}</dt>
								<dd class="text-slate-400">{formatUSD(tierLimits.max_single_bet_usd)}</dd>
							</div>
							<div class="flex justify-between col-span-2 sm:col-span-1">
								<dt>{$_('compliance.tierMaxMultiplier')}</dt>
								<dd class="text-slate-400">{formatMultiplier(tierLimits.max_payout_multiplier)}</dd>
							</div>
							<div class="flex justify-between col-span-2 sm:col-span-1">
								<dt>{$_('compliance.tierStdDevRange')}</dt>
								<dd class="text-slate-400">{tierLimits.std_dev_min}–{tierLimits.std_dev_max}</dd>
							</div>
						</dl>
					{/if}
				</div>

				<ul class="grid gap-3 xl:grid-cols-2">
					{#each modeResult.checks as check (check.id)}
						<li class="flex gap-3 rounded-xl border border-slate-700/35 bg-slate-900/20 p-3.5">
							<span class="mt-0.5 shrink-0" aria-hidden="true">
								{#if check.passed}
									<svg class="h-4 w-4 text-emerald-500/90" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.25">
										<path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />
									</svg>
								{:else}
									<svg class="h-4 w-4 opacity-90 {rowTone(check)}" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.25">
										<path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
									</svg>
								{/if}
							</span>
							<div class="min-w-0 flex-1 space-y-1.5">
								<div class="text-sm font-medium leading-snug {titleClass(check)}">
									{$_(check.nameKey)}
								</div>
								<p class="text-[13px] leading-relaxed text-slate-500">
									{$_(check.descriptionKey)}
								</p>
								<div class="text-[11px] leading-relaxed text-slate-600">
									<span class="font-mono">{$_('compliance.expected')}:</span>
									<span class="text-slate-500"> {check.expected}</span>
									<span class="mx-1.5 text-slate-700">→</span>
									<span class="font-mono">{$_('compliance.result')}:</span>
									<span class="font-medium {valueClass(check)}"> {check.value}</span>
								</div>
								{#if !check.passed && check.reasonKey}
									<p class="text-[12px] leading-relaxed border-l-2 border-slate-600/60 pl-2.5 {rowTone(check)}">
										{$_(check.reasonKey)}
									</p>
								{/if}
							</div>
						</li>
					{/each}
				</ul>
			</section>
		</div>
	{/if}
</div>
