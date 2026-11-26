<template>
  <section class="position-map" aria-label="位置地图">
    <div class="position-map-heading">
      <div><h2>位置地图</h2><p>{{ valid ? `${selection?.name || '长者'} · ${selection?.lng}，${selection?.lat}` : '选择一条位置记录，在此查看。' }}</p></div>
      <div class="position-map-actions">
        <el-button v-if="valid && mapPreferences.provider === 'amap' && !online" @click="enableOnlineMap">加载高德地图</el-button>
        <el-button v-if="online" link @click="disableOnlineMap">关闭在线地图</el-button>
        <a v-if="valid && (mapPreferences.provider === 'tencent' || mapPreferences.provider === 'google') && providerUrl"
          class="online-map-link" :href="providerUrl" target="_blank" rel="noopener noreferrer"
          referrerpolicy="no-referrer">在{{ mapPreferences.provider === 'google' ? 'Google' : '腾讯' }}地图查看位置</a>
        <a v-if="online && embeddedUrl" class="online-map-link" :href="embeddedUrl"
          target="_blank" rel="noopener noreferrer" referrerpolicy="no-referrer">在高德地图中打开</a>
      </div>
    </div>
    <p v-if="online && mapLoading" class="position-map-note" role="status">正在加载高德地图，位置坐标仍可查看…</p>
    <div v-if="online && mapLoadError" role="alert" class="position-map-actions">
      <span>地图未能加载，仍可查看离线位置。</span>
      <el-button @click="startMapLoad">重新加载</el-button>
      <el-button @click="disableOnlineMap">切回离线标记</el-button>
    </div>
    <iframe v-if="online && embeddedUrl && !mapLoadError" :key="iframeKey" ref="frame" :src="embeddedUrl" class="position-map-canvas online-map"
      @load="finishMapLoad($event, false)" @error="finishMapLoad($event, true)"
      title="高德地图 · 设备最近上报位置" loading="eager" referrerpolicy="no-referrer" sandbox="allow-scripts allow-same-origin"
      allowfullscreen></iframe>
    <div v-show="!online" ref="canvas" class="position-map-canvas" :class="{ 'has-location': valid }" role="region" aria-label="离线位置标记"></div>
    <p v-if="valid" class="position-map-note">{{ describePositionContext(selection ?? {}) }}</p>
    <p class="position-map-note" role="status">{{ message }}</p>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch, type PropType } from 'vue'
import L from 'leaflet'
import { useMapFrameLoad } from '../composables/useMapFrameLoad'
import 'leaflet/dist/leaflet.css'
import { describePositionContext, type PositionContext } from '../utils/positionContext'
import { mapPreferences } from '../utils/mapProviders'
import { mapLocationUrl } from '../utils/mapLinks'

interface PositionSelection extends PositionContext {
  lng?: number | string | null
  lat?: number | string | null
  name?: string
}
const props = defineProps({ selection: { type: Object as PropType<PositionSelection | null>, default: null } })
const canvas = ref<HTMLElement | null>(null)
const online = ref(false)
const { frame, iframeKey, mapLoading, mapLoadError, startMapLoad, finishMapLoad, resetFrameLoad } = useMapFrameLoad()
const valid = computed(() => {
  const value = props.selection
  return value && value.lng != null && value.lat != null && String(value.lng).trim() !== '' && String(value.lat).trim() !== '' &&
    Number.isFinite(Number(value.lng)) && Number.isFinite(Number(value.lat)) && Math.abs(Number(value.lng)) <= 180 && Math.abs(Number(value.lat)) <= 90
})
const providerUrl = computed(() => {
  if (!valid.value || !props.selection || mapPreferences.provider === 'offline') return ''
  return mapLocationUrl(mapPreferences.provider, Number(props.selection.lng), Number(props.selection.lat), mapPreferences.style, mapPreferences.coordinateSystem)
})
const embeddedUrl = computed(() => mapPreferences.provider === 'amap' ? providerUrl.value : '')
const message = computed(() => {
  if (!valid.value) return '暂无可用坐标。'
  if (mapPreferences.provider === 'offline') return '当前只显示本地位置标记；未向地图服务发送坐标。'
  if (mapPreferences.provider === 'tencent' || mapPreferences.provider === 'google')
    return `使用${mapPreferences.provider === 'google' ? 'Google' : '腾讯'}官方地图链接查看位置；仅在确认打开链接后发送坐标。`
  return online.value
    ? '高德底图使用转换后的 GCJ-02 显示坐标，仅供参考；原始上报数据未修改。'
    : '原始坐标已在本地标记。高德地图可按需加载，加载后将向服务商发送坐标。'
})
let map: L.Map | null = null
let marker: L.CircleMarker | null = null
let observer: ResizeObserver | null = null
let resizeFrame: number | null = null
function scheduleResize () {
  if (resizeFrame !== null) return
  resizeFrame = requestAnimationFrame(() => {
    resizeFrame = null
    map?.invalidateSize({ animate: false })
  })
}
function updateLocation () {
  if (!map) return
  if (marker) { marker.remove(); marker = null }
  if (!valid.value || !props.selection) { disableOnlineMap(); return }
  const point: L.LatLngTuple = [Number(props.selection.lat), Number(props.selection.lng)]
  map.setView(point, 15, { animate: false })
  marker = L.circleMarker(point, { radius: 9, color: '#fff', weight: 3, fillColor: '#215c4a', fillOpacity: 1 }).addTo(map)
  const label = document.createElement('span')
  label.textContent = props.selection.name || '最近上报位置'
  marker.bindTooltip(label, { direction: 'top', permanent: true, offset: [0, -12] })
}
function enableOnlineMap () {
  if (!map || !valid.value || mapPreferences.provider !== 'amap') return
  online.value = true
  startMapLoad()
}
function disableOnlineMap () {
  resetFrameLoad()
  online.value = false
  scheduleResize()
}
onMounted(() => {
  if (!canvas.value) return
  map = L.map(canvas.value, { scrollWheelZoom: false, attributionControl: true, zoomControl: false }).setView([30, 110], 5)
  L.control.zoom({ zoomInTitle: '放大地图', zoomOutTitle: '缩小地图' }).addTo(map)
  // The offline marker has no provider tiles, so keep Leaflet branding out of its controls.
  map.attributionControl.setPrefix(false)
  observer = new ResizeObserver(scheduleResize)
  observer.observe(canvas.value)
  updateLocation()
  if (valid.value && mapPreferences.provider === 'amap' && mapPreferences.loadOnlineAutomatically) enableOnlineMap()
})
watch(() => [props.selection?.lng, props.selection?.lat, props.selection?.name] as const, updateLocation)
watch(() => [mapPreferences.provider, mapPreferences.loadOnlineAutomatically, valid.value] as const, () => {
  if (mapPreferences.provider !== 'amap' || !valid.value) disableOnlineMap()
  else if (mapPreferences.loadOnlineAutomatically && !online.value) enableOnlineMap()
})
watch(embeddedUrl, (url, previous) => {
  if (online.value && url && url !== previous) startMapLoad()
})
onBeforeUnmount(() => {
  observer?.disconnect()
  observer = null
  resetFrameLoad()
  online.value = false
  if (resizeFrame !== null) cancelAnimationFrame(resizeFrame)
  resizeFrame = null
  marker = null
  map?.remove()
  map = null
})
</script>

<style scoped>
.position-map { margin-top: 24px; padding: clamp(16px, 3vw, 28px); border-top: 1px solid #dce3d8; }
.position-map-heading { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 16px; margin-bottom: 18px; }
.position-map-heading h2 { margin: 0; font-size: 22px; font-weight: 500; }
.position-map-heading p, .position-map-note { color: var(--care-text-muted); line-height: 1.7; overflow-wrap: anywhere; }
.position-map-heading p { margin: 6px 0 0; }
.position-map-canvas { height: clamp(240px, 46vh, 440px); z-index: 0; border-radius: 16px; background-color: #eef1e9; background-image: linear-gradient(#dce3d833 1px, transparent 1px), linear-gradient(90deg, #dce3d833 1px, transparent 1px); background-size: 32px 32px; }
.online-map { display: block; width: 100%; border: 0; }
.position-map-actions { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.online-map-link { display: inline-flex; align-items: center; min-height: var(--care-target-min, 48px); color: var(--care-primary); font-size: 14px; text-underline-offset: 3px; }
.position-map-canvas:not(.has-location):not(.online-map) { opacity: .55; pointer-events: none; }
.position-map-note { margin: 12px 0 0; font-size: 15px; }
.position-map :deep(.leaflet-container) { font-family: inherit; }
.position-map :deep(.leaflet-control-zoom a) { width: 44px; height: 44px; line-height: 44px; }
.position-map :deep(.leaflet-control-attribution) { max-width: 100%; font-size: 12px; }
</style>
