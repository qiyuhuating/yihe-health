import { expect, it } from 'vitest'
import { wgs84ToGcj02 } from '@/utils/gcj02'

it('offsets WGS84 coordinates in mainland China for the Amap overlay', () => {
  const [lng, lat] = wgs84ToGcj02(116.397, 39.908)
  expect(lng).toBeGreaterThan(116.398)
  expect(lng).toBeLessThan(116.405)
  expect(lat).toBeGreaterThan(39.908)
  expect(lat).toBeLessThan(39.915)
})

it('preserves coordinates outside the mainland mapping extent', () => {
  expect(wgs84ToGcj02(151.2093, -33.8688)).toEqual([151.2093, -33.8688])
  expect(wgs84ToGcj02(-73.9857, 40.7484)).toEqual([-73.9857, 40.7484])
})
