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

// The public site on Cloudflare Pages: the landing page is its home, and the
// board lives on its own domain behind Cloudflare Access.
const PAGES = { VITE_BOARD_URL: 'https://app.braveduck.io.vn', VITE_LANDING_URL: '/' }

// `--mode pages` builds only the landing and 404 pages, for Cloudflare Pages
// (scripts/pages.mjs then makes the landing page the site's index.html).
export default defineConfig(({ mode }) => ({
  base: '/',
  plugins: [react(), tailwindcss(), devToken()],
  define:
    mode === 'pages'
      ? Object.fromEntries(Object.entries(PAGES).map(([k, v]) => [`import.meta.env.${k}`, JSON.stringify(v)]))
      : {},
  resolve: { alias: { '@': path.resolve(import.meta.dirname, './src') } },
  server: { proxy: { '/api': board } },
  // The Go binary embeds the build (internal/api/app.go).
  // Three pages: the board, the landing page (/welcome) and the 404 page.
  build: {
    outDir: mode === 'pages' ? 'dist-pages' : '../internal/api/app',
    emptyOutDir: true,
    chunkSizeWarningLimit: 1000,
    rollupOptions: {
      input: {
        ...(mode === 'pages' ? {} : { index: 'index.html' }),
        welcome: 'welcome.html',
        notfound: '404.html',
      } as Record<string, string>,
    },
  },
}))
