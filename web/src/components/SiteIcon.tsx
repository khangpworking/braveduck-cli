// Each job site's own icon (its favicon, bundled with the board: no request
// to the site or a favicon service when it's shown).
import careerlink from '@/assets/sites/careerlink.png'
import facebook from '@/assets/sites/facebook.png'
import joboko from '@/assets/sites/joboko.png'
import topcv from '@/assets/sites/topcv.png'
import vietnamworks from '@/assets/sites/vietnamworks.png'
import { cn } from '@/lib/utils'

const ICONS: Record<string, string> = { careerlink, facebook, joboko, topcv, vietnamworks }

/**
 * The site's icon, or nothing for a source without one. Next to the site's
 * name it's decoration; on its own, label says which site it is.
 */
export function SiteIcon({ id, label, className }: { id: string; label?: string; className?: string }) {
  const src = ICONS[id.toLowerCase()]
  if (!src) return null
  return (
    <img
      src={src}
      alt={label ?? ''}
      title={label}
      data-site-icon={id.toLowerCase()}
      width={16}
      height={16}
      className={cn('inline-block size-4 shrink-0 rounded-sm align-text-bottom', className)}
    />
  )
}
