import { expect, it } from 'vitest'
import { mapPreferences, resetMapPreferences, saveMapPreferences } from '@/utils/mapProviders'
import { mapLocationUrl } from '@/utils/mapLinks'

it('builds only official map destinations with generic coordinate markers', () => {
  const amap = new URL(mapLocationUrl('amap', 116.397, 39.908, 'satellite'))
  expect(amap.origin).toBe('https://www.amap.com')
  expect(amap.pathname).toBe('/ssr/embed/place')
  expect(amap.searchParams.get('maptype')).toBe('satellite')
  expect(Number(amap.searchParams.get('lng'))).toBeGreaterThan(116.397)
  expect(amap.searchParams.get('name')).toBe('设备最近上报位置')
  const tencent = new URL(mapLocationUrl('tencent', 116.397, 39.908, 'roadmap'))
  expect(tencent.origin).toBe('https://apis.map.qq.com')
  expect(tencent.pathname).toBe('/uri/v1/marker')
  expect(tencent.searchParams.get('marker')).toContain('coord:')
  expect(tencent.toString()).not.toContain('长者姓名')
})

it('stores only supported map providers, display preferences and no credentials', () => {
  resetMapPreferences()
  mapPreferences.provider = 'tencent'
  mapPreferences.reducedMotion = true
  saveMapPreferences()
  expect(localStorage.getItem('yihe_map_preferences_v1')).toContain('"provider":"tencent"')
  expect(document.documentElement.classList.contains('reduce-motion')).toBe(true)
  resetMapPreferences()
  expect(mapPreferences.provider).toBe('amap')
  expect(document.documentElement.classList.contains('reduce-motion')).toBe(false)
})

it('does not double-transform coordinates already reported as GCJ-02', () => {
  const uri = new URL(mapLocationUrl('amap', 116.403, 39.91, 'roadmap', 'gcj02'))
  expect(uri.searchParams.get('lng')).toBe('116.403')
  expect(uri.searchParams.get('lat')).toBe('39.91')
})

it('uses the public Google Maps search URL without an API key or resident identity', () => {
  const url = new URL(mapLocationUrl('google', 116.397, 39.908, 'roadmap'))
  expect(url.origin).toBe('https://www.google.com')
  expect(url.pathname).toBe('/maps/search/')
  expect(url.searchParams.get('api')).toBe('1')
  expect(url.searchParams.get('query')).toBe('39.908,116.397')
  expect(url.searchParams.has('key')).toBe(false)
  expect(url.toString()).not.toContain('老人')
})

it('converts existing GCJ-02 samples back to WGS84 before Google Maps lookup', () => {
  const original = { longitude: 116.397, latitude: 39.908 }
  const amap = new URL(mapLocationUrl('amap', original.longitude, original.latitude, 'roadmap'))
  const url = new URL(mapLocationUrl(
    'google', Number(amap.searchParams.get('lng')), Number(amap.searchParams.get('lat')), 'roadmap', 'gcj02'
  ))
  const [latitude, longitude] = (url.searchParams.get('query') || '').split(',').map(Number)
  expect(longitude).toBeCloseTo(original.longitude, 4)
  expect(latitude).toBeCloseTo(original.latitude, 4)
})

it('keeps Google Maps as an explicit persisted provider choice', () => {
  resetMapPreferences()
  mapPreferences.provider = 'google'
  saveMapPreferences()
  expect(localStorage.getItem('yihe_map_preferences_v1')).toContain('"provider":"google"')
  resetMapPreferences()
})
