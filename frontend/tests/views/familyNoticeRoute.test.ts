import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, nextTick } from 'vue'
import { expect, it, vi } from 'vitest'
import FamilyNotice from '@/views/FamilyNotice.vue'

const state = vi.hoisted(() => ({
  post: vi.fn(),
  query: null as unknown as { textId: string },
  push: vi.fn()
}))

vi.mock('vue-router', async () => {
  const { reactive } = await import('vue')
  state.query = reactive({ textId: 'A' })
  return { useRoute: () => ({ query: state.query }), useRouter: () => ({ push: state.push }) }
})
vi.mock('axios', async importOriginal => ({
  ...await importOriginal<typeof import('axios')>(),
  default: { post: state.post }
}))

const Input = defineComponent({
  props: ['modelValue'],
  emits: ['update:modelValue'],
  template: '<textarea :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)"></textarea>'
})
const Button = defineComponent({
  emits: ['click'],
  template: '<button @click="$emit(\'click\')"><slot /></button>'
})

it('retires an in-flight comment when the notice route key changes', async () => {
  let finishCreate!: (value: unknown) => void
  const createBodies: { textId?: unknown }[] = []
  state.post.mockReset()
  state.post.mockImplementation((url: string, body?: { textId?: unknown }) => {
    if (url === '/api/v1/notices/detail') return Promise.resolve({ data: { text: { title: '公告' }, pics: [] } })
    if (url === '/api/v1/notice-comments/list') return Promise.resolve({ data: { comments: [] } })
    if (url === '/api/v1/notice-comments/create') {
      createBodies.push(body || {})
      return new Promise(resolve => { finishCreate = resolve })
    }
    throw new Error(`unexpected request: ${url}`)
  })
  const view = mount(FamilyNotice, {
    global: {
      stubs: {
        'el-input': Input, 'el-button': Button,
        'el-form': { template: '<form><slot /></form>' },
        'el-form-item': { template: '<div><slot /></div>' }
      },
      directives: { loading() {} }
    }
  })
  try {
    await flushPromises()
    await view.find('textarea').setValue('给 A 的留言')
    const submit = view.findAll('button').find(button => button.text().includes('发表留言'))
    expect(submit).toBeDefined()
    await submit!.trigger('click')
    await flushPromises()
    expect(createBodies).toEqual([{ said: '给 A 的留言', textId: 'A' }])

    state.query.textId = 'B'
    await nextTick()
    await flushPromises()
    expect((view.find('textarea').element as HTMLTextAreaElement).value).toBe('')

    finishCreate({ data: { returnCode: '1111' } })
    await flushPromises()
    expect(view.text()).not.toContain('留言已发表。')
    expect(createBodies).toHaveLength(1)
  } finally {
    view.unmount()
  }
})
