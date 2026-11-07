import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import './lib/i18n'
import './index.css'
import App from './App'
import { startLive } from './lib/live'

// Live updates refetch on change, so nothing needs to refetch on its own.
const qc = new QueryClient({
  defaultOptions: { queries: { staleTime: Infinity, refetchOnWindowFocus: true, retry: 1 } },
})
startLive(qc)

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={qc}>
      <App />
    </QueryClientProvider>
  </StrictMode>,
)
