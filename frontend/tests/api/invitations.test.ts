import { afterEach, expect, it, vi } from 'vitest'
import axios from 'axios'
import { invitations } from '@/api/invitations'
import { REQUEST_TIMEOUT_MS } from '@/api/client'
afterEach(() => vi.restoreAllMocks())
it('preserves invitation routes, payloads and cookies', async () => {
  const post = vi.spyOn(axios, 'post').mockResolvedValue({ data: { returnCode: '1111' } })
  const cases = [['checkCode', { code: 'abc' }], ['join', { code: 'abc' }], ['list', { page: 2, pageSize: 10, state: 'used' }], ['oldPersonList', {}], ['create', { oldId: 7, createUser: 'operator' }], ['delete', { id: 9 }]] as const
  for (const [name, body] of cases) {
    await (invitations[name] as Function)(body)
    expect(post).toHaveBeenLastCalledWith(`/api/v1/inviteCode/${name}`, body, { withCredentials: true, signal: undefined, timeout: REQUEST_TIMEOUT_MS })
  }
})
