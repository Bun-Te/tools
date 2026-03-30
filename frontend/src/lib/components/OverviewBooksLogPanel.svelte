<script lang="ts">
	import { api } from '$lib/api';
	import type { ModeSummary } from '$lib/api/types';

	type AllModesBooksLogResponse = {
		mode_count: number;
		log: string;
	};

	type ApiEnvelope = {
		success: boolean;
		data?: unknown;
		error?: string;
	};

	interface Props {
		modes: ModeSummary[];
	}

	let { modes }: Props = $props();

	const AVG_OPTIONS = [1, 2, 5, 10] as const;
	const TARGET_OPTIONS = [50, 100, 200, 500] as const;
	/** Fixed server-side band; not exposed in UI. */
	const DEFAULT_TOLERANCE_PCT = 12;

	let avgTarget = $state<number>(2);
	let targetWin = $state<number>(50);

	let copyLoading = $state(false);
	let previewLoading = $state(false);

	let previewLog = $state('');
	let statusMessage = $state<string | null>(null);
	let statusError = $state<string | null>(null);
	let previewError = $state<string | null>(null);

	let previewSeq = 0;

	function scheduleStatusClear() {
		window.setTimeout(() => {
			statusMessage = null;
			statusError = null;
		}, 5000);
	}

	function queryParamsSnapshot() {
		let avg = avgTarget;
		let tw = targetWin;
		if (!Number.isFinite(avg) || avg <= 0) avg = 2;
		if (!Number.isFinite(tw) || tw <= 0) tw = 50;
		return new URLSearchParams({
			avg_target: String(avg),
			target_win: String(tw),
			tolerance_pct: String(DEFAULT_TOLERANCE_PCT)
		});
	}

	async function fetchBooksLog(endpoint: string) {
		const response = await fetch(`${api.getBaseUrl()}${endpoint}`);
		const envelope = (await response.json()) as ApiEnvelope;
		if (!envelope.success || !envelope.data) {
			throw new Error(envelope.error || 'Unknown error');
		}
		return envelope.data;
	}

	$effect(() => {
		previewSeq++;
		const mySeq = previewSeq;
		modes.length;
		avgTarget;
		targetWin;

		if (modes.length === 0) {
			previewLog = '';
			previewLoading = false;
			previewError = null;
			return;
		}

		const timer = window.setTimeout(() => {
			void (async () => {
				previewLoading = true;
				previewError = null;
				try {
					const params = queryParamsSnapshot();
					const data = (await fetchBooksLog(
						`/api/books-log?${params.toString()}`
					)) as AllModesBooksLogResponse;
					if (mySeq !== previewSeq) return;
					previewLog = data.log;
				} catch (e) {
					if (mySeq !== previewSeq) return;
					previewError = e instanceof Error ? e.message : 'Preview failed';
					previewLog = '';
				} finally {
					if (mySeq === previewSeq) previewLoading = false;
				}
			})();
		}, 380);

		return () => window.clearTimeout(timer);
	});

	async function copyLog() {
		statusMessage = null;
		statusError = null;
		if (modes.length === 0) {
			statusError = 'No modes available';
			scheduleStatusClear();
			return;
		}

		copyLoading = true;
		try {
			const params = queryParamsSnapshot();
			const response = (await fetchBooksLog(
				`/api/books-log?${params.toString()}`
			)) as AllModesBooksLogResponse;
			previewLog = response.log;
			previewError = null;
			await navigator.clipboard.writeText(response.log);
			statusMessage = `Copied (${response.mode_count} mode(s))`;
		} catch (e) {
			statusError = e instanceof Error ? e.message : 'Failed to copy';
		} finally {
			copyLoading = false;
		}
		scheduleStatusClear();
	}

	function presetBtnClass(active: boolean) {
		const base =
			'px-2 py-1 rounded-lg text-[10px] font-mono tabular-nums border transition-all duration-150 shrink-0 leading-none';
		return active
			? `${base} border-[var(--color-cyan)]/45 bg-[linear-gradient(180deg,rgba(34,211,238,0.18),rgba(34,211,238,0.08))] text-[var(--color-cyan)] shadow-[inset_0_0_0_1px_rgba(34,211,238,0.15)]`
			: `${base} border-white/[0.07] bg-[var(--color-graphite)]/45 text-[var(--color-mist)] hover:border-[var(--color-cyan)]/22 hover:text-[var(--color-light)]`;
	}
</script>

<div class="glass-panel rounded-2xl p-3 sm:p-4">
	<div class="flex items-center gap-3 mb-4">
		<div class="w-1 h-5 bg-[var(--color-violet)] rounded-full"></div>
		<h3 class="font-display text-lg text-[var(--color-light)] tracking-wider">Event finder</h3>
		{#if previewLoading}
			<span class="text-[10px] font-mono text-[var(--color-mist)]/80 ml-auto">Loading...</span>
		{/if}
	</div>

	<div class="grid gap-2 sm:grid-cols-2 mb-3">
		<div class="min-w-0">
			<div class="flex items-center gap-2 mb-1">
				<input
					type="number"
					step="0.1"
					min="0.1"
					bind:value={avgTarget}
					class="w-[4.5rem] h-6 rounded-md border border-white/[0.1] bg-[var(--color-graphite)]/60 px-1.5 py-0 text-[10px] font-mono text-[var(--color-light)] tabular-nums outline-none focus:border-[var(--color-cyan)]/40"
					aria-label="Average target multiplier"
				/>
				<span class="text-[10px] font-mono text-[var(--color-mist)]">Avg (x)</span>
			</div>
			<div class="flex flex-wrap gap-1">
				{#each AVG_OPTIONS as v (v)}
					<button
						type="button"
						class={presetBtnClass(avgTarget === v)}
						onclick={() => {
							avgTarget = v;
						}}>{v}x</button
					>
				{/each}
			</div>
		</div>

		<div class="min-w-0">
			<div class="flex items-center gap-2 mb-1">
				<input
					type="number"
					step="1"
					min="1"
					bind:value={targetWin}
					class="w-[4.5rem] h-6 rounded-md border border-white/[0.1] bg-[var(--color-graphite)]/60 px-1.5 py-0 text-[10px] font-mono text-[var(--color-light)] tabular-nums outline-none focus:border-[var(--color-cyan)]/40"
					aria-label="Target win multiplier"
				/>
				<span class="text-[10px] font-mono text-[var(--color-mist)]">Target (x)</span>
			</div>
			<div class="flex flex-wrap gap-1">
				{#each TARGET_OPTIONS as v (v)}
					<button
						type="button"
						class={presetBtnClass(targetWin === v)}
						onclick={() => {
							targetWin = v;
						}}>{v}x</button
					>
				{/each}
			</div>
		</div>
	</div>

	<div class="mb-2">
		<textarea
			readonly
			rows="13"
			bind:value={previewLog}
			class="w-full min-h-[10.5rem] rounded-lg border border-white/[0.08] bg-[var(--color-graphite)]/50 px-2.5 py-2 font-mono text-[11px] sm:text-xs text-[var(--color-light)] resize-y outline-none focus:border-[var(--color-cyan)]/35"
			aria-label="All modes event finder log"
		></textarea>
	</div>

	<div class="flex items-center gap-2 mb-1">
		<button
			type="button"
			class="px-3 py-1.5 rounded-lg text-[11px] font-mono tracking-wide border border-[var(--color-cyan)]/35 text-[var(--color-cyan)] bg-[var(--color-cyan)]/10 hover:bg-[var(--color-cyan)]/20 transition-colors disabled:opacity-40 disabled:pointer-events-none inline-flex items-center gap-1.5"
			onclick={() => void copyLog()}
			disabled={copyLoading || modes.length === 0}
		>
			{#if copyLoading}
				<svg class="w-3 h-3 animate-spin shrink-0" fill="none" viewBox="0 0 24 24" aria-hidden="true">
					<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"
					></circle>
					<path
						class="opacity-75"
						fill="currentColor"
						d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
					></path>
				</svg>
			{/if}
			Copy
		</button>
	</div>

	{#if previewError}
		<p class="text-[11px] font-mono text-[var(--color-coral)]">{previewError}</p>
	{/if}
	{#if statusMessage}
		<p class="text-[11px] font-mono text-emerald-400/90">{statusMessage}</p>
	{/if}
	{#if statusError}
		<p class="text-[11px] font-mono text-[var(--color-coral)]">{statusError}</p>
	{/if}
</div>
