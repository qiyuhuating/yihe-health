import { defineComponent, h, type App } from 'vue'
import {
  ElAlert, ElBadge, ElButton, ElConfigProvider, ElDatePicker, ElDialog, ElDropdown, ElDropdownItem, ElDropdownMenu,
  ElForm, ElFormItem, ElInput, ElInputNumber, ElIcon, ElLoading, ElMenu, ElMenuItem, ElOption,
  ElPagination, ElRadio, ElRadioGroup, ElSelect, ElSubMenu, ElSwitch, ElTabPane, ElTable, ElTableColumn, ElTabs, ElTag, ElUpload
} from 'element-plus'
import zhCn from 'element-plus/es/locale/lang/zh-cn'

const components = [
  ElAlert, ElBadge, ElButton, ElDatePicker, ElDialog, ElDropdown, ElDropdownItem, ElDropdownMenu, ElForm, ElFormItem,
  ElIcon, ElInput, ElInputNumber, ElMenu, ElMenuItem, ElOption, ElPagination, ElRadio, ElRadioGroup, ElSelect, ElSubMenu,
  ElSwitch, ElTabPane, ElTable, ElTableColumn, ElTabs, ElTag, ElUpload
]

const CareConfig = defineComponent({
  setup (_, { slots }) { return () => h(ElConfigProvider, { locale: zhCn }, slots) }
})

export default function installElement (app: App) {
  components.forEach(component => app.use(component))
  app.directive('loading', ElLoading.directive)
  app.component('CareConfig', CareConfig)
}
