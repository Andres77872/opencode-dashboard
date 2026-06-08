import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 7451,
    strictPort: true,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:7450',
        changeOrigin: true,
      },
      '/health': {
        target: 'http://127.0.0.1:7450',
        changeOrigin: true,
      },
    },
  },
})
