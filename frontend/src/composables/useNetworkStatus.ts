import { onBeforeUnmount, onMounted, ref } from 'vue'

export function useNetworkStatus() {
  const isOnline = ref(typeof navigator !== 'undefined' ? navigator.onLine !== false : true)

  function updateOnline() { isOnline.value = navigator.onLine !== false }
  function updateOffline() { isOnline.value = false }

  onMounted(() => {
    if (typeof window === 'undefined') return
    isOnline.value = navigator.onLine !== false
    window.addEventListener('online', updateOnline)
    window.addEventListener('offline', updateOffline)
  })

  onBeforeUnmount(() => {
    if (typeof window === 'undefined') return
    window.removeEventListener('online', updateOnline)
    window.removeEventListener('offline', updateOffline)
  })

  return { isOnline }
}
