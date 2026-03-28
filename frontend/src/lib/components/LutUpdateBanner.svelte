<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import type { WSMessage } from '$lib/api/types';
	import { _ } from '$lib/i18n';
	import { requestLutClientRefresh } from '$lib/stores/lutClientRefresh';

	const SNOOZE_MS = 5 * 60 * 1000;
	const SNOOZE_KEY = 'lutUpdatePromptSnoozeUntil';

	let visible = $state(false);
	let pendingModes = $state<string[]>([]);
	let snoozeChecked = $state(false);
	let updating = $state(false);
	let ws: WebSocket | null = null;

	function isSnoozed(): boolean {
		if (typeof localStorage === 'undefined') return false;
		const until = Number(localStorage.getItem(SNOOZE_KEY) || '0');
		return Number.isFinite(until) && Date.now() < until;
	}

	function rememberSnooze(): void {
		localStorage.setItem(SNOOZE_KEY, String(Date.now() + SNOOZE_MS));
	}

	function addPendingMode(mode: string): void {
		if (!mode) return;
		if (pendingModes.includes(mode)) return;
		pendingModes = [...pendingModes, mode];
		visible = true;
	}

	function hideAndClear(): void {
		visible = false;
		pendingModes = [];
		snoozeChecked = false;
	}

	function handleMessage(msg: WSMessage): void {
		if (msg.type !== 'lut_changed_on_disk') return;
		if (isSnoozed()) return;
		const payload = msg.payload as { mode?: string } | undefined;
		const mode = payload?.mode ?? msg.mode;
		if (mode) addPendingMode(mode);
	}

	function connectWebSocket(): void {
		const wsUrl = api.getWebSocketUrl();
		ws = new WebSocket(wsUrl);

		ws.onclose = () => {
			ws = null;
			setTimeout(connectWebSocket, 3000);
		};

		ws.onerror = () => {};

		ws.onmessage = (event) => {
			try {
				const msg = JSON.parse(event.data) as WSMessage;
				handleMessage(msg);
			} catch {
				// ignore
			}
		};
	}

	async function applyUpdate(): Promise<void> {
		if (pendingModes.length === 0) return;
		updating = true;
		try {
			const modes = [...pendingModes];
			for (const mode of modes) {
				await api.reloadLut(mode);
			}
			requestLutClientRefresh();
			hideAndClear();
		} catch (e) {
			console.error('LUT reload failed:', e);
		} finally {
			updating = false;
		}
	}

	function handleClose(): void {
		if (updating) return;
		if (snoozeChecked) rememberSnooze();
		hideAndClear();
	}

	function handleKeydown(event: KeyboardEvent): void {
		if (event.key !== 'Escape' || !visible) return;
		handleClose();
	}

	onMount(() => {
		connectWebSocket();
		return () => {
			if (ws) ws.close();
		};
	});
</script>

<svelte:window onkeydown={handleKeydown} />

{#if visible && pendingModes.length > 0}
	<div
		class="fixed right-4 top-4 z-[120] w-[calc(100vw-2rem)] max-w-sm"
		role="dialog"
		aria-modal="false"
		aria-labelledby="lut-update-title"
		aria-describedby="lut-update-description"
		aria-live="polite"
	>
		<div class="glass-panel relative overflow-hidden rounded-xl shadow-xl shadow-black/35">
			<div class="pointer-events-none absolute inset-0">
				<div class="absolute -right-8 top-0 h-16 w-16 rounded-full bg-[var(--color-cyan-glow)] blur-2xl opacity-80"></div>
				<div
					class="absolute inset-x-0 top-0 h-px bg-gradient-to-r from-transparent via-[var(--color-cyan)]/40 to-transparent"
				></div>
			</div>

			<div class="relative flex items-start gap-3 px-4 py-3.5">
				<div class="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-[var(--color-cyan)]/12">
					<svg
						class="h-4 w-4 text-[var(--color-cyan)]"
						fill="none"
						viewBox="0 0 24 24"
						stroke="currentColor"
						stroke-width="2"
						aria-hidden="true"
					>
						<path
							stroke-linecap="round"
							stroke-linejoin="round"
							d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"
						/>
					</svg>
				</div>

				<div class="min-w-0 flex-1">
					<div class="flex items-start justify-between gap-3">
						<div class="min-w-0">
							<h2
								id="lut-update-title"
								class="font-display text-sm tracking-wide text-[var(--color-light)]"
							>
								{$_('lutUpdate.title')}
							</h2>
							<p
								id="lut-update-description"
								class="mt-1 text-xs leading-5 text-[var(--color-mist)]"
							>
								{$_('lutUpdate.body')}
							</p>
						</div>

						<button
							type="button"
							class="rounded-md p-1.5 text-[var(--color-mist)] transition-colors hover:bg-white/10 hover:text-[var(--color-light)] disabled:cursor-not-allowed disabled:opacity-40"
							onclick={handleClose}
							disabled={updating}
							aria-label={$_('lutUpdate.close')}
						>
							<svg
								class="h-4 w-4"
								fill="none"
								viewBox="0 0 24 24"
								stroke="currentColor"
								stroke-width="2"
								aria-hidden="true"
							>
								<path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
							</svg>
						</button>
					</div>

					<div class="mt-2.5 flex items-start gap-2 text-xs">
						<span
							class="shrink-0 rounded-md bg-white/5 px-1.5 py-0.5 font-mono uppercase tracking-wide text-[var(--color-mist)]"
						>
							{$_('lutUpdate.modesLabel')}
						</span>
						<span class="min-w-0 break-words font-mono leading-5 text-[var(--color-light)]/90">
							{pendingModes.join(', ')}
						</span>
					</div>

					<label class="mt-3 flex cursor-pointer items-center gap-2 text-xs text-[var(--color-mist)]">
						<input
							type="checkbox"
							bind:checked={snoozeChecked}
							class="h-4 w-4 rounded border-white/10 bg-[var(--color-slate)] accent-[var(--color-cyan)] focus:ring-2 focus:ring-[var(--color-cyan-glow)] focus:ring-offset-0"
						/>
						<span>{$_('lutUpdate.snooze5min')}</span>
					</label>

					<div class="mt-3 flex flex-wrap items-center justify-end gap-2">
						<button
							type="button"
							class="rounded-lg border border-white/10 px-3 py-1.5 text-xs font-mono text-[var(--color-mist)] transition-colors hover:bg-white/10 hover:text-[var(--color-light)] disabled:cursor-not-allowed disabled:opacity-50"
							onclick={handleClose}
							disabled={updating}
						>
							{$_('lutUpdate.close')}
						</button>
						<button
							type="button"
							class="inline-flex items-center gap-2 rounded-lg bg-[var(--color-cyan)] px-3 py-1.5 text-xs font-mono font-semibold text-[var(--color-void)] transition-colors hover:bg-[var(--color-cyan)]/90 disabled:cursor-not-allowed disabled:opacity-50"
							onclick={applyUpdate}
							disabled={updating}
						>
							{#if updating}
								<svg class="h-3.5 w-3.5 animate-spin" fill="none" viewBox="0 0 24 24" aria-hidden="true">
									<circle
										class="opacity-25"
										cx="12"
										cy="12"
										r="10"
										stroke="currentColor"
										stroke-width="4"
									></circle>
									<path
										class="opacity-75"
										fill="currentColor"
										d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
									></path>
								</svg>
								<span>{$_('lutUpdate.updating')}</span>
							{:else}
								<svg
									class="h-3.5 w-3.5"
									fill="none"
									viewBox="0 0 24 24"
									stroke="currentColor"
									stroke-width="2"
									aria-hidden="true"
								>
									<path
										stroke-linecap="round"
										stroke-linejoin="round"
										d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"
									/>
								</svg>
								<span>{$_('lutUpdate.update')}</span>
							{/if}
						</button>
					</div>
				</div>
			</div>
		</div>
	</div>
{/if}
