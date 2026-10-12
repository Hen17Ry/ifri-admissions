import { fileURLToPath, URL } from 'node:url'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
import { loadEnv } from 'vite'
import { defineConfig } from 'vitest/config'

export default defineConfig(({ mode }) => {
  // Only the API port is read from the root .env; secrets never become VITE_* values.
  const env = loadEnv(mode, fileURLToPath(new URL('../../', import.meta.url)), 'API_PORT')
  const apiPort = env.API_PORT || '18080'
  if (!/^\d+$/.test(apiPort) || Number(apiPort) < 1 || Number(apiPort) > 65535) {
    throw new Error('API_PORT must be a valid TCP port')
  }
  return {
    plugins: [vue(), tailwindcss()],
    resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
    server: {
      host: '127.0.0.1',
      port: 5173,
      strictPort: true,
      proxy: {
        '/api': {
          target: `http://127.0.0.1:${apiPort}`,
          rewrite: (path) => (path.startsWith('/api/v1/') ? path : path.replace(/^\/api/, '')),
        },
      },
    },
    test: {
      include: ['src/**/*.test.ts'],
      environment: 'jsdom',
      clearMocks: true,
      restoreMocks: true,
    },
  }
})
