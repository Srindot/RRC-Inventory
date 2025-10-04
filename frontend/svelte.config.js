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
    // Ensure all routes are pre-rendered for static hosting
    prerender: {
      handleMissingId: 'warn',
      handleHttpError: 'warn'
    }
  }
};

export default config;