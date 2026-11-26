<template>
  <el-dialog v-model="visible" class="general-settings-dialog" title="常规设置" width="min(520px, 94vw)" append-to-body>
    <section class="settings-group">
      <h3>显示与操作</h3>
      <div class="setting"><label for="setting-large-text">大字模式</label><el-switch id="setting-large-text" :model-value="largeText" @change="updateLargeText" /></div>
      <div class="setting"><label for="setting-high-contrast">高对比度模式</label><el-switch id="setting-high-contrast" :model-value="highContrast" @change="updateHighContrast" /></div>
      <div class="setting"><label for="setting-reduced-motion">减少界面动画</label><el-switch id="setting-reduced-motion" v-model="mapPreferences.reducedMotion" @change="saveMapPreferences" /></div>
      <div class="setting"><span>当前显示器刷新率</span><strong>{{ hostHz == null ? (probing ? '检测中…' : hostError || '暂不可用') : hostHz + ' Hz' }}</strong></div>
      <div class="setting"><span>WebView 空载帧调度</span><strong>{{ frameHz == null ? (probing ? '采样中…' : '未测得') : '约 ' + frameHz + ' FPS' }}</strong></div>
      <p class="hint" role="status">{{ refreshAdvice }}</p>
      <el-button plain :loading="probing" @click="probe">重新检测显示性能</el-button>
    </section>
    <section class="settings-group">
      <h3>位置地图</h3>
      <label for="setting-map-provider">默认地图服务</label>
      <el-select id="setting-map-provider" v-model="mapPreferences.provider" @change="saveMapPreferences">
        <el-option value="amap" label="高德地图 · 内嵌，无需 Key" />
        <el-option value="tencent" label="腾讯地图 · 在浏览器打开" />
        <el-option value="google" label="Google 地图 · 官方免 Key 链接" />
        <el-option value="offline" label="仅显示原始坐标（不联网）" />
      </el-select>
      <label for="setting-coordinate-system">设备上报坐标系</label>
      <el-select id="setting-coordinate-system" v-model="mapPreferences.coordinateSystem" @change="saveMapPreferences">
        <el-option value="wgs84" label="WGS84 · 原始 GPS（默认）" />
        <el-option value="gcj02" label="GCJ-02 · 已转换的中国地图坐标" />
      </el-select>
      <template v-if="mapPreferences.provider === 'amap'">
        <label for="setting-map-style">高德地图类型</label>
        <el-select id="setting-map-style" v-model="mapPreferences.style" @change="saveMapPreferences">
          <el-option value="roadmap" label="标准地图" />
          <el-option value="satellite" label="卫星地图" />
        </el-select>
        <div class="setting"><label for="setting-map-auto">选中位置后自动加载地图</label><el-switch id="setting-map-auto" v-model="mapPreferences.loadOnlineAutomatically" @change="saveMapPreferences" /></div>
      </template>
      <p class="hint">默认按需加载。加载在线地图时，所选坐标会发送给地图服务商；不会发送老人姓名。腾讯地图与 Google 地图使用官方位置链接，经确认后由浏览器打开；Google 地图在中国大陆网络可能不可用。坐标系必须与设备真实上报格式一致，错误设置会产生位置偏移。需要自定义轨迹、多点图层和 Key 认证时，应通过独立 SDK 接口实现。</p>
    </section>
    <template #footer><el-button @click="resetMapPreferences">恢复地图及动画默认设置</el-button><el-button type="primary" @click="visible=false">完成</el-button></template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { isTauri } from '@tauri-apps/api/core'
import { mapPreferences, saveMapPreferences, resetMapPreferences } from '../utils/mapProviders'
import { hostMonitorHz, sampleWebViewHz } from '../utils/displayRefresh'
import { useTextSizePreference } from '../composables/useTextSizePreference'

const visible = ref(false)
const { largeText, highContrast, setLargeText, setHighContrast } = useTextSizePreference()
const hostHz = ref<number | null>(null)
const frameHz = ref<number | null>(null)
const probing = ref(false)
const hostError = ref('')
let activeProbe: AbortController | undefined

const refreshAdvice = computed(() => {
  if (!isTauri()) return '网页无法读取显示器的系统刷新率；下方空载采样只反映浏览器帧调度。'
  if (hostHz.value == null) return hostError.value || '正在读取当前窗口所在显示器的系统刷新率。'
  if (hostHz.value <= 300) return `当前显示模式为 ${hostHz.value} Hz，无法真正显示超过 300 FPS；需更高刷新率的显示模式。`
  if (frameHz.value != null && frameHz.value <= 300) return `显示器支持 ${hostHz.value} Hz，但 WebView 空载仅测得约 ${frameHz.value} FPS，需要检查系统节能和帧调度限制。`
  return `当前显示模式为 ${hostHz.value} Hz，具备超过 300 FPS 的硬件刷新条件；交互帧率仍需实机性能测试。`
})

async function probe() {
  activeProbe?.abort()
  const controller = new AbortController()
  activeProbe = controller
  probing.value = true
  hostHz.value = null
  frameHz.value = null
  hostError.value = ''
  const [native, browser] = await Promise.allSettled([hostMonitorHz(), sampleWebViewHz(controller.signal)])
  if (activeProbe !== controller || controller.signal.aborted) return
  if (native.status === 'fulfilled') hostHz.value = native.value
  else hostError.value = '原生刷新率读取失败'
  if (browser.status === 'fulfilled') frameHz.value = browser.value
  probing.value = false
  activeProbe = undefined
}
function open() { visible.value = true }
watch(visible, value => {
  if (value) void probe()
  else { activeProbe?.abort(); activeProbe = undefined; probing.value = false }
})
onBeforeUnmount(() => activeProbe?.abort())
function updateLargeText(value: string | number | boolean) {
  setLargeText(Boolean(value))
}
function updateHighContrast(value: string | number | boolean) {
  setHighContrast(Boolean(value))
}
defineExpose({ open })
</script>

<style scoped>
.settings-group { display: grid; gap: 12px; padding: 8px 0 22px; }
.settings-group + .settings-group { border-top: 1px solid var(--care-border); padding-top: 20px; }
.settings-group h3 { margin: 0; font-size: 18px; }
.setting { display: flex; justify-content: space-between; align-items: center; gap: 16px; min-height: 42px; }
.hint { margin: 0; color: var(--care-text-muted); font-size: 13px; line-height: 1.7; }
</style>

<style>
.general-settings-dialog {
  display: flex;
  flex-direction: column;
  max-height: calc(100dvh - 32px);
  margin: 16px auto !important;
  overflow: hidden;
}
.general-settings-dialog .el-dialog__header,
.general-settings-dialog .el-dialog__footer {
  flex-shrink: 0;
}
.general-settings-dialog .el-dialog__body {
  min-height: 0;
  overflow-y: auto;
  overscroll-behavior: contain;
}
@media (max-width: 420px) {
  .general-settings-dialog .el-dialog__footer {
    display: flex;
    justify-content: flex-end;
    flex-wrap: wrap;
    gap: 8px;
  }
  .general-settings-dialog .el-dialog__footer .el-button {
    margin-left: 0;
  }
}
</style>
