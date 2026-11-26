import { describe, expect, it } from 'vitest'
import { noticeImageUrl } from '@/domain/notices'

describe('notice attachment URLs', () => {
  it('uses an absolute API route from nested family and management pages', () => {
    expect(noticeImageUrl('care photo #1.png'))
      .toBe('/api/v1/image/download?name=care%20photo%20%231.png')
  })

  it('encodes separators rather than turning file names into routes', () => {
    expect(noticeImageUrl('a/b?c.png'))
      .toBe('/api/v1/image/download?name=a%2Fb%3Fc.png')
  })
})
