import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, describe, it, expect, vi } from 'vitest'
import SecurityPanel from '@/components/SecurityPanel.vue'
const { post } = vi.hoisted(() => ({ post: vi.fn() }))
vi.mock('@/api/client', () => ({ api: { post }, accepted: (data: { returnCode: string }) => { if (data.returnCode !== '1111') throw new Error('rejected') } }))
describe('two-factor setup', () => {
  beforeEach(() => { post.mockReset() })
  it('shows a pending secret, then displays recovery codes only after confirmation', async () => {
    post.mockResolvedValueOnce({ data: { returnCode: '1111', secret: 'MANUALTESTSEED' } }).mockResolvedValueOnce({ data: { returnCode: '1111', recoveryCodes: ['one-use-code'], reloginRequired: true } })
    const view = mount(SecurityPanel)
    await view.get('#security-password').setValue('strong-password')
    await view.findAll('button')[0]!.trigger('click'); await flushPromises()
    expect((view.get('#security-secret').element as HTMLInputElement).value).toBe('MANUALTESTSEED')
    expect(view.find('#security-recovery').exists()).toBe(false)
    await view.get('#security-otp').setValue('123456')
    const confirm = view.findAll('button').find(button => button.text() === '确认启用')!
    await confirm.trigger('click'); await flushPromises()
    expect((view.get('#security-recovery').element as HTMLTextAreaElement).value).toBe('one-use-code')
    expect(view.find('#security-password').exists()).toBe(false)
    expect(view.find('#security-secret').exists()).toBe(false)
    view.unmount()
  })
  it('does not claim setup succeeded after a server failure', async () => {
    post.mockRejectedValue({ response: { data: { returnCode: '0000', code: 'INVALID_REQUEST', message: '安全密钥不可用' } } })
    const view = mount(SecurityPanel)
    await view.get('#security-password').setValue('strong-password')
    await view.findAll('button')[0]!.trigger('click'); await flushPromises()
    expect(view.get('[role=alert]').text()).toBe('安全密钥不可用')
    expect(view.find('#security-secret').exists()).toBe(false)
    view.unmount()
  })
})
