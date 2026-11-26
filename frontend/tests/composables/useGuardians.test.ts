import { afterEach, describe, it, expect, vi } from 'vitest'
import { effectScope } from 'vue'
import axios from 'axios'
import { useGuardians, validAge, validPhone } from '@/composables/useGuardians'
import { api, type PostClient } from '@/api/client'
import { flushPromises } from '@vue/test-utils'

describe('guardian data flow', () => {
  afterEach(() => { vi.useRealTimers() })

  it('passes cancellation through the real API client and aborts on scope disposal', async () => {
    const post = vi.spyOn(axios, 'post').mockReturnValue(new Promise(() => {}))
    const scope = effectScope()
    const state = scope.run(() => useGuardians(api))!
    let settled = false
    state.loadGuardians().then(() => { settled = true })
    await flushPromises()
    const config = post.mock.calls[0][2]!
    expect(config.withCredentials).toBe(true)
    expect(config.signal?.aborted).toBe(false)
    scope.stop()
    await flushPromises()
    expect(config.signal?.aborted).toBe(true)
    expect(settled).toBe(true)
    expect(state.isLoading.value).toBe(false)
    expect(state.loadError.value).toBe('')
    await state.loadGuardians()
    expect(post).toHaveBeenCalledTimes(1)
  })

  it('ends loading on timeout, then recovers with a new list request', async () => {
    vi.useFakeTimers()
    const post = vi.fn().mockReturnValueOnce(new Promise(() => {})).mockResolvedValueOnce({ data: { customers: [], total: 0 } })
    const state = useGuardians({ post } as PostClient)
    const pending = state.loadGuardians()
    await vi.advanceTimersByTimeAsync(10000)
    expect(state.isLoading.value).toBe(false)
    expect(state.loadError.value).not.toBe('')
    expect(post.mock.calls[0][2].signal.aborted).toBe(true)
    await pending
    await state.loadGuardians()
    expect(state.loadError.value).toBe('')
    expect(state.guardianList.value).toEqual([])
  })

  it('preserves an unconfirmed write and never aborts or replays it when reads time out or scope stops', async () => {
    vi.useFakeTimers()
    let rejectWrite!: (error: Error) => void
    const post = vi.fn((path: string) => path.endsWith('/list')
      ? new Promise(() => {})
      : new Promise((_resolve, reject) => { rejectWrite = reject }))
    const scope = effectScope()
    const state = scope.run(() => useGuardians({ post } as PostClient))!
    const form = { customerName: '姓名', customerPhone: '19912345678', customerAge: 70 }
    Object.assign(state.createForm, form)
    state.createDialogVisible.value = true
    const pendingRead = state.loadGuardians()
    const saved = state.save()
    await vi.advanceTimersByTimeAsync(10000)
    await pendingRead
    expect(state.saving.value).toBe(true)
    const write = post.mock.calls.find(([path]) => path.endsWith('/create'))!
    expect(write).toHaveLength(2)
    scope.stop()
    rejectWrite(new Error('response lost; verify result before retry'))
    expect(await saved).toBe(false)
    await vi.advanceTimersByTimeAsync(30000)
    expect(state.createForm).toMatchObject(form)
    expect(state.createDialogVisible.value).toBe(true)
    expect(state.mutationError.value).toContain('未收到保存确认')
    expect(post.mock.calls.filter(([path]) => path.endsWith('/create'))).toHaveLength(1)
  })

  it('loads the latest page and search after changes during an active read', async () => {
    let first!: (value: unknown) => void
    let last!: (value: unknown) => void
    const post = vi.fn().mockReturnValueOnce(new Promise(resolve => { first = resolve })).mockReturnValueOnce(new Promise(resolve => { last = resolve }))
    const state = useGuardians({ post } as PostClient)
    const pending = state.loadGuardians()
    await flushPromises()
    state.changePage(2)
    state.changePage(3)
    state.searchText.value = '新查询'
    state.loadGuardians()
    first({ data: { customers: [{ customerName: '旧数据' }], total: 99 } })
    await flushPromises()
    expect(state.guardianList.value).toEqual([])
    expect(post.mock.calls.map(([, body]) => body)).toEqual([
      { page: 1, pageSize: 10, customerName: '' },
      { page: 1, pageSize: 10, customerName: '新查询' }
    ])
    last({ data: { customers: [{ customerName: '当前数据' }], total: 1 } })
    await pending
    expect(state.guardianList.value[0]?.customerName).toBe('当前数据')
    expect(state.total.value).toBe(1)
    expect(state.isLoading.value).toBe(false)
  })

  it('can refresh after an earlier customer query hangs indefinitely', async () => {
    const post = vi.fn()
      .mockReturnValueOnce(new Promise(() => {}))
      .mockResolvedValueOnce({ data: { customers: [{ customerName: '新数据' }], total: 1 } })
    const state = useGuardians({ post } as PostClient)
    const old = state.loadGuardians()
    await flushPromises()
    const current = state.changePage(2)
    await Promise.all([old, current])
    expect(post.mock.calls.map(([, body]) => body.page)).toEqual([1, 2])
    expect(state.guardianList.value[0]?.customerName).toBe('新数据')
    expect(state.isLoading.value).toBe(false)
  })
  it('waits for the replacement read after a confirmed write before save completes', async () => {
    let first!: (value: unknown) => void
    let refreshed!: (value: unknown) => void
    const post = vi.fn()
      .mockReturnValueOnce(new Promise(resolve => { first = resolve }))
      .mockResolvedValueOnce({ data: { returnCode: '1111' } })
      .mockReturnValueOnce(new Promise(resolve => { refreshed = resolve }))
    const state = useGuardians({ post } as PostClient)
    state.loadGuardians()
    await flushPromises()
    Object.assign(state.createForm, { customerName: '姓名', customerPhone: '19912345678', customerAge: 70 })
    const saved = state.save()
    await flushPromises()
    expect(state.saving.value).toBe(true)
    expect(post.mock.calls[0][2].signal.aborted).toBe(true)
    refreshed({ data: { customers: [], total: 0 } })
    expect(await saved).toBe(true)
    first({ data: { customers: [{ customerName: '过期数据' }], total: 1 } })
    await flushPromises()
    expect(post.mock.calls.map(([path]) => path)).toEqual(['api/v1/guardians/list', 'api/v1/guardians/create', 'api/v1/guardians/list'])
    expect(state.guardianList.value).toEqual([])
    expect(state.saving.value).toBe(false)
  })
  it('accepts modern phone prefixes and valid integer age boundaries', () => {
    expect(validPhone('19912345678')).toBe(true)
    expect(validPhone('1|912345678')).toBe(false)
    for (const age of [0, '0', 150]) expect(validAge(age)).toBe(true)
    for (const age of ['', '   ', null, undefined, false, -1, 151, 1.2, '1.5', '1e2', '0x10']) expect(validAge(age)).toBe(false)
  })
  it('clears failed list state and can retry the same page', async () => {
    const post = vi.fn().mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce({ data: { customers: [], total: 0 } })
    const state = useGuardians({ post } as PostClient)
    state.currentPage.value = 3
    await state.loadGuardians(true)
    expect(state.loadError.value).not.toBe('')
    expect(state.isLoading.value).toBe(false)
    await state.loadGuardians(true)
    expect(post.mock.calls[1]?.[1]).toMatchObject({ page: 3 })
    expect(state.loadError.value).toBe('')
  })
  it('keeps a rejected edit open and never mutates the selected table row', async () => {
    const post = vi.fn().mockResolvedValue({ data: { returnCode: '0000', code: 'INVALID_REQUEST', message: 'denied' } })
    const state = useGuardians({ post } as PostClient)
    const row = { customerId: 1, customerName: '原名', customerAge: 70, customerPhone: '19912345678', customerSex: '女', customerAddress: '' }
    state.openEditDialog(0, row); state.editForm.customerName = '编辑中'
    expect(row.customerName).toBe('原名')
    expect(await state.save(true)).toBe(false)
    expect(state.editDialogVisible.value).toBe(true)
    expect(state.mutationError.value).toBe('denied')
  })
  it('does not pretend a rejected deletion succeeded', async () => {
    const post = vi.fn().mockRejectedValue(new Error('network'))
    const state = useGuardians({ post } as PostClient)
    expect(await state.remove(1)).toBe(false)
    expect(post).toHaveBeenCalledTimes(1)
    expect(state.mutationError.value).not.toBe('')
  })
})
