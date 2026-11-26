import { flushPromises, shallowMount } from '@vue/test-utils'
import { defineComponent, nextTick } from 'vue'
import { expect, it, vi } from 'vitest'
import Setup from '@/views/Setup.vue'

vi.mock('/static/brand/yihe-logo-512.webp', () => ({ default: '/static/brand/yihe-logo-512.webp' }))
const mocks = vi.hoisted(() => ({ create: vi.fn(), login: vi.fn() }))
vi.mock('@/desktop', () => ({ createAdministrator: mocks.create }))
vi.mock('@/api/client', () => ({ api: { post: mocks.login } }))
vi.mock('@/auth', () => ({ loadSession: vi.fn() }))
vi.mock('vue-router', () => ({ useRouter: () => ({ replace: vi.fn() }) }))

it('clears the setup password after creation even if automatic login fails', async () => {
  let rejectLogin!: (reason: Error) => void
  const login = new Promise<void>((_resolve, reject) => { rejectLogin = reject })
  mocks.login.mockReturnValue(login)
  const form = defineComponent({
    props: ['model'],
    methods: { validate: async () => true },
    template: '<form><slot /></form>'
  })
  const view = shallowMount(Setup, { global: { stubs: {
    ElForm: form, ElFormItem: true, ElInput: true, ElButton: true, ElIcon: true
  } } })
  try {
    const fields = view.getComponent(form).props('model')
    Object.assign(fields, { name: '测试', phone: '13800000000', password: 'TestPassword123' })
    const node = view.get('form').element
    node.dispatchEvent(new Event('submit', { cancelable: true }))
    node.dispatchEvent(new Event('submit', { cancelable: true }))
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledTimes(1)
    expect(fields.password).toBe('')
    expect(mocks.login).toHaveBeenCalledWith('/api/v1/auth/login', { phone: fields.phone, password: 'TestPassword123' })
    rejectLogin(new Error('offline'))
    await flushPromises()
    await nextTick()
    expect(view.get('[role="alert"]').text()).toContain('账号已创建')
  } finally {
    view.unmount()
  }
})
