import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// The proxy keeps the browser on one origin, so the session cookie
// works without CORS. Production uses the same /api prefix via nginx.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: process.env.API_URL ?? 'http://localhost:8080',
        rewrite: (path) => path.replace(/^\/api/, ''),
      },
    },
  },
})
