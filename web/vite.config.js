import {defineConfig} from 'vite'
import vue from '@vitejs/plugin-vue'

// The dev server proxies API calls to the Go backend so the browser only
// ever talks to one origin. In production the Go binary serves the built
// assets itself.
export default defineConfig({
  plugins: [vue()],
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
})
