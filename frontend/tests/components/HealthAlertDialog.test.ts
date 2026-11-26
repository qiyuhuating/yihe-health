import { mount } from '@vue/test-utils'
import { defineComponent, reactive } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import HealthAlertDialog from '@/components/records/HealthAlertDialog.vue'

vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }))

const Dialog = defineComponent({
  props: ['modelValue'],
  template: '<section v-if="modelValue"><slot /><slot name="footer" /></section>'
})

describe('health alert evidence', () => {
  it('shows the active alarm evidence without inventing a universal normal range', () => {
    const dialog = reactive({
      visible: true, loading: false, hasAlert: 1,
      title: '异常详情', reason: '设备上报异常', oldPersonName: '测试长者',
      hr: 116, bp: 145, occurTime: '2026-11-08 10:00',
      detail: '当前监测档案触发', advice: '联系照护人员',
      rateBad: 1, preBad: 0, resolving: false,
      recordId: null, message: ''
    })
    const view = mount(HealthAlertDialog, {
      props: { dialog },
      global: {
        stubs: {
          'el-dialog': Dialog,
          'el-alert': defineComponent({ props: ['title'], template: '<p>{{ title }}</p>' }),
          'el-button': defineComponent({ template: '<button><slot /></button>' }),
          'el-icon': defineComponent({ template: '<span><slot /></span>' })
        }
      }
    })
    expect(view.text()).toContain('当前监测档案触发')
    expect(view.text()).toContain('116')
    expect(view.text()).not.toContain('正常范围')
    expect(view.text()).not.toContain('55-110')
    view.unmount()
  })
})
