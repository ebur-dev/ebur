import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

export default defineConfig({
  plugins: [svelte()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    // /wails/runtime.js is served by the app at run time, not bundled.
    rollupOptions: { external: ['/wails/runtime.js'] },
  },
})
