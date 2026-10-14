import { fileURLToPath, URL } from 'node:url'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
import { loadEnv } from 'vite'
import { defineConfig } from 'vitest/config'
import type { Plugin } from 'vite'

// The administration SPA is never framed, cached or referred. Development keeps what the Vite
// client needs (HMR websocket, injected styles); production policy: docs/ADMIN_SECURITY.md.
function adminHeaders(): Plugin {
  const headers = (dev: boolean): Record<string, string> => ({
    'Content-Security-Policy': [
      "default-src 'self'",
      "script-src 'self'",
      `style-src 'self'${dev ? " 'unsafe-inline'" : ''}`,
      "img-src 'self' data:",
      "font-src 'self'",
      `connect-src 'self'${dev ? ' ws://localhost:5173' : ''}`,
      "object-src 'none'",
      "base-uri 'none'",
      "form-action 'self'",
      "frame-ancestors 'none'",
    ].join('; '),
    'X-Frame-Options': 'DENY',
    'X-Content-Type-Options': 'nosniff',
    'Referrer-Policy': 'no-referrer',
    'Cross-Origin-Opener-Policy': 'same-origin',
    'Permissions-Policy': 'camera=(), microphone=(), geolocation=(), payment=(), usb=()',
    'Cache-Control': 'no-store',
  })
  const apply =
    (dev: boolean) =>
    (
      req: { url?: string },
      res: { setHeader(name: string, value: string): void },
      next: () => void,
    ) => {
      if (
        req.url === '/admin' ||
        req.url?.startsWith('/admin/') ||
        req.url?.startsWith('/admin?')
      ) {
        for (const [name, value] of Object.entries(headers(dev))) res.setHeader(name, value)
      }
      next()
    }
  return {
    name: 'ifri-admin-headers',
    configureServer: (server) => void server.middlewares.use(apply(true)),
    configurePreviewServer: (server) => void server.middlewares.use(apply(false)),
  }
}

export default defineConfig(({ mode }) => {
  // Only the API port is read from the root .env; secrets never become VITE_* values.
  const env = loadEnv(mode, fileURLToPath(new URL('../../', import.meta.url)), 'API_PORT')
  const apiPort = env.API_PORT || '18080'
  if (!/^\d+$/.test(apiPort) || Number(apiPort) < 1 || Number(apiPort) > 65535) {
    throw new Error('API_PORT must be a valid TCP port')
  }
  return {
    plugins: [vue(), tailwindcss(), adminHeaders()],
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
