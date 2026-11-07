import path from 'node:path'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { defineConfig, type Plugin } from 'vite'

// `npm run dev` talks to a running board (BRAVEDUCK_URL, default
// http://127.0.0.1:8097): /api is proxied there, and the page gets that
// board's per-run token, read from its own page.
const board = process.env.BRAVEDUCK_URL ?? 'http://127.0.0.1:8097'

function devToken(): Plugin {
  return {
    name: 'braveduck-dev-token',
    apply: 'serve',
    async transformIndexHtml(html) {
      const page = await fetch(board).then(r => r.text()).catch(() => '')
      const token = /name="braveduck-token" content="([^"]*)"/.exec(page)?.[1] ?? ''
      return html.replace('<meta name="braveduck-token" content="">', `<meta name="braveduck-token" content="${token}">`)
    },
  }
}

export default defineConfig({
  base: '/',
  plugins: [react(), tailwindcss(), devToken()],
  resolve: { alias: { '@': path.resolve(import.meta.dirname, './src') } },
  server: { proxy: { '/api': board } },
  // The Go binary embeds the build (internal/api/app.go).
  // Three pages: the board, the landing page (/welcome) and the 404 page.
  build: {
    outDir: '../internal/api/app',
    emptyOutDir: true,
    chunkSizeWarningLimit: 1000,
    rollupOptions: { input: { index: 'index.html', welcome: 'welcome.html', notfound: '404.html' } },
  },
})
