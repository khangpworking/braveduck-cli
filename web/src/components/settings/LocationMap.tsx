// The map in the Location window: OpenStreetMap tiles, a click places the
// pin. Loaded only when the window opens (React.lazy), so Leaflet stays out
// of the board's main bundle.
import { useEffect } from 'react'
import L from 'leaflet'
import { MapContainer, Marker, TileLayer, ZoomControl, useMap, useMapEvents } from 'react-leaflet'
import { useTranslation } from 'react-i18next'
import 'leaflet/dist/leaflet.css'
import icon from 'leaflet/dist/images/marker-icon.png'
import icon2x from 'leaflet/dist/images/marker-icon-2x.png'
import shadow from 'leaflet/dist/images/marker-shadow.png'

const pin = L.icon({
  iconUrl: icon,
  iconRetinaUrl: icon2x,
  shadowUrl: shadow,
  iconSize: [25, 41],
  iconAnchor: [12, 41],
  shadowSize: [41, 41],
})

export interface Point {
  lat: number
  lng: number
}

function Clicks({ onPick }: { onPick: (p: Point) => void }) {
  useMapEvents({ click: e => onPick({ lat: e.latlng.lat, lng: e.latlng.lng }) })
  return null
}

/** Moves the view to a newly picked point, closer in if it's far out. */
function Follow({ at }: { at: Point | null }) {
  const map = useMap()
  useEffect(() => {
    if (at) map.setView([at.lat, at.lng], Math.max(map.getZoom(), 13))
  }, [at, map])
  // The window animates open; measure again once it has its size.
  useEffect(() => {
    const id = setTimeout(() => map.invalidateSize(), 120)
    return () => clearTimeout(id)
  }, [map])
  return null
}

export default function LocationMap({ at, onPick }: { at: Point | null; onPick: (p: Point) => void }) {
  const { t } = useTranslation()
  return (
    <MapContainer
      center={at ? [at.lat, at.lng] : [16, 106]}
      zoom={at ? 13 : 5}
      zoomControl={false}
      className="h-64 w-full rounded-lg border"
      id="location-map"
    >
      {/* OpenStreetMap refuses tiles asked for without a Referer, and the
          board sends none (Referrer-Policy: no-referrer): the tiles alone
          name the board's origin, nothing more. */}
      <TileLayer
        url="https://tile.openstreetmap.org/{z}/{x}/{y}.png"
        maxZoom={19}
        referrerPolicy="origin"
        attribution={`&copy; ${t('OpenStreetMap contributors')}`}
      />
      {at && <Marker position={[at.lat, at.lng]} icon={pin} title={t('Your location')} alt={t('Your location')} />}
      <ZoomControl position="topleft" zoomInTitle={t('Zoom in')} zoomOutTitle={t('Zoom out')} />
      <Clicks onPick={onPick} />
      <Follow at={at} />
    </MapContainer>
  )
}
