import { toast } from 'sonner'

export function copy(text: string, t: (k: string) => string) {
  return navigator.clipboard.writeText(text).then(
    () => toast(t('Copied.')),
    () => toast.error(t("Couldn't copy. Select the text and copy it yourself.")),
  )
}
