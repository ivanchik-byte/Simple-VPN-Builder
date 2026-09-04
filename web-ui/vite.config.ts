import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// https://vite.dev/config/
export default defineConfig({
  base: '/ui/',
  plugins: [react(), tailwindcss()],
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://localhost:8110',
      '/sub': 'http://localhost:8110',
    }
  },
  build: {
    outDir: '../internal/controlplane/web/dist',
    emptyOutDir: true,
  }
})
