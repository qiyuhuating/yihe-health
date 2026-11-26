const pi = Math.PI
const a = 6378245
const ee = 0.006693421622965943

// Only apply the GCJ-02 offset inside mainland China's approximate geographic bounds.
// Preserve all source measurements in storage and convert solely for the map overlay.
export function wgs84ToGcj02(lng: number, lat: number): [number, number] {
  if (lng < 73.66 || lng > 135.05 || lat < 3.86 || lat > 53.55) return [lng, lat]
  const x = lng - 105.0
  const y = lat - 35.0
  let dLat = -100 + 2 * x + 3 * y + 0.2 * y * y + 0.1 * x * y + 0.2 * Math.sqrt(Math.abs(x))
  dLat += (20 * Math.sin(6 * x * pi) + 20 * Math.sin(2 * x * pi)) * 2 / 3
  dLat += (20 * Math.sin(y * pi) + 40 * Math.sin(y / 3 * pi)) * 2 / 3
  dLat += (160 * Math.sin(y / 12 * pi) + 320 * Math.sin(y * pi / 30)) * 2 / 3
  let dLng = 300 + x + 2 * y + 0.1 * x * x + 0.1 * x * y + 0.1 * Math.sqrt(Math.abs(x))
  dLng += (20 * Math.sin(6 * x * pi) + 20 * Math.sin(2 * x * pi)) * 2 / 3
  dLng += (20 * Math.sin(x * pi) + 40 * Math.sin(x / 3 * pi)) * 2 / 3
  dLng += (150 * Math.sin(x / 12 * pi) + 300 * Math.sin(x / 30 * pi)) * 2 / 3
  const radLat = lat / 180 * pi
  const magic = 1 - ee * Math.sin(radLat) ** 2
  const sqrtMagic = Math.sqrt(magic)
  dLat = dLat * 180 / ((a * (1 - ee)) / (magic * sqrtMagic) * pi)
  dLng = dLng * 180 / (a / sqrtMagic * Math.cos(radLat) * pi)
  return [lng + dLng, lat + dLat]
}

export function gcj02ToWgs84(lng: number, lat: number): [number, number] {
  // Two corrections remove the map offset to sub-meter precision for typical GPS positions.
  let longitude = lng
  let latitude = lat
  for (let index = 0; index < 2; index++) {
    const [convertedLng, convertedLat] = wgs84ToGcj02(longitude, latitude)
    longitude += lng - convertedLng
    latitude += lat - convertedLat
  }
  return [longitude, latitude]
}
