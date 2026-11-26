import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import RecordCards from '@/components/RecordCards.vue'

describe('record card data boundary', () => {
  it('renders plain typed record fields and keeps actions attached to the selected row', () => {
    const records = [{ oldId: 42, name: '测试长者', status: 1, note: null }]
    const view = mount(RecordCards, {
      props: {
        records,
        label: '长者信息',
        fields: [
          { key: 'status', label: '告警状态', state: 'fall' },
          { key: 'note', label: '备注' }
        ]
      },
      slots: { actions: ({ row }: { row: object }) => (row as typeof records[number]).name }
    })
    expect(view.text()).toContain('测试长者')
    expect(view.text()).toContain('紧急告警')
    expect(view.text()).toContain('暂未提供')
    expect(view.find('.record-actions').text()).toBe('测试长者')
  })
})
