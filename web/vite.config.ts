import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// The backend the dev server proxies to. Override it to develop against a
// dashboard on another port (a scratch instance, say) without touching the one
// on the default port: OCD_API_TARGET=http://127.0.0.1:8399 npm run dev
const apiTarget = process.env.OCD_API_TARGET || 'http://127.0.0.1:7450'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 7451,
    strictPort: true,
    proxy: {
      '/api': {
        target: apiTarget,
        changeOrigin: true,
      },
      '/health': {
        target: apiTarget,
        changeOrigin: true,
      },
    },
  },
})
