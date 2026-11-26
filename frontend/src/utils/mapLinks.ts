import { wgs84ToGcj02, gcj02ToWgs84 } from './gcj02'
import type { MapProvider, MapStyle, MapCoordinateSystem } from './mapProviders'

export function mapDisplayPosition(longitude: number, latitude: number, coordinateSystem: MapCoordinateSystem): [number, number] {
  return coordinateSystem === 'wgs84' ? wgs84ToGcj02(longitude, latitude) : [longitude, latitude]
}
export function mapLocationUrl(provider: Exclude<MapProvider, 'offline'>, longitude: number, latitude: number, style: MapStyle, coordinateSystem: MapCoordinateSystem = 'wgs84'): string {
  if (provider === 'google') {
    // Google Maps URLs use geographic GPS coordinates; GCJ-02 is only for Chinese map providers.
    const [lng, lat] = coordinateSystem === 'gcj02'
      ? gcj02ToWgs84(longitude, latitude) : [longitude, latitude]
    const params = new URLSearchParams({ api: '1', query: `${lat},${lng}` })
    return 'https://www.google.com/maps/search/?' + params.toString()
  }
  const [lng, lat] = mapDisplayPosition(longitude, latitude, coordinateSystem)
  if (provider === 'amap') {
    const params = new URLSearchParams({
      lng: String(lng), lat: String(lat), name: '设备最近上报位置',
      zoom: '16', maptype: style, platform: 'pc', src: 'yihe'
    })
    return 'https://www.amap.com/ssr/embed/place?' + params.toString()
  }
  const params = new URLSearchParams({
    marker: `coord:${lat},${lng};title:设备最近上报位置`,
    referer: 'yihe'
  })
  return 'https://apis.map.qq.com/uri/v1/marker?' + params.toString()
}
