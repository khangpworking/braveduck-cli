// Your location: where you live, to show how far each job is. Three ways to
// pick it (this browser's location, a search, a click on the map) and the
// farthest you'll travel.
import { Component, Suspense, lazy, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { LocateFixed, MapPin, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Q, S, type Location, type Place } from '@/lib/settings'
import { locale } from '@/lib/i18n'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { confirm } from './Confirm'
import { Body, Field, Footer, Loading, Problem, Win, useErr, useFresh, type WinProps } from './parts'

const LocationMap = lazy(() => import('./LocationMap'))

/** Shows why the map didn't load instead of the map; the rest still works. */
class MapGuard extends Component<{ fallback: (e: Error) => React.ReactNode; children: React.ReactNode }, { error: Error | null }> {
  state = { error: null as Error | null }
  static getDerivedStateFromError(error: Error) {
    return { error }
  }
  render() {
    return this.state.error ? this.props.fallback(this.state.error) : this.props.children
  }
}

export function LocationWindow(p: WinProps) {
  const { t } = useTranslation()
  return (
    <Win
      {...p}
      title={t('Your location')}
      intro={t('Used to show how far each job is. It stays on this computer; typed addresses are looked up with OpenStreetMap, and the map loads OpenStreetMap tiles.')}
    >
      <LocationBody close={() => p.onOpenChange(false)} />
    </Win>
  )
}

function LocationBody({ close }: { close: () => void }) {
  const err = useErr()
  const q = useFresh(Q.location, S.location)
  if (q.isError) return <Body><Problem>{err(q.error)}</Problem></Body>
  if (!q.ready) return <Body><Loading /></Body>
  return <LocationForm saved={q.data} close={close} />
}

const coord = (n: number) => n.toLocaleString(locale(), { minimumFractionDigits: 5, maximumFractionDigits: 5 })

function LocationForm({ saved, close }: { saved: Location; close: () => void }) {
  const { t } = useTranslation()
  const err = useErr()
  const qc = useQueryClient()
  const [picked, setPicked] = useState<Place | null>(
    saved.set && saved.lat !== undefined && saved.lng !== undefined ? { lat: saved.lat, lng: saved.lng, label: saved.label ?? '' } : null,
  )
  const [fromSaved, setFromSaved] = useState(saved.set)
  const [maxKm, setMaxKm] = useState(String(saved.max_km || 10))
  const [query, setQuery] = useState('')
  const pick = (p: Place) => {
    setPicked(p)
    setFromSaved(false)
  }

  const search = useMutation({ mutationFn: S.places })
  const done = () => {
    for (const k of [Q.location, Q.setup, ['jobs']]) void qc.invalidateQueries({ queryKey: k })
  }
  const save = useMutation({
    mutationFn: S.saveLocation,
    onSuccess: () => {
      done()
      toast.success(t('Location saved — job distances update shortly.'))
      close()
    },
    onError: e => toast.error(t("Couldn't save: {error}", { error: err(e) })),
  })
  const forget = useMutation({
    mutationFn: async () => {
      if (!(await confirm({ title: t('Forget your location and all job distances?'), ok: t('Forget my location'), danger: true }))) return false
      await S.forgetLocation()
      return true
    },
    onSuccess: yes => {
      if (!yes) return
      done()
      toast(t('Your location is forgotten.'))
      close()
    },
    onError: e => toast.error(err(e)),
  })

  const locateMe = () => {
    if (!navigator.geolocation) {
      toast.error(t("This browser can't share its location — search or click the map instead."))
      return
    }
    navigator.geolocation.getCurrentPosition(
      pos => pick({ lat: pos.coords.latitude, lng: pos.coords.longitude, label: t('My current location') }),
      e =>
        toast.error(
          e.code === 1 ? t('Location permission denied — search or click the map instead.') : t("Couldn't get your location: {error}", { error: t(e.message) }),
        ),
      { enableHighAccuracy: false, timeout: 15_000 },
    )
  }

  const doSave = () => {
    if (!picked) {
      toast.error(t('Pick a place first: your current location, a search result, or a click on the map.'))
      return
    }
    save.mutate({ ...picked, max_km: Number(maxKm) || 0 })
  }

  const pinText = !picked
    ? t('Click the map to place your pin.')
    : fromSaved
      ? t('Saved: {place} ({lat}, {lon})', { place: picked.label, lat: coord(picked.lat), lon: coord(picked.lng) })
      : t('Pin: {place}{lat}, {lon}', { place: picked.label ? picked.label + ' — ' : '', lat: coord(picked.lat), lon: coord(picked.lng) })

  return (
    <>
      <Body>
        <Button className="h-12 w-full text-base sm:w-auto" onClick={locateMe}>
          <LocateFixed />
          {t('Use my current location')}
        </Button>
        <form
          className="flex gap-2"
          onSubmit={e => {
            e.preventDefault()
            if (query.trim()) search.mutate(query.trim())
          }}
        >
          <Input
            value={query}
            onChange={e => setQuery(e.target.value)}
            placeholder={t('…or search an address, district or city')}
            aria-label={t('Search an address')}
            className="h-11 flex-1 text-base md:text-base"
          />
          <Button type="submit" variant="outline" className="h-11 text-base" disabled={search.isPending}>
            <Search />
            {t('Search')}
          </Button>
        </form>
        {(search.isPending || search.isError || search.data) && (
          <div className="-mt-2 flex flex-col gap-1.5" data-places>
            {search.isPending ? (
              <span className="text-muted-foreground">{t('Searching…')}</span>
            ) : search.isError ? (
              <span className="text-red-700">{err(search.error)}</span>
            ) : search.data?.length ? (
              search.data.map(p => (
                <Button key={`${p.lat},${p.lng}`} variant="outline" className="h-auto min-h-11 justify-start py-2 text-left text-base whitespace-normal" onClick={() => pick(p)}>
                  <MapPin />
                  {p.label}
                </Button>
              ))
            ) : (
              <span className="text-muted-foreground">{t('No match. Try adding the city.')}</span>
            )}
          </div>
        )}
        <MapGuard
          fallback={e => (
            <Problem>{t('Map unavailable: {error}. You can still search or use your current location.', { error: t(e.message) })}</Problem>
          )}
        >
          <Suspense fallback={<Skeleton className="h-64 w-full rounded-lg" />}>
            <LocationMap at={picked} onPick={p => pick({ ...p, label: '' })} />
          </Suspense>
        </MapGuard>
        <p className="-mt-2 text-[0.95rem] text-muted-foreground" data-picked>
          {pinText}
        </p>
        <Field label={t('Maximum commute (km)')} htmlFor="max-km">
          <Input id="max-km" type="number" min={1} max={500} step={1} value={maxKm} onChange={e => setMaxKm(e.target.value)} className="h-11 w-32 text-base md:text-base" />
        </Field>
      </Body>
      <Footer className="sm:justify-between">
        {saved.set ? (
          <Button variant="outline" className="h-11 text-base" disabled={forget.isPending} onClick={() => forget.mutate()}>
            {t('Forget my location')}
          </Button>
        ) : (
          <span />
        )}
        <Button className="h-11 text-base" disabled={save.isPending} onClick={doSave}>
          {save.isPending ? t('Saving…') : t('Save')}
        </Button>
      </Footer>
    </>
  )
}
