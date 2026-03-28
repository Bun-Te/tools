// See https://svelte.dev/docs/kit/types#app.d.ts
// for information about these interfaces
declare global {
	/** Injected in vite.config.ts at build time (package.json version) */
	var __BUILD_VERSION__: string;
	/** Injected at build time: short git SHA or empty */
	var __BUILD_COMMIT__: string;

	namespace App {
		// interface Error {}
		// interface Locals {}
		// interface PageData {}
		// interface PageState {}
		// interface Platform {}
	}
}

export {};
