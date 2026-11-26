import { describe, expect, it, vi } from 'vitest'
import { api } from '@/api/client'
import { postContract } from '@/api/contractClient'

vi.mock('@/api/client', () => ({ api: { post: vi.fn() } }))

describe('generated contract client', () => {
  it('uses the existing relative API transport and preserves cancellation', async () => {
    const response = { data: { returnCode: '1111', oldId: 3, items: [] } }
    vi.mocked(api.post).mockResolvedValueOnce(response)
    const controller = new AbortController()
    const result = await postContract('/api/v1/health/latest', { oldId: 3 }, { signal: controller.signal })
    expect(api.post).toHaveBeenCalledWith('api/v1/health/latest', { oldId: 3 }, { signal: controller.signal })
    expect(result).toBe(response)
  })
})
