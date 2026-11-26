import { mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { expect, it } from 'vitest'
import HarmonyWatchSetup from '@/components/HarmonyWatchSetup.vue'

it('guides device setup with an HTTPS host, keeping credentials out of address configuration', async () => {
  const Input = defineComponent({ props: ['modelValue'], emits: ['update:modelValue'], template: '<input :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />' })
  const Button = defineComponent({ template: '<button><slot /></button>' })
  const view = mount(HarmonyWatchSetup, { props: { deviceNo: 'WATCH001' }, global: { stubs: { ElInput: Input, ElButton: Button, ElRadioGroup: true, ElRadioButton: true } } })
  try {
    await view.get('input').setValue('http://192.168.1.10')
    expect(view.find('[role="alert"]').exists()).toBe(true)
    expect(view.find('button').exists()).toBe(false)
    await view.get('input').setValue('https://care.example.com')
    expect(view.find('[role="alert"]').exists()).toBe(false)
    expect(view.get('button').text()).toContain('复制后台地址')
    expect(view.text()).toContain('WATCH001')
    expect(view.text()).toContain('家属邀请码不能用于设备绑定')
    await view.get('input').setValue('https://care.example.com/?secret=key')
    expect(view.find('button').exists()).toBe(false)
  } finally { view.unmount() }
})
