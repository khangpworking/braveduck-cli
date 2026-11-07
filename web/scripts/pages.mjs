// After `vite build --mode pages`: the landing page becomes the site's
// index.html; Cloudflare Pages serves 404.html for unknown addresses itself.
import { renameSync } from 'node:fs'
renameSync('dist-pages/welcome.html', 'dist-pages/index.html')
