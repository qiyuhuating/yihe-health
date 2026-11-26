import { ref } from 'vue'

export function useMapFrameLoad () {
  const mapLoading = ref(false)
  const mapLoadError = ref(false)
  const iframeKey = ref(0)
  const frame = ref<HTMLIFrameElement | null>(null)
  let loadTimer: ReturnType<typeof setTimeout> | undefined
  function clearLoadTimer () {
    clearTimeout(loadTimer)
    loadTimer = undefined
  }
  function startMapLoad () {
    clearLoadTimer()
    frame.value = null
    iframeKey.value++
    mapLoadError.value = false
    mapLoading.value = true
    loadTimer = setTimeout(() => {
      mapLoading.value = false
      mapLoadError.value = true
    }, 10_000)
  }
  function finishMapLoad (event: Event, failed: boolean) {
    if (event.target !== frame.value || mapLoadError.value) return
    clearLoadTimer()
    mapLoading.value = false
    mapLoadError.value = failed
  }
  function resetFrameLoad () {
    clearLoadTimer()
    frame.value = null
    mapLoadError.value = false
    mapLoading.value = false
  }
  return { frame, iframeKey, mapLoading, mapLoadError, startMapLoad, finishMapLoad, resetFrameLoad }
}
