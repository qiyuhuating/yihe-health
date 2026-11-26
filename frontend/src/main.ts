import { createApp } from 'vue'
import { ElMessage } from 'element-plus'
import App from './App.vue'
import { applyMapPreferences } from './utils/mapProviders'
import DatabaseNotice from './components/DatabaseNotice.vue'
import { installDesktopKeys } from './desktop'
import router from './router'
import installElement from './element'
import { installHttpSecurity } from './auth'
import { recoverFromChunkError } from './appGuards'
import 'element-plus/dist/index.css'
import '@/assets/css/fonts.css'
import '@/assets/css/index.css'
import '@/assets/css/tokens.css'
import '@/assets/css/element-theme.css'
import '@/assets/css/accessibility.css'
import '@/assets/css/commercial.css'

applyMapPreferences()
const app = createApp(App)
app.component('database-notice', DatabaseNotice)
installElement(app)
installHttpSecurity(router)
// 兜底：组件渲染/生命周期里未捕获的错误不再静默，页面脚本加载失败时自动刷新到新版本。
app.config.errorHandler = (error, _instance, info) => {
  console.error('[app]', info, error)
  if (recoverFromChunkError(error)) return
  ElMessage.error({ message: '页面出现异常，请刷新后重试；若持续出现请联系管理员。', grouping: true })
}
app.use(router)
app.mount('#app')
installDesktopKeys()
