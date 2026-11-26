import { reactive } from 'vue'

export type MapProvider = 'amap' | 'tencent' | 'google' | 'offline'
export type MapStyle = 'roadmap' | 'satellite'
export type MapCoordinateSystem = 'wgs84' | 'gcj02'
export interface MapPreferences {
  provider: MapProvider
  style: MapStyle
  coordinateSystem: MapCoordinateSystem
  loadOnlineAutomatically: boolean
  reducedMotion: boolean
}
const defaults: MapPreferences = {
  provider: 'amap',
  style: 'roadmap',
  coordinateSystem: 'wgs84',
  loadOnlineAutomatically: false,
  reducedMotion: false
}
const storageKey = 'yihe_map_preferences_v1'
function readPreferences(): MapPreferences {
  try {
    const data: unknown = JSON.parse(localStorage.getItem(storageKey) || 'null')
    if (!data || typeof data !== 'object') return { ...defaults }
    const value = data as Partial<MapPreferences>
    return {
      provider: value.provider === 'amap' || value.provider === 'tencent' || value.provider === 'google' || value.provider === 'offline' ? value.provider : defaults.provider,
      style: value.style === 'satellite' ? 'satellite' : 'roadmap',
      coordinateSystem: value.coordinateSystem === 'gcj02' ? 'gcj02' : 'wgs84',
      loadOnlineAutomatically: value.loadOnlineAutomatically === true,
      reducedMotion: value.reducedMotion === true
    }
  } catch { return { ...defaults } }
}
export const mapPreferences = reactive<MapPreferences>(readPreferences())
export function saveMapPreferences() {
  try { localStorage.setItem(storageKey, JSON.stringify(mapPreferences)) } catch { /* Session-only preferences. */ }
  document.documentElement.classList.toggle('reduce-motion', mapPreferences.reducedMotion)
}
export function resetMapPreferences() {
  Object.assign(mapPreferences, defaults)
  saveMapPreferences()
}
export function applyMapPreferences() {
  document.documentElement.classList.toggle('reduce-motion', mapPreferences.reducedMotion)
}
