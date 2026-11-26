// svelte.config.js
import adapter from '@sveltejs/adapter-static';

/** @type {import('@sveltejs/kit').Config} */
const config = {
  kit: {
    adapter: adapter({
      pages: 'build',
      assets: 'build',
      fallback: 'index.html', 
      precompress: false,
      strict: true
    }),
    // Check for a new deploy every minute; once there is one, the next link
    // followed loads the new version in full instead of continuing with the
    // old app in a tab that was left open.
    version: {
      pollInterval: 60000
    },
    // Ensure all routes are pre-rendered for static hosting
    prerender: {
      handleMissingId: 'warn',
      handleHttpError: 'warn'
    }
  }
};

export default config;