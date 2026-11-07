import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import '../lib/i18n'
import '../index.css'
import { NotFound } from './NotFound'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <NotFound />
  </StrictMode>,
)
