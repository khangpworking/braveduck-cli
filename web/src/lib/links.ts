// Where the landing and 404 pages point. Built into the board, they are this
// server's own pages; built for Cloudflare Pages (npm run build:pages, see
// vite.config.ts), the landing page is the site's home and the board is on
// its own domain.
export const BOARD_URL: string = import.meta.env.VITE_BOARD_URL ?? '/'
export const LANDING_URL: string = import.meta.env.VITE_LANDING_URL ?? '/welcome'
