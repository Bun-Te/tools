import { writable } from 'svelte/store';

/** Increment after a confirmed LUT reload so the main page can refresh stats and charts. */
export const lutClientRefreshRequest = writable(0);

export function requestLutClientRefresh(): void {
	lutClientRefreshRequest.update((n) => n + 1);
}
