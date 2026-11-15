// See https://svelte.dev/docs/kit/types#app.d.ts
// for information about these interfaces
declare global {
	namespace App {
		// interface Error {}
		// interface Locals {}
		// interface PageData {}
		// Home page views live in history so the phone's back button works
		interface PageState {
			view?: string;
			pushed?: boolean;
		}
		// interface Platform {}
	}
}

export {};
