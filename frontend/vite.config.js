import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

export default defineConfig({
  plugins: [svelte()],
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
      '/ws': { target: 'ws://localhost:8080', ws: true }
    }
  },
  build: {
    outDir: 'dist',
    target: 'es2022',
    reportCompressedSize: false,
    cssCodeSplit: true,
    rollupOptions: {
      output: {
        // Vendor-Code separat cachen (immutable, hashbasiert)
        manualChunks(id) {
          if (
            id.includes('node_modules/svelte') ||
            id.includes('marked') ||
            id.includes('dompurify')
          ) {
            return 'vendor'
          }
        }
      }
    }
  }
})
